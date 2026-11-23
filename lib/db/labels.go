package libdb

import (
	"database/sql"
	"strings"
	"time"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
)

// LabelValTakedown is the label value the mirror enforces (when the space's authority applies it): the labelled record is hidden.
// Every other label is passed through.
const LabelValTakedown string = "!takedown"

// Label is a label record, as stored.
type Label struct {
	URI string // the subject
	CID string // "" for every version of the subject
	Val string
	CTS string
	Exp string // "" for none
}

// LabelKey is where a label record lives: in a space, in a repo (the labeller's), at a generation, at a collection and rkey.
type LabelKey struct {
	SpaceURI   string
	SrcDID     string
	Generation int64
	Collection string
	RKey       string
}

// PutLabelTx stores (or replaces) a label record, inside the caller's (fenced) write transaction,
// and brings the "label-takedown" hides of its subject — and of the subject it replaced, if any — up to date.
//
// It returns whether any hide changed.
func PutLabelTx(log log.Logger, tx *sql.Tx, key LabelKey, label Label, now int64) (bool, error) {
	oldURI, err := deleteLabelRowTx(tx, key)
	if nil != err {
		return false, err
	}

	var exp sql.NullString
	if "" != label.Exp {
		exp = sql.NullString{String: label.Exp, Valid: true}
	}

	if _, err := tx.Exec(
		`INSERT INTO labels (space_uri, src_did, generation, collection, rkey, uri, cid, val, cts, exp) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		key.SpaceURI, key.SrcDID, key.Generation, key.Collection, key.RKey, label.URI, label.CID, label.Val, label.CTS, exp,
	); nil != err {
		const msg string = "failed to store label"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", key.SpaceURI), field.String("src-did", key.SrcDID), field.String("rkey", key.RKey))
		return false, erorr.Wrap(err, msg)
	}

	var subjects = []string{label.URI}
	if "" != oldURI && oldURI != label.URI {
		subjects = append(subjects, oldURI)
	}

	return syncLabelHidesForSubjectsTx(log, tx, key.SpaceURI, subjects, now)
}

// DeleteLabelTx deletes a label record, inside the caller's (fenced) write transaction,
// and brings the "label-takedown" hides of its subject up to date.
//
// It returns whether any hide changed.
func DeleteLabelTx(log log.Logger, tx *sql.Tx, key LabelKey, now int64) (bool, error) {
	oldURI, err := deleteLabelRowTx(tx, key)
	if nil != err {
		return false, err
	}
	if "" == oldURI {
		return false, nil
	}

	return syncLabelHidesForSubjectsTx(log, tx, key.SpaceURI, []string{oldURI}, now)
}

// deleteLabelRowTx deletes a label row, and returns its subject ("" if there was no row).
func deleteLabelRowTx(tx *sql.Tx, key LabelKey) (string, error) {
	var uri string
	err := tx.QueryRow(
		`SELECT uri FROM labels WHERE space_uri = ? AND src_did = ? AND generation = ? AND collection = ? AND rkey = ?`,
		key.SpaceURI, key.SrcDID, key.Generation, key.Collection, key.RKey,
	).Scan(&uri)
	switch {
	case nil == err:
		// Nothing here.
	case erorr.Is(err, sql.ErrNoRows):
		return "", nil
	default:
		return "", erorr.Wrap(err, "failed to look up label")
	}

	if _, err := tx.Exec(
		`DELETE FROM labels WHERE space_uri = ? AND src_did = ? AND generation = ? AND collection = ? AND rkey = ?`,
		key.SpaceURI, key.SrcDID, key.Generation, key.Collection, key.RKey,
	); nil != err {
		return "", erorr.Wrap(err, "failed to delete label")
	}

	return uri, nil
}

// ReevaluateRecordTx brings the "label-takedown" hides of one record up to date, inside the caller's (fenced) write transaction.
//
// Call it on every record upsert, before deciding whether the record is visible:
// a label can arrive before its subject — and a purge of the subject's repo deletes its hides, while the label stays.
//
// It returns whether any hide changed.
func ReevaluateRecordTx(log log.Logger, tx *sql.Tx, spaceURI string, did string, collection string, rkey string, now int64) (bool, error) {
	return syncLabelHidesTx(log, tx, spaceURI, "at://"+did+"/"+collection+"/"+rkey, now)
}

// ReevaluateLabelsAfterFlipTx brings the "label-takedown" hides up to date after a repo's generation flipped,
// inside the flip's transaction: the subjects of the labels in the old generation, and in the (now current) new one.
//
// (It only does anything when the repo is the space's authority's — only those labels are enforced.)
//
// It returns whether any hide changed.
func ReevaluateLabelsAfterFlipTx(log log.Logger, tx *sql.Tx, spaceURI string, srcDID string, oldGeneration int64, now int64) (bool, error) {
	rows, err := tx.Query(
		`SELECT DISTINCT l.uri FROM labels AS l` +" "+
		`JOIN repos AS rp ON rp.space_uri = l.space_uri AND rp.did = l.src_did` +" "+
		`WHERE l.space_uri = ? AND l.src_did = ? AND l.val = ? AND l.generation IN (?, rp.generation)`,
		spaceURI, srcDID, LabelValTakedown, oldGeneration,
	)
	if nil != err {
		const msg string = "failed to list label subjects"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI), field.String("src-did", srcDID))
		return false, erorr.Wrap(err, msg)
	}

	var subjects []string
	for rows.Next() {
		var uri string
		if err := rows.Scan(&uri); nil != err {
			rows.Close()
			return false, erorr.Wrap(err, "failed to scan label subject")
		}
		subjects = append(subjects, uri)
	}
	rows.Close()
	if err := rows.Err(); nil != err {
		return false, erorr.Wrap(err, "failed after the label subject rows loop")
	}

	return syncLabelHidesForSubjectsTx(log, tx, spaceURI, subjects, now)
}

// SweepExpiredLabels removes the "label-takedown" hides whose labels have expired — by bringing the hides of every subject
// with such a hide up to date. Each subject gets its own short transaction. Subjects under a purge are left alone.
//
// It returns whether any hide changed.
func SweepExpiredLabels(logger log.Logger, db *sql.DB, now int64) (bool, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return false, erorr.Wrap(ErrNilDB, "failed to sweep expired labels")
	}

	type internalSubject struct{ spaceURI, uri string }
	var subjects []internalSubject
	{
		rows, err := db.Query(
			`SELECT DISTINCT h.space_uri, 'at://' || h.did || '/' || h.collection || '/' || h.rkey FROM hides AS h` +" "+
			`WHERE h.scope_kind = 'record' AND h.reason = ?` +" "+
			`AND NOT EXISTS (SELECT 1 FROM purge_jobs AS j WHERE` +
			` (j.scope_kind = 'space' AND j.space_uri = h.space_uri)` +
			` OR (j.scope_kind = 'repo' AND j.space_uri = h.space_uri AND j.did = h.did)` +
			` OR (j.scope_kind = 'account' AND j.did = h.did))`,
			ReasonLabelTakedown,
		)
		if nil != err {
			const msg string = "failed to list label-takedown hides"
			log.Error(field.S(msg), field.E(err))
			return false, erorr.Wrap(err, msg)
		}
		for rows.Next() {
			var subject internalSubject
			if err := rows.Scan(&subject.spaceURI, &subject.uri); nil != err {
				rows.Close()
				return false, erorr.Wrap(err, "failed to scan label-takedown hide")
			}
			subjects = append(subjects, subject)
		}
		rows.Close()
		if err := rows.Err(); nil != err {
			return false, erorr.Wrap(err, "failed after the label-takedown hide rows loop")
		}
	}

	var changed bool
	for _, subject := range subjects {
		subjectChanged, err := func() (bool, error) {
			tx, err := db.Begin()
			if nil != err {
				return false, erorr.Wrap(err, "failed to begin transaction")
			}
			defer tx.Rollback()

			changed, err := syncLabelHidesTx(log, tx, subject.spaceURI, subject.uri, now)
			if nil != err {
				return false, err
			}

			if err := tx.Commit(); nil != err {
				return false, erorr.Wrap(err, "failed to commit transaction")
			}
			return changed, nil
		}()
		if nil != err {
			log.Error(field.S("failed to sweep expired labels"), field.E(err), field.String("space-uri", subject.spaceURI), field.String("subject", subject.uri))
			return changed, err
		}
		changed = changed || subjectChanged
	}

	return changed, nil
}

func syncLabelHidesForSubjectsTx(log log.Logger, tx *sql.Tx, spaceURI string, subjects []string, now int64) (bool, error) {
	var changed bool
	for _, subject := range subjects {
		subjectChanged, err := syncLabelHidesTx(log, tx, spaceURI, subject, now)
		if nil != err {
			return changed, err
		}
		changed = changed || subjectChanged
	}
	return changed, nil
}

// syncLabelHidesTx makes the "label-takedown" hides of one subject (in one space) match the labels:
// for each version the space's authority labelled "!takedown" (in its repo's current generation) —
// the newest such label (by "cts") for that version, if not expired — the subject has a hide; and no other.
//
// A subject that is not a record (ex: an account), or not named by DID, is passed through: it gets no hide.
//
// It returns whether any hide changed.
func syncLabelHidesTx(log log.Logger, tx *sql.Tx, spaceURI string, subject string, now int64) (bool, error) {
	did, collection, rkey, ok := splitRecordURI(subject)
	if !ok {
		return false, nil
	}

	var fields = []field.StringlyField{
		field.String("space-uri", spaceURI),
		field.String("subject", subject),
	}

	// The versions that should be hidden ("" for every version).
	var wanted = map[string]struct{}{}
	{
		rows, err := tx.Query(
			`SELECT l.cid, l.cts, COALESCE(l.exp, '') FROM labels AS l` +" "+
			`JOIN spaces AS s ON s.space_uri = l.space_uri AND s.authority = l.src_did` +" "+
			`JOIN repos AS rp ON rp.space_uri = l.space_uri AND rp.did = l.src_did AND rp.generation = l.generation` +" "+
			`WHERE l.space_uri = ? AND l.uri = ? AND l.val = ?`,
			spaceURI, subject, LabelValTakedown,
		)
		if nil != err {
			const msg string = "failed to look up labels"
			log.Error(append([]field.StringlyField{field.S(msg), field.E(err)}, fields...)...)
			return false, erorr.Wrap(err, msg, fields...)
		}

		type internalNewest struct{ cts, exp string }
		var newest = map[string]internalNewest{}
		for rows.Next() {
			var cid, cts, exp string
			if err := rows.Scan(&cid, &cts, &exp); nil != err {
				rows.Close()
				return false, erorr.Wrap(err, "failed to scan label row", fields...)
			}
			if current, found := newest[cid]; !found || newerTimestamp(cts, current.cts) {
				newest[cid] = internalNewest{cts: cts, exp: exp}
			}
		}
		rows.Close()
		if err := rows.Err(); nil != err {
			return false, erorr.Wrap(err, "failed after the label rows loop", fields...)
		}

		for cid, label := range newest {
			if !expired(label.exp, now) {
				wanted[cid] = struct{}{}
			}
		}
	}

	// The versions that are hidden.
	var have = map[string]struct{}{}
	{
		rows, err := tx.Query(
			`SELECT cid FROM hides WHERE scope_kind = 'record' AND space_uri = ? AND did = ? AND collection = ? AND rkey = ? AND reason = ?`,
			spaceURI, did, collection, rkey, ReasonLabelTakedown,
		)
		if nil != err {
			const msg string = "failed to look up label-takedown hides"
			log.Error(append([]field.StringlyField{field.S(msg), field.E(err)}, fields...)...)
			return false, erorr.Wrap(err, msg, fields...)
		}
		for rows.Next() {
			var cid string
			if err := rows.Scan(&cid); nil != err {
				rows.Close()
				return false, erorr.Wrap(err, "failed to scan hide row", fields...)
			}
			have[cid] = struct{}{}
		}
		rows.Close()
		if err := rows.Err(); nil != err {
			return false, erorr.Wrap(err, "failed after the hide rows loop", fields...)
		}
	}

	var changed bool
	var change = func(cid string, add bool) error {
		var scope = HideScope{Kind: ScopeRecord, SpaceURI: spaceURI, DID: did, Collection: collection, RKey: rkey, CID: cid}
		hideChanged, err := changeHideTx(log, tx, scope, ReasonLabelTakedown, now, add, sql.NullInt64{})
		if nil != err {
			return err
		}
		changed = changed || hideChanged
		return nil
	}
	for cid := range wanted {
		if _, found := have[cid]; !found {
			if err := change(cid, true); nil != err {
				return changed, err
			}
		}
	}
	for cid := range have {
		if _, found := wanted[cid]; !found {
			if err := change(cid, false); nil != err {
				return changed, err
			}
		}
	}

	return changed, nil
}

// splitRecordURI splits "at://{did}/{collection}/{rkey}".
func splitRecordURI(uri string) (did string, collection string, rkey string, ok bool) {
	const prefix string = "at://"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", "", false
	}

	parts := strings.Split(uri[len(prefix):], "/")
	if 3 != len(parts) {
		return "", "", "", false
	}
	if !strings.HasPrefix(parts[0], "did:") || "" == parts[1] || "" == parts[2] {
		return "", "", "", false
	}

	return parts[0], parts[1], parts[2], true
}

// newerTimestamp returns whether timestamp 'a' is after timestamp 'b'.
// (Timestamps that don't parse are compared as strings.)
func newerTimestamp(a string, b string) bool {
	timeA, errA := time.Parse(time.RFC3339Nano, a)
	timeB, errB := time.Parse(time.RFC3339Nano, b)
	if nil != errA || nil != errB {
		return a > b
	}
	return timeA.After(timeB)
}

// expired returns whether an "exp" timestamp has passed.
// No "exp" never expires; nor does one that doesn't parse — a takedown errs on the side of hiding.
func expired(exp string, now int64) bool {
	if "" == exp {
		return false
	}

	t, err := time.Parse(time.RFC3339Nano, exp)
	if nil != err {
		return false
	}

	return t.Unix() <= now
}
