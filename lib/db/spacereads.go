package libdb

import (
	"context"
	"database/sql"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
	"github.com/reiver/go-opt"
)

// Writer is a repo in a space, as the space-mirror sees it.
type Writer struct {
	DID string
	Rev string

	// Visible is whether the repo is visible (not counting its space).
	Visible bool

	// HideReasons are the reasons the repo is hidden — from 'account' and 'repo' hides. Empty when Visible.
	HideReasons []string
}

// WritersList returns up to 'limit' repos of a space, ordered by DID, starting after 'afterDID' — pass "" to start at the beginning.
//
// If 'includeHidden' is false, only visible repos are returned.
// (It does not check the visibility of the space — call [SpaceVisible] for that.)
func WritersList(logger log.Logger, db *sql.DB, spaceURI string, afterDID string, limit int, includeHidden bool) ([]Writer, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return nil, erorr.Wrap(ErrNilDB, "failed to list writers")
	}

	var query string =
		`SELECT rp.did, rp.rev, (` + sqlRepoVisible + `) AS visible,` +" "+
		`(SELECT group_concat(h.reason, ' ') FROM hides AS h WHERE` +" "+
			`(h.scope_kind = 'account' AND h.space_uri = '' AND h.did = rp.did AND h.collection = '' AND h.rkey = '' AND h.cid = '')` +" "+
			`OR (h.scope_kind = 'repo' AND h.space_uri = rp.space_uri AND h.did = rp.did AND h.collection = '' AND h.rkey = '' AND h.cid = '')` +
		`) AS hide_reasons` +" "+
		`FROM repos AS rp` +" "+
		`WHERE rp.space_uri = ? AND rp.did > ?`
	if !includeHidden {
		query += ` AND ` + sqlRepoVisible
	}
	query += ` ORDER BY rp.did ASC LIMIT ?`

	rows, err := db.Query(query, spaceURI, afterDID, limit)
	if nil != err {
		const msg string = "failed to list writers"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI))
		return nil, erorr.Wrap(err, msg, field.String("space-uri", spaceURI))
	}
	defer rows.Close()

	var writers []Writer
	for rows.Next() {
		var writer Writer
		var hideReasons sql.NullString

		if err := rows.Scan(&writer.DID, &writer.Rev, &writer.Visible, &hideReasons); nil != err {
			const msg string = "failed to scan writer row"
			log.Error(field.S(msg), field.E(err))
			return nil, erorr.Wrap(err, msg)
		}
		if hideReasons.Valid {
			writer.HideReasons = unionReasons(hideReasons.String)
		}

		writers = append(writers, writer)
	}
	if err := rows.Err(); nil != err {
		const msg string = "failed after the writer rows loop"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}

	return writers, nil
}

// SpaceRecord is a record in a space: its repo (author DID), path, CID, and DRISL bytes.
type SpaceRecord struct {
	DID string
	Record
}

// SpaceRecordsList returns up to 'limit' visible records across all the repos of a space,
// ordered by (DID, collection, rkey), starting after the record (afterDID, afterCollection, afterRKey) — pass "", "", "" to start at the beginning.
// If 'collection' is something, only records in that collection are returned.
//
// It also returns the change-log high-water mark (the largest seq), read in the same read transaction as the records.
// A consumer that replays the change log from that seq repairs anything that changed while it was paging.
//
// It does not check the visibility of the space — call [SpaceVisible] for that.
func SpaceRecordsList(logger log.Logger, db *sql.DB, spaceURI string, collection opt.Optional[string], afterDID string, afterCollection string, afterRKey string, limit int) (records []SpaceRecord, highWaterSeq int64, err error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return nil, 0, erorr.Wrap(ErrNilDB, "failed to list space records")
	}

	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if nil != err {
		const msg string = "failed to begin read transaction"
		log.Error(field.S(msg), field.E(err))
		return nil, 0, erorr.Wrap(err, msg)
	}
	defer tx.Rollback()

	{
		err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM changelog`).Scan(&highWaterSeq)
		if nil != err {
			const msg string = "failed to read the change-log high-water mark"
			log.Error(field.S(msg), field.E(err))
			return nil, 0, erorr.Wrap(err, msg)
		}
	}

	var query string =
		`SELECT r.did, r.collection, r.rkey, r.cid, r.drisl FROM records AS r` +" "+
		`JOIN repos AS rp ON rp.space_uri = r.space_uri AND rp.did = r.did` +" "+
		`WHERE r.space_uri = ?` +" "+
		`AND ` + sqlRepoVisible +" "+
		`AND ` + sqlRecordVisible +" "+
		`AND (r.did > ? OR (r.did = ? AND (r.collection > ? OR (r.collection = ? AND r.rkey > ?))))`
	var args []any = []any{spaceURI, afterDID, afterDID, afterCollection, afterCollection, afterRKey}

	if c, found := collection.Get(); found {
		query += ` AND r.collection = ?`
		args = append(args, c)
	}

	query += ` ORDER BY r.did ASC, r.collection ASC, r.rkey ASC LIMIT ?`
	args = append(args, limit)

	rows, err := tx.Query(query, args...)
	if nil != err {
		const msg string = "failed to list space records"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI))
		return nil, 0, erorr.Wrap(err, msg, field.String("space-uri", spaceURI))
	}
	defer rows.Close()

	for rows.Next() {
		var record SpaceRecord

		if err := rows.Scan(&record.DID, &record.Collection, &record.RKey, &record.CID, &record.DRISL); nil != err {
			const msg string = "failed to scan space record row"
			log.Error(field.S(msg), field.E(err))
			return nil, 0, erorr.Wrap(err, msg)
		}

		records = append(records, record)
	}
	if err := rows.Err(); nil != err {
		const msg string = "failed after the space record rows loop"
		log.Error(field.S(msg), field.E(err))
		return nil, 0, erorr.Wrap(err, msg)
	}

	return records, highWaterSeq, nil
}

// SpaceStatus is the status of a listed space.
type SpaceStatus struct {
	SpaceURI  string
	Lifecycle string

	// Serving is whether the space is visible (served).
	Serving bool

	Freshness Freshness

	// HideReasons are the reasons the space is hidden (from 'space' hides).
	HideReasons []string
}

// SpacesStatusList returns the status of up to 'limit' listed spaces, ordered by space URI, starting after 'afterSpaceURI' — pass "" to start at the beginning.
// If 'onlySpaceURI' is something, only that space is returned (if it is listed).
//
// It also returns the change-log high-water mark (the largest seq), read in the same read transaction.
func SpacesStatusList(logger log.Logger, db *sql.DB, onlySpaceURI opt.Optional[string], afterSpaceURI string, limit int) (statuses []SpaceStatus, highWaterSeq int64, err error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return nil, 0, erorr.Wrap(ErrNilDB, "failed to list space statuses")
	}

	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if nil != err {
		const msg string = "failed to begin read transaction"
		log.Error(field.S(msg), field.E(err))
		return nil, 0, erorr.Wrap(err, msg)
	}
	defer tx.Rollback()

	{
		err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM changelog`).Scan(&highWaterSeq)
		if nil != err {
			const msg string = "failed to read the change-log high-water mark"
			log.Error(field.S(msg), field.E(err))
			return nil, 0, erorr.Wrap(err, msg)
		}
	}

	var query string =
		`SELECT s.space_uri, s.lifecycle, (` + sqlSpaceVisible + `) AS serving, s.verified_at, s.stale_reasons,` +" "+
		`(SELECT group_concat(h.reason, ' ') FROM hides AS h WHERE h.scope_kind = 'space' AND h.space_uri = s.space_uri AND h.did = '' AND h.collection = '' AND h.rkey = '' AND h.cid = '') AS hide_reasons` +" "+
		`FROM spaces AS s` +" "+
		`WHERE s.listed = 1 AND s.space_uri > ?`
	var args []any = []any{afterSpaceURI}

	if spaceURI, found := onlySpaceURI.Get(); found {
		query += ` AND s.space_uri = ?`
		args = append(args, spaceURI)
	}

	query += ` ORDER BY s.space_uri ASC LIMIT ?`
	args = append(args, limit)

	rows, err := tx.Query(query, args...)
	if nil != err {
		const msg string = "failed to list space statuses"
		log.Error(field.S(msg), field.E(err))
		return nil, 0, erorr.Wrap(err, msg)
	}
	defer rows.Close()

	for rows.Next() {
		var status SpaceStatus
		var verifiedAt sql.NullInt64
		var staleReasons string
		var hideReasons sql.NullString

		if err := rows.Scan(&status.SpaceURI, &status.Lifecycle, &status.Serving, &verifiedAt, &staleReasons, &hideReasons); nil != err {
			const msg string = "failed to scan space status row"
			log.Error(field.S(msg), field.E(err))
			return nil, 0, erorr.Wrap(err, msg)
		}

		if verifiedAt.Valid {
			status.Freshness.VerifiedAt = opt.Something(verifiedAt.Int64)
		}
		status.Freshness.StaleReasons = unionReasons(staleReasons)
		if hideReasons.Valid {
			status.HideReasons = unionReasons(hideReasons.String)
		}

		statuses = append(statuses, status)
	}
	if err := rows.Err(); nil != err {
		const msg string = "failed after the space status rows loop"
		log.Error(field.S(msg), field.E(err))
		return nil, 0, erorr.Wrap(err, msg)
	}

	return statuses, highWaterSeq, nil
}

// RepoStatus is the status of a repo in a space.
type RepoStatus struct {
	DID       string
	Rev       string
	Visible   bool
	Freshness Freshness

	// HideReasons are the reasons the repo is hidden (from 'account' and 'repo' hides).
	HideReasons []string
}

// RepoStatusesList returns the status of every repo in a space, ordered by DID.
func RepoStatusesList(logger log.Logger, db *sql.DB, spaceURI string) ([]RepoStatus, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return nil, erorr.Wrap(ErrNilDB, "failed to list repo statuses")
	}

	const query string =
		`SELECT rp.did, rp.rev, (` + sqlRepoVisible + `) AS visible, rp.verified_at, rp.stale_reasons,` +" "+
		`(SELECT group_concat(h.reason, ' ') FROM hides AS h WHERE` +" "+
			`(h.scope_kind = 'account' AND h.space_uri = '' AND h.did = rp.did AND h.collection = '' AND h.rkey = '' AND h.cid = '')` +" "+
			`OR (h.scope_kind = 'repo' AND h.space_uri = rp.space_uri AND h.did = rp.did AND h.collection = '' AND h.rkey = '' AND h.cid = '')` +
		`) AS hide_reasons` +" "+
		`FROM repos AS rp WHERE rp.space_uri = ? ORDER BY rp.did ASC`

	rows, err := db.Query(query, spaceURI)
	if nil != err {
		const msg string = "failed to list repo statuses"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI))
		return nil, erorr.Wrap(err, msg, field.String("space-uri", spaceURI))
	}
	defer rows.Close()

	var statuses []RepoStatus
	for rows.Next() {
		var status RepoStatus
		var verifiedAt sql.NullInt64
		var staleReasons string
		var hideReasons sql.NullString

		if err := rows.Scan(&status.DID, &status.Rev, &status.Visible, &verifiedAt, &staleReasons, &hideReasons); nil != err {
			const msg string = "failed to scan repo status row"
			log.Error(field.S(msg), field.E(err))
			return nil, erorr.Wrap(err, msg)
		}

		if verifiedAt.Valid {
			status.Freshness.VerifiedAt = opt.Something(verifiedAt.Int64)
		}
		status.Freshness.StaleReasons = unionReasons(staleReasons)
		if hideReasons.Valid {
			status.HideReasons = unionReasons(hideReasons.String)
		}

		statuses = append(statuses, status)
	}
	if err := rows.Err(); nil != err {
		const msg string = "failed after the repo status rows loop"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}

	return statuses, nil
}

// SpaceFreshness returns how up-to-date a space is, as a whole:
// the earliest verified-at time of the space and all its visible repos, and all their stale reasons.
func SpaceFreshness(logger log.Logger, db *sql.DB, spaceURI string) (Freshness, error) {
	log := logger.Begin()
	defer log.End()

	var nada Freshness

	if nil == db {
		return nada, erorr.Wrap(ErrNilDB, "failed to get space freshness")
	}

	const query string =
		`SELECT s.verified_at, s.stale_reasons,` +" "+
		`(SELECT COUNT(*) FROM repos AS rp WHERE rp.space_uri = s.space_uri AND rp.verified_at IS NULL AND ` + sqlRepoVisible + `),` +" "+
		`(SELECT MIN(rp.verified_at) FROM repos AS rp WHERE rp.space_uri = s.space_uri AND ` + sqlRepoVisible + `),` +" "+
		`(SELECT group_concat(rp.stale_reasons, ' ') FROM repos AS rp WHERE rp.space_uri = s.space_uri AND ` + sqlRepoVisible + `)` +" "+
		`FROM spaces AS s WHERE s.space_uri = ?`

	var spaceVerifiedAt sql.NullInt64
	var spaceStaleReasons string
	var neverVerified int
	var minRepoVerifiedAt sql.NullInt64
	var repoStaleReasons sql.NullString

	err := db.QueryRow(query, spaceURI).Scan(&spaceVerifiedAt, &spaceStaleReasons, &neverVerified, &minRepoVerifiedAt, &repoStaleReasons)
	if erorr.Is(err, sql.ErrNoRows) {
		return nada, nil
	}
	if nil != err {
		const msg string = "failed to get space freshness"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI))
		return nada, erorr.Wrap(err, msg, field.String("space-uri", spaceURI))
	}

	var freshness Freshness

	if spaceVerifiedAt.Valid && 0 == neverVerified {
		var verifiedAt int64 = spaceVerifiedAt.Int64
		if minRepoVerifiedAt.Valid && minRepoVerifiedAt.Int64 < verifiedAt {
			verifiedAt = minRepoVerifiedAt.Int64
		}
		freshness.VerifiedAt = opt.Something(verifiedAt)
	}

	freshness.StaleReasons = unionReasons(spaceStaleReasons, repoStaleReasons.String)

	return freshness, nil
}
