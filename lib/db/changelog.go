package libdb

import (
	"database/sql"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
)

// The kinds of change-log entries.
const (
	ChangeKindRecord        = "record"
	ChangeKindRepoReplaced  = "repoReplaced"
	ChangeKindHidden        = "hidden"
	ChangeKindVisible       = "visible"
	ChangeKindWriterRemoved = "writerRemoved"
	ChangeKindSpacePurged   = "spacePurged"
	ChangeKindAccountPurged = "accountPurged"
	ChangeKindAccountStatus = "accountStatus"
	ChangeKindLabel         = "label"
	ChangeKindFreshness     = "freshness"
)

// Change is a change-log entry. It holds references only — never record values.
type Change struct {
	Seq int64
	At  int64

	Kind  string
	Scope string

	SpaceURI   string
	DID        string // the subject
	Collection string
	RKey       string
	CID        string
	Rev        string
	Reason     string
}

// appendChange adds an entry to the change log, inside the caller's transaction, and returns its seq.
func appendChange(tx *sql.Tx, change Change) (int64, error) {
	result, err := tx.Exec(
		`INSERT INTO changelog (at, kind, scope, space_uri, did, collection, rkey, cid, rev, reason)` +" "+
		`VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		change.At, change.Kind, change.Scope, change.SpaceURI, change.DID, change.Collection, change.RKey, change.CID, change.Rev, change.Reason,
	)
	if nil != err {
		return 0, erorr.Wrap(err, "failed to append to the change log")
	}

	seq, err := result.LastInsertId()
	if nil != err {
		return 0, erorr.Wrap(err, "failed to get the seq of the new change-log entry")
	}

	return seq, nil
}

// ChangesList returns up to 'limit' change-log entries with a seq greater than 'afterSeq', in seq order.
func ChangesList(logger log.Logger, db *sql.DB, afterSeq int64, limit int) ([]Change, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return nil, erorr.Wrap(ErrNilDB, "failed to list changes")
	}

	rows, err := db.Query(
		`SELECT seq, at, kind, scope, space_uri, did, collection, rkey, cid, rev, reason FROM changelog` +" "+
		`WHERE seq > ? ORDER BY seq ASC LIMIT ?`,
		afterSeq, limit,
	)
	if nil != err {
		const msg string = "failed to list changes"
		log.Error(field.S(msg), field.E(err), field.Int64("after-seq", afterSeq))
		return nil, erorr.Wrap(err, msg, field.Int64("after-seq", afterSeq))
	}
	defer rows.Close()

	var changes []Change
	for rows.Next() {
		var change Change
		if err := rows.Scan(&change.Seq, &change.At, &change.Kind, &change.Scope, &change.SpaceURI, &change.DID, &change.Collection, &change.RKey, &change.CID, &change.Rev, &change.Reason); nil != err {
			const msg string = "failed to scan change row"
			log.Error(field.S(msg), field.E(err))
			return nil, erorr.Wrap(err, msg)
		}
		changes = append(changes, change)
	}
	if err := rows.Err(); nil != err {
		const msg string = "failed after the change rows loop"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}

	return changes, nil
}

// ChangelogBounds returns the change log's retention floor (a cursor below it is too old)
// and its high-water mark (the largest seq; 0 if empty).
func ChangelogBounds(logger log.Logger, db *sql.DB) (floorSeq int64, maxSeq int64, err error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return 0, 0, erorr.Wrap(ErrNilDB, "failed to get change-log bounds")
	}

	err = db.QueryRow(
		`SELECT (SELECT changelog_floor_seq FROM meta WHERE id = 1), (SELECT COALESCE(MAX(seq), 0) FROM changelog)`,
	).Scan(&floorSeq, &maxSeq)
	if nil != err {
		const msg string = "failed to get change-log bounds"
		log.Error(field.S(msg), field.E(err))
		return 0, 0, erorr.Wrap(err, msg)
	}

	return floorSeq, maxSeq, nil
}
