package libdb

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
)

// The reasons for a purge.
//
// The first three are deletions (the data must be gone for good); the others are not.
// When two purges of the same scope overlap, a deletion reason wins.
const (
	PurgeReasonSpaceDeleted     = "space-deleted"
	PurgeReasonAccountDeleted   = "account-deleted"
	PurgeReasonAuthorityDeleted = "authority-deleted"
	PurgeReasonDenied           = "denied"
	PurgeReasonUnlisted         = "unlisted"
	PurgeReasonWriterRemoved    = "writer-removed"
)

// purgeChunkSize is how many rows a purge deletes per statement — so no one statement holds the write lock for long.
const purgeChunkSize = 1000

func isDeletionReason(reason string) bool {
	switch reason {
	case PurgeReasonSpaceDeleted, PurgeReasonAccountDeleted, PurgeReasonAuthorityDeleted:
		return true
	default:
		return false
	}
}

// PurgeScope is what a purge removes:
//
//	• space:   SpaceURI — everything in the space,
//	• repo:    SpaceURI + DID — one repo in one space,
//	• account: DID — every repo of the account, in every space.
type PurgeScope struct {
	Kind     string
	SpaceURI string
	DID      string
}

// Validate returns an error if the scope's fields don't fit its kind.
func (receiver PurgeScope) Validate() error {
	var valid bool
	switch receiver.Kind {
	case ScopeSpace:
		valid = "" != receiver.SpaceURI && "" == receiver.DID
	case ScopeRepo:
		valid = "" != receiver.SpaceURI && "" != receiver.DID
	case ScopeAccount:
		valid = "" == receiver.SpaceURI && "" != receiver.DID
	}
	if !valid {
		return erorr.Wrap(ErrBadPurgeScope, "bad purge scope", field.String("kind", receiver.Kind))
	}
	return nil
}

func (receiver PurgeScope) hideScope() HideScope {
	return HideScope{Kind: receiver.Kind, SpaceURI: receiver.SpaceURI, DID: receiver.DID}
}

// PurgeJob is a purge in progress.
type PurgeJob struct {
	PurgeScope

	Reason    string
	Epoch     int64
	StartedAt int64

	// EventSeq is the seq of the job's purge event, once it has been written.
	EventSeq sql.NullInt64
}

// StartPurge starts purging a scope — in one transaction:
//
//	• the scope gets a new epoch (from the mirror-wide counter), so work started before the purge can't commit (see [CheckFence]),
//	• the scope gets the "purging" hide reason, so it is hidden at once (a "hidden" change-log entry, if it was visible),
//	• a purge job is recorded, so the purge resumes after a crash.
//
// If the scope already has a purge job, it is kept (with its epoch and start time); only its reason may get stronger
// (a deletion reason replaces a non-deletion one).
//
// If the scope is an account that is the authority of spaces, a purge of each of those spaces is started too.
//
// Then call [RunPurge] to do the rest.
func StartPurge(logger log.Logger, db *sql.DB, scope PurgeScope, reason string, now int64) error {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return erorr.Wrap(ErrNilDB, "failed to start purge")
	}

	tx, err := db.Begin()
	if nil != err {
		const msg string = "failed to begin transaction"
		log.Error(field.S(msg), field.E(err))
		return erorr.Wrap(err, msg)
	}
	defer tx.Rollback()

	if err := startPurgeTx(log, tx, scope, reason, now); nil != err {
		return err
	}

	if err := tx.Commit(); nil != err {
		const msg string = "failed to commit transaction"
		log.Error(field.S(msg), field.E(err))
		return erorr.Wrap(err, msg)
	}

	return nil
}

func startPurgeTx(log log.Logger, tx *sql.Tx, scope PurgeScope, reason string, now int64) error {
	if err := scope.Validate(); nil != err {
		return err
	}

	var fields = []field.StringlyField{
		field.String("scope-kind", scope.Kind),
		field.String("space-uri", scope.SpaceURI),
		field.String("did", scope.DID),
		field.String("reason", reason),
	}

	// An existing job for the scope: keep it; maybe strengthen its reason.
	{
		var existingReason string
		err := tx.QueryRow(
			`SELECT reason FROM purge_jobs WHERE scope_kind = ? AND space_uri = ? AND did = ?`,
			scope.Kind, scope.SpaceURI, scope.DID,
		).Scan(&existingReason)
		switch {
		case nil == err:
			if isDeletionReason(reason) && !isDeletionReason(existingReason) {
				if _, err := tx.Exec(
					`UPDATE purge_jobs SET reason = ? WHERE scope_kind = ? AND space_uri = ? AND did = ?`,
					reason, scope.Kind, scope.SpaceURI, scope.DID,
				); nil != err {
					const msg string = "failed to strengthen purge reason"
					log.Error(append([]field.StringlyField{field.S(msg), field.E(err)}, fields...)...)
					return erorr.Wrap(err, msg, fields...)
				}
			}
			return nil
		case erorr.Is(err, sql.ErrNoRows):
			// Nothing here.
		default:
			const msg string = "failed to look up purge job"
			log.Error(append([]field.StringlyField{field.S(msg), field.E(err)}, fields...)...)
			return erorr.Wrap(err, msg, fields...)
		}
	}

	epoch, err := nextEpoch(tx)
	if nil != err {
		log.Error(append([]field.StringlyField{field.S("failed to get a new epoch"), field.E(err)}, fields...)...)
		return err
	}

	// The new epoch fences off work started before the purge.
	{
		var statements []string
		var args [][]any
		switch scope.Kind {
		case ScopeSpace:
			statements = []string{`UPDATE spaces SET epoch = ? WHERE space_uri = ?`}
			args = [][]any{{epoch, scope.SpaceURI}}
		case ScopeRepo:
			statements = []string{`UPDATE repos SET epoch = ? WHERE space_uri = ? AND did = ?`}
			args = [][]any{{epoch, scope.SpaceURI, scope.DID}}
		case ScopeAccount:
			statements = []string{
				`UPDATE repos SET epoch = ? WHERE did = ?`,
				`INSERT INTO accounts (did, purge_epoch) VALUES (?, ?) ON CONFLICT (did) DO UPDATE SET purge_epoch = excluded.purge_epoch`,
			}
			args = [][]any{{epoch, scope.DID}, {scope.DID, epoch}}
		}
		for index, statement := range statements {
			if _, err := tx.Exec(statement, args[index]...); nil != err {
				const msg string = "failed to set the purge epoch"
				log.Error(append([]field.StringlyField{field.S(msg), field.E(err)}, fields...)...)
				return erorr.Wrap(err, msg, fields...)
			}
		}
	}

	if _, err := changeHideTx(log, tx, scope.hideScope(), ReasonPurging, now, true, sql.NullInt64{}); nil != err {
		return err
	}

	if _, err := tx.Exec(
		`INSERT INTO purge_jobs (scope_kind, space_uri, did, reason, epoch, started_at) VALUES (?, ?, ?, ?, ?, ?)`,
		scope.Kind, scope.SpaceURI, scope.DID, reason, epoch, now,
	); nil != err {
		const msg string = "failed to record purge job"
		log.Error(append([]field.StringlyField{field.S(msg), field.E(err)}, fields...)...)
		return erorr.Wrap(err, msg, fields...)
	}

	// An account that is a space authority takes its spaces with it.
	if ScopeAccount == scope.Kind {
		rows, err := tx.Query(`SELECT space_uri FROM spaces WHERE authority = ?`, scope.DID)
		if nil != err {
			const msg string = "failed to look up the account's spaces"
			log.Error(append([]field.StringlyField{field.S(msg), field.E(err)}, fields...)...)
			return erorr.Wrap(err, msg, fields...)
		}
		var spaceURIs []string
		for rows.Next() {
			var spaceURI string
			if err := rows.Scan(&spaceURI); nil != err {
				rows.Close()
				return erorr.Wrap(err, "failed to scan space row")
			}
			spaceURIs = append(spaceURIs, spaceURI)
		}
		rows.Close()

		var spaceReason string = reason
		if PurgeReasonAccountDeleted == reason {
			spaceReason = PurgeReasonAuthorityDeleted
		}

		for _, spaceURI := range spaceURIs {
			if err := startPurgeTx(log, tx, PurgeScope{Kind: ScopeSpace, SpaceURI: spaceURI}, spaceReason, now); nil != err {
				return err
			}
		}
	}

	return nil
}

// PurgeJobsList returns every purge job in progress (ex: to resume them at start).
func PurgeJobsList(logger log.Logger, db *sql.DB) ([]PurgeJob, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return nil, erorr.Wrap(ErrNilDB, "failed to list purge jobs")
	}

	rows, err := db.Query(`SELECT scope_kind, space_uri, did, reason, epoch, started_at, event_seq FROM purge_jobs ORDER BY started_at ASC`)
	if nil != err {
		const msg string = "failed to list purge jobs"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}
	defer rows.Close()

	var jobs []PurgeJob
	for rows.Next() {
		var job PurgeJob
		if err := rows.Scan(&job.Kind, &job.SpaceURI, &job.DID, &job.Reason, &job.Epoch, &job.StartedAt, &job.EventSeq); nil != err {
			const msg string = "failed to scan purge job row"
			log.Error(field.S(msg), field.E(err))
			return nil, erorr.Wrap(err, msg)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); nil != err {
		const msg string = "failed after the purge job rows loop"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}

	return jobs, nil
}

// RunPurge does (or resumes) a purge job, step by step. Each step is safe to repeat, so a purge interrupted at any point
// can just be run again:
//
//	• delete the scope's records (all generations), labels, and blob references — in chunks;
//	  then delete blobs nothing refers to any more (file first, then row),
//	• write the purge event (once), and delete the scope's earlier change-log entries — in chunks,
//	• finish, in one transaction: reset (or delete) the repos, set a purged space's lifecycle,
//	  delete the scope's hides (operator hides are kept — except record ones when the reason is a deletion),
//	  and delete the job — the "purging" hide goes with the rest, without a "visible" change-log entry,
//	• then reclaim the freed space in the database file, and truncate the write-ahead log.
//
// It returns whether the write-ahead log was fully truncated (false means a reader still held it — try again later).
func RunPurge(logger log.Logger, db *sql.DB, blobDir string, job PurgeJob, now int64) (walTruncated bool, err error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return false, erorr.Wrap(ErrNilDB, "failed to run purge")
	}
	if err := job.Validate(); nil != err {
		return false, err
	}

	var fields = []field.StringlyField{
		field.String("scope-kind", job.Kind),
		field.String("space-uri", job.SpaceURI),
		field.String("did", job.DID),
		field.String("reason", job.Reason),
	}

	log.Inform(append([]field.StringlyField{field.S("purging")}, fields...)...)

	// The rows of a table that belong to the scope.
	var where = func(spaceColumn string, didColumn string) (string, []any) {
		switch job.Kind {
		case ScopeSpace:
			return spaceColumn + ` = ?`, []any{job.SpaceURI}
		case ScopeRepo:
			return spaceColumn + ` = ? AND ` + didColumn + ` = ?`, []any{job.SpaceURI, job.DID}
		default: // ScopeAccount
			return didColumn + ` = ?`, []any{job.DID}
		}
	}

	// Step: delete the scope's records, labels, and blob references.
	for _, table := range []struct{ name, spaceColumn, didColumn string }{
		{"records", "space_uri", "did"},
		{"labels", "space_uri", "src_did"},
		{"blob_refs", "space_uri", "did"},
	} {
		condition, args := where(table.spaceColumn, table.didColumn)
		if err := deleteInChunks(db, table.name, "rowid", condition, args); nil != err {
			log.Error(append([]field.StringlyField{field.S("failed to purge rows"), field.E(err), field.String("table", table.name)}, fields...)...)
			return false, err
		}
	}

	// Step: delete the blobs nothing refers to any more.
	if err := deleteUnreferencedBlobs(log, db, blobDir); nil != err {
		return false, err
	}

	// Step: the purge event (once), then the scope's earlier change-log entries.
	{
		var eventSeq int64
		{
			tx, err := db.Begin()
			if nil != err {
				return false, erorr.Wrap(err, "failed to begin transaction")
			}

			var existing sql.NullInt64
			err = tx.QueryRow(
				`SELECT event_seq FROM purge_jobs WHERE scope_kind = ? AND space_uri = ? AND did = ?`,
				job.Kind, job.SpaceURI, job.DID,
			).Scan(&existing)
			if nil != err {
				tx.Rollback()
				log.Error(append([]field.StringlyField{field.S("failed to read purge job"), field.E(err)}, fields...)...)
				return false, erorr.Wrap(err, "failed to read purge job", fields...)
			}

			if existing.Valid {
				eventSeq = existing.Int64
				tx.Rollback()
			} else {
				var kind string
				switch job.Kind {
				case ScopeSpace:
					kind = ChangeKindSpacePurged
				case ScopeRepo:
					kind = ChangeKindWriterRemoved
				default:
					kind = ChangeKindAccountPurged
				}

				eventSeq, err = appendChange(tx, Change{
					At:       now,
					Kind:     kind,
					Scope:    job.Kind,
					SpaceURI: job.SpaceURI,
					DID:      job.DID,
					Reason:   job.Reason,
				})
				if nil != err {
					tx.Rollback()
					return false, err
				}

				if _, err := tx.Exec(
					`UPDATE purge_jobs SET event_seq = ? WHERE scope_kind = ? AND space_uri = ? AND did = ?`,
					eventSeq, job.Kind, job.SpaceURI, job.DID,
				); nil != err {
					tx.Rollback()
					return false, erorr.Wrap(err, "failed to record the purge event")
				}

				if err := tx.Commit(); nil != err {
					return false, erorr.Wrap(err, "failed to commit transaction")
				}
			}
		}

		condition, args := where("space_uri", "did")
		condition += ` AND seq < ?`
		args = append(args, eventSeq)
		if err := deleteInChunks(db, "changelog", "seq", condition, args); nil != err {
			log.Error(append([]field.StringlyField{field.S("failed to purge change-log entries"), field.E(err)}, fields...)...)
			return false, err
		}
	}

	// Step: finish.
	{
		tx, err := db.Begin()
		if nil != err {
			return false, erorr.Wrap(err, "failed to begin transaction")
		}
		defer tx.Rollback()

		// A purged space: "unavailable" (if still listed — so it can come back), otherwise "purged".
		if ScopeSpace == job.Kind {
			if _, err := tx.Exec(
				`UPDATE spaces SET lifecycle = CASE WHEN 1 = listed THEN 'unavailable' ELSE 'purged' END,`+
				` had_success = 0, space_rev = '', space_rev_seen_at = NULL, last_catchup_at = NULL, last_authorized_at = NULL, verified_at = NULL, stale_reasons = ''`+
				` WHERE space_uri = ?`,
				job.SpaceURI,
			); nil != err {
				return false, erorr.Wrap(err, "failed to set the purged space's lifecycle")
			}
		}

		// Repos: delete them — except those with a target recorded after the purge started (a writer re-added meanwhile),
		// which are reset, so they are synced from scratch.
		{
			condition, args := where("space_uri", "did")

			if _, err := tx.Exec(`DELETE FROM repos WHERE `+condition+` AND target_epoch <= ?`, append(args, job.Epoch)...); nil != err {
				return false, erorr.Wrap(err, "failed to delete repos")
			}
			if _, err := tx.Exec(
				`UPDATE repos SET rev = '', data = NULL, lthash = NULL, generation = generation + 1, verified_at = NULL, stale_reasons = '' WHERE `+condition,
				args...,
			); nil != err {
				return false, erorr.Wrap(err, "failed to reset repos")
			}
		}

		// Hides: all of the scope's — except operator hides (policy, not data);
		// and even those, for single records, when the reason is a deletion.
		{
			var condition string
			var args []any
			switch job.Kind {
			case ScopeSpace:
				condition, args = `space_uri = ?`, []any{job.SpaceURI}
			case ScopeRepo:
				condition, args = `space_uri = ? AND did = ? AND scope_kind IN ('repo', 'record')`, []any{job.SpaceURI, job.DID}
			default: // ScopeAccount
				condition, args = `did = ?`, []any{job.DID}
			}

			var keepOperator string = `reason <> 'operator'`
			if isDeletionReason(job.Reason) {
				keepOperator = `(reason <> 'operator' OR scope_kind = 'record')`
			}

			if _, err := tx.Exec(`DELETE FROM hides WHERE `+condition+` AND `+keepOperator, args...); nil != err {
				return false, erorr.Wrap(err, "failed to delete hides")
			}
		}

		if _, err := tx.Exec(
			`DELETE FROM purge_jobs WHERE scope_kind = ? AND space_uri = ? AND did = ?`,
			job.Kind, job.SpaceURI, job.DID,
		); nil != err {
			return false, erorr.Wrap(err, "failed to delete purge job")
		}

		if err := tx.Commit(); nil != err {
			log.Error(append([]field.StringlyField{field.S("failed to commit the end of the purge"), field.E(err)}, fields...)...)
			return false, erorr.Wrap(err, "failed to commit transaction")
		}
	}

	walTruncated, err = Reclaim(log, db)
	if nil != err {
		return false, err
	}

	log.Inform(append([]field.StringlyField{field.S("purged"), field.Bool("wal-truncated", walTruncated)}, fields...)...)

	return walTruncated, nil
}

// deleteInChunks deletes the rows of 'table' that match 'condition', at most purgeChunkSize at a time.
func deleteInChunks(db *sql.DB, table string, keyColumn string, condition string, args []any) error {
	var statement string =
		`DELETE FROM ` + table + ` WHERE ` + keyColumn + ` IN (SELECT ` + keyColumn + ` FROM ` + table + ` WHERE ` + condition + ` LIMIT ?)`

	for {
		result, err := db.Exec(statement, append(append([]any{}, args...), purgeChunkSize)...)
		if nil != err {
			return erorr.Wrap(err, "failed to delete rows", field.String("table", table))
		}

		affected, err := result.RowsAffected()
		if nil != err {
			return erorr.Wrap(err, "failed to get rows affected", field.String("table", table))
		}
		if affected < purgeChunkSize {
			return nil
		}
	}
}

// deleteUnreferencedBlobs deletes every blob that no blob reference refers to:
// first the row is marked "deleting" (only if still unreferenced), then the file is deleted, then the row.
// A crash in between leaves a "deleting" row, which a later call finishes.
func deleteUnreferencedBlobs(log log.Logger, db *sql.DB, blobDir string) error {
	for {
		rows, err := db.Query(
			`SELECT cid, path FROM blobs WHERE NOT EXISTS (SELECT 1 FROM blob_refs AS br WHERE br.cid = blobs.cid) LIMIT ?`,
			purgeChunkSize,
		)
		if nil != err {
			return erorr.Wrap(err, "failed to find unreferenced blobs")
		}

		type internalBlob struct{ cid, path string }
		var blobs []internalBlob
		for rows.Next() {
			var blob internalBlob
			if err := rows.Scan(&blob.cid, &blob.path); nil != err {
				rows.Close()
				return erorr.Wrap(err, "failed to scan blob row")
			}
			blobs = append(blobs, blob)
		}
		rows.Close()

		if 0 == len(blobs) {
			return nil
		}

		for _, blob := range blobs {
			// Mark it — only if nothing has started referring to it meanwhile.
			result, err := db.Exec(
				`UPDATE blobs SET state = 'deleting' WHERE cid = ? AND NOT EXISTS (SELECT 1 FROM blob_refs AS br WHERE br.cid = blobs.cid)`,
				blob.cid,
			)
			if nil != err {
				return erorr.Wrap(err, "failed to mark blob for deletion", field.String("cid", blob.cid))
			}
			if affected, _ := result.RowsAffected(); 0 == affected {
				continue
			}

			if "" != blob.path {
				filename, inside := blobFilename(blobDir, blob.path)
				if !inside {
					log.Alert(field.S("blob path escapes the blob directory; not deleting the file"), field.String("cid", blob.cid), field.String("path", blob.path))
				} else if err := os.Remove(filename); nil != err && !os.IsNotExist(err) {
					log.Error(field.S("failed to delete blob file"), field.E(err), field.String("cid", blob.cid), field.String("file-name", filename))
					return erorr.Wrap(err, "failed to delete blob file", field.String("cid", blob.cid))
				}
			}

			if _, err := db.Exec(`DELETE FROM blobs WHERE cid = ? AND state = 'deleting'`, blob.cid); nil != err {
				return erorr.Wrap(err, "failed to delete blob row", field.String("cid", blob.cid))
			}
		}

		if len(blobs) < purgeChunkSize {
			return nil
		}
	}
}

// blobFilename returns the file name of a blob (its 'path' is relative to 'blobDir'),
// and whether it is inside 'blobDir'.
func blobFilename(blobDir string, path string) (string, bool) {
	var filename string = filepath.Join(blobDir, filepath.Clean("/"+path))
	return filename, strings.HasPrefix(filename, filepath.Clean(blobDir)+string(filepath.Separator))
}

// Reclaim returns the database file's free pages (from deletions) to the file system, and truncates the write-ahead log,
// so deleted data does not linger in either.
//
// The write-ahead log can't be truncated while a reader still uses it, so this retries a few times.
// It returns whether the write-ahead log was fully truncated.
//
// It must be run on the write database (the one writer).
func Reclaim(logger log.Logger, db *sql.DB) (bool, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return false, erorr.Wrap(ErrNilDB, "failed to reclaim")
	}

	if _, err := db.Exec(`PRAGMA incremental_vacuum`); nil != err {
		log.Error(field.S("failed to run incremental vacuum"), field.E(err))
		return false, erorr.Wrap(err, "failed to run incremental vacuum")
	}

	const attempts = 10
	for attempt := 1; attempt <= attempts; attempt++ {
		var busy, logFrames, checkpointed int
		if err := db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); nil != err {
			log.Error(field.S("failed to checkpoint the write-ahead log"), field.E(err))
			return false, erorr.Wrap(err, "failed to checkpoint the write-ahead log")
		}
		if 0 == busy {
			return true, nil
		}

		time.Sleep(time.Duration(attempt) * 50 * time.Millisecond)
	}

	log.Warn(field.S("the write-ahead log could not be truncated (a reader is holding it) — deleted data may linger in it until the next checkpoint"))
	return false, nil
}

// DuePurge is a scope whose grace period is over.
type DuePurge struct {
	PurgeScope
	Reason string
}

// DuePurgesList returns the scopes with a hide whose purge time has passed (ex: unlisted or denied spaces after the grace period)
// and which are not being purged yet.
func DuePurgesList(logger log.Logger, db *sql.DB, now int64) ([]DuePurge, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return nil, erorr.Wrap(ErrNilDB, "failed to list due purges")
	}

	rows, err := db.Query(
		`SELECT h.scope_kind, h.space_uri, h.did, h.reason FROM hides AS h` +" "+
		`WHERE h.purge_after IS NOT NULL AND h.purge_after <= ? AND h.scope_kind IN ('space', 'repo', 'account')` +" "+
		`AND NOT EXISTS (SELECT 1 FROM purge_jobs AS j WHERE j.scope_kind = h.scope_kind AND j.space_uri = h.space_uri AND j.did = h.did)` +" "+
		`ORDER BY h.purge_after ASC`,
		now,
	)
	if nil != err {
		const msg string = "failed to list due purges"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}
	defer rows.Close()

	var due []DuePurge
	for rows.Next() {
		var purge DuePurge
		var hideReason string
		if err := rows.Scan(&purge.Kind, &purge.SpaceURI, &purge.DID, &hideReason); nil != err {
			const msg string = "failed to scan due purge row"
			log.Error(field.S(msg), field.E(err))
			return nil, erorr.Wrap(err, msg)
		}

		switch hideReason {
		case ReasonUnlisted:
			purge.Reason = PurgeReasonUnlisted
		case ReasonDenied:
			purge.Reason = PurgeReasonDenied
		default:
			continue
		}

		due = append(due, purge)
	}
	if err := rows.Err(); nil != err {
		const msg string = "failed after the due purge rows loop"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}

	return due, nil
}

// Fence is the epochs (and scope) a piece of work — a sync, a staging, a blob download — started under.
//
// Before writing, the work calls [CheckFence] (inside its write transaction): if a purge has started since — or is going on —
// the work must not commit.
type Fence struct {
	SpaceURI string
	DID      string // "" for work on a whole space

	SpaceEpoch   int64
	RepoEpoch    int64
	AccountEpoch int64
}

// ReadFence reads the current epochs for a space (and repo).
func ReadFence(logger log.Logger, db *sql.DB, spaceURI string, did string) (Fence, error) {
	log := logger.Begin()
	defer log.End()

	var fence = Fence{SpaceURI: spaceURI, DID: did}

	if nil == db {
		return fence, erorr.Wrap(ErrNilDB, "failed to read fence")
	}

	err := db.QueryRow(
		`SELECT COALESCE((SELECT epoch FROM spaces WHERE space_uri = ?), -1),` +
		` COALESCE((SELECT epoch FROM repos WHERE space_uri = ? AND did = ?), -1),` +
		` COALESCE((SELECT purge_epoch FROM accounts WHERE did = ?), 0)`,
		spaceURI, spaceURI, did, did,
	).Scan(&fence.SpaceEpoch, &fence.RepoEpoch, &fence.AccountEpoch)
	if nil != err {
		const msg string = "failed to read fence"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI), field.String("did", did))
		return fence, erorr.Wrap(err, msg, field.String("space-uri", spaceURI), field.String("did", did))
	}

	return fence, nil
}

// CheckFence returns [ErrFenced] if, since the fence was read, any of its epochs changed (or its row is gone),
// or if a purge of the space, the repo, or the account is going on.
//
// Call it inside the write transaction that would commit the work.
func CheckFence(tx *sql.Tx, fence Fence) error {
	var spaceEpoch, repoEpoch, accountEpoch int64
	var purging int

	err := tx.QueryRow(
		`SELECT COALESCE((SELECT epoch FROM spaces WHERE space_uri = ?), -1),` +
		` COALESCE((SELECT epoch FROM repos WHERE space_uri = ? AND did = ?), -1),` +
		` COALESCE((SELECT purge_epoch FROM accounts WHERE did = ?), 0),` +
		` (SELECT COUNT(*) FROM purge_jobs WHERE` +
		`   (scope_kind = 'space' AND space_uri = ?)` +
		`   OR (scope_kind = 'repo' AND space_uri = ? AND did = ?)` +
		`   OR (scope_kind = 'account' AND did = ? AND '' <> ?))`,
		fence.SpaceURI, fence.SpaceURI, fence.DID, fence.DID,
		fence.SpaceURI, fence.SpaceURI, fence.DID, fence.DID, fence.DID,
	).Scan(&spaceEpoch, &repoEpoch, &accountEpoch, &purging)
	if nil != err {
		return erorr.Wrap(err, "failed to check fence")
	}

	switch {
	case 0 < purging:
		return erorr.Wrap(ErrFenced, "a purge is going on")
	case spaceEpoch < 0 || spaceEpoch != fence.SpaceEpoch:
		return erorr.Wrap(ErrFenced, "the space's epoch changed")
	case "" != fence.DID && (repoEpoch < 0 || repoEpoch != fence.RepoEpoch):
		return erorr.Wrap(ErrFenced, "the repo's epoch changed")
	case accountEpoch != fence.AccountEpoch:
		return erorr.Wrap(ErrFenced, "the account's epoch changed")
	}

	return nil
}

// SweepBlobDir cleans up the blob directory, after a crash:
//
//	• blobs that nothing refers to any more (including those left half-deleted) are deleted — file, then row,
//	• files that no blob row names (ex: a download interrupted, or a row deleted before its file) are deleted.
//
// Run it at start, before any blob is downloaded.
func SweepBlobDir(logger log.Logger, db *sql.DB, blobDir string) error {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return erorr.Wrap(ErrNilDB, "failed to sweep the blob directory")
	}

	if err := deleteUnreferencedBlobs(log, db, blobDir); nil != err {
		return err
	}

	var known = map[string]struct{}{}
	{
		rows, err := db.Query(`SELECT path FROM blobs WHERE '' <> path`)
		if nil != err {
			return erorr.Wrap(err, "failed to list blob paths")
		}
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); nil != err {
				rows.Close()
				return erorr.Wrap(err, "failed to scan blob path")
			}
			if filename, inside := blobFilename(blobDir, path); inside {
				known[filename] = struct{}{}
			}
		}
		rows.Close()
	}

	var swept int
	err := filepath.WalkDir(blobDir, func(filename string, entry os.DirEntry, err error) error {
		if nil != err {
			if os.IsNotExist(err) && filename == blobDir {
				return filepath.SkipDir
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if _, found := known[filename]; found {
			return nil
		}

		if err := os.Remove(filename); nil != err && !os.IsNotExist(err) {
			return erorr.Wrap(err, "failed to delete stray blob file", field.String("file-name", filename))
		}
		swept++
		return nil
	})
	if nil != err {
		log.Error(field.S("failed to sweep the blob directory"), field.E(err), field.String("blob-dir", blobDir))
		return erorr.Wrap(err, "failed to sweep the blob directory", field.String("blob-dir", blobDir))
	}

	if 0 < swept {
		log.Inform(field.S("swept stray blob files"), field.Int("count", swept), field.String("blob-dir", blobDir))
	}

	return nil
}
