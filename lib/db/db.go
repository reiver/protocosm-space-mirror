package libdb

import (
	"database/sql"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
)

// codeCreate is the space-mirror's schema.
//
// Notes:
//
//	• There are no foreign keys. Purges delete in an explicit order, in chunks.
//	• In the 'hides' and 'purge_jobs' tables, every primary-key column is NOT NULL, with '' meaning "not applicable".
//	  (SQLite allows NULL in a non-integer primary key and treats NULLs as distinct, which would allow duplicate rows.)
//	• Account-scope rows (in 'hides' and 'purge_jobs') have space_uri = ''.
//	• Epochs (spaces.epoch, repos.epoch, repos.target_epoch, accounts.purge_epoch, purge_jobs.epoch) are all taken from meta.epoch_counter,
//	  so they never repeat, even when rows are deleted and re-created.
//	• The change-log holds references only — never record values. Its 'did' column is the event's subject.
//	• Times are unix seconds.
const codeCreate string =
`CREATE TABLE IF NOT EXISTS meta (
	id INTEGER PRIMARY KEY CHECK (1 = id),

	epoch_counter       INTEGER NOT NULL DEFAULT 0,
	changelog_floor_seq INTEGER NOT NULL DEFAULT 0,
	firehose_cursor     INTEGER
);

INSERT OR IGNORE INTO meta (id) VALUES (1);

CREATE TABLE IF NOT EXISTS spaces (
	space_uri TEXT PRIMARY KEY,
	when_created INTEGER DEFAULT (strftime('%s','now')),

	authority TEXT NOT NULL,
	type      TEXT NOT NULL,
	skey      TEXT NOT NULL,

	listed      BOOLEAN NOT NULL DEFAULT 1,
	lifecycle   TEXT    NOT NULL DEFAULT 'unavailable' CHECK (lifecycle IN ('unavailable','active','purged')),
	had_success BOOLEAN NOT NULL DEFAULT 0,
	epoch       INTEGER NOT NULL,

	space_rev          TEXT NOT NULL DEFAULT '',
	space_rev_seen_at  INTEGER,
	last_catchup_at    INTEGER,
	last_authorized_at INTEGER,

	notify_token TEXT NOT NULL,

	verified_at   INTEGER,
	stale_reasons TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS repos (
	space_uri TEXT NOT NULL,
	did       TEXT NOT NULL,

	host       TEXT    NOT NULL DEFAULT '',
	epoch      INTEGER NOT NULL,
	generation INTEGER NOT NULL DEFAULT 0,

	rev    TEXT NOT NULL DEFAULT '',
	data   BLOB,
	lthash BLOB,

	target_rev       TEXT    NOT NULL DEFAULT '',
	target_data      BLOB,
	target_space_rev TEXT    NOT NULL DEFAULT '',
	target_epoch     INTEGER NOT NULL DEFAULT 0,

	verified_at   INTEGER,
	stale_reasons TEXT NOT NULL DEFAULT '',

	PRIMARY KEY (space_uri, did)
);

CREATE TABLE IF NOT EXISTS records (
	space_uri  TEXT    NOT NULL,
	did        TEXT    NOT NULL,
	generation INTEGER NOT NULL,
	collection TEXT    NOT NULL,
	rkey       TEXT    NOT NULL,

	cid   TEXT NOT NULL,
	drisl BLOB NOT NULL,

	PRIMARY KEY (space_uri, did, generation, collection, rkey)
);

CREATE INDEX IF NOT EXISTS records_by_collection ON records (space_uri, collection, did, generation, rkey);

CREATE TABLE IF NOT EXISTS hides (
	scope_kind TEXT NOT NULL CHECK (scope_kind IN ('space','account','repo','record')),
	space_uri  TEXT NOT NULL DEFAULT '',
	did        TEXT NOT NULL DEFAULT '',
	collection TEXT NOT NULL DEFAULT '',
	rkey       TEXT NOT NULL DEFAULT '',
	cid        TEXT NOT NULL DEFAULT '',
	reason     TEXT NOT NULL CHECK (reason IN (
		'purging',
		'denied',
		'denial-unresolved',
		'unlisted',
		'authority-suspended',
		'account-inactive:hosting',
		'account-inactive:relay',
		'account-deleted-unconfirmed',
		'operator',
		'label-takedown',
		'over-quota'
	)),

	since       INTEGER NOT NULL,
	purge_after INTEGER,

	PRIMARY KEY (scope_kind, space_uri, did, collection, rkey, cid, reason)
);

CREATE INDEX IF NOT EXISTS hides_by_did ON hides (did);

CREATE TABLE IF NOT EXISTS purge_jobs (
	scope_kind TEXT NOT NULL CHECK (scope_kind IN ('space','account','repo')),
	space_uri  TEXT NOT NULL DEFAULT '',
	did        TEXT NOT NULL DEFAULT '',

	reason TEXT NOT NULL CHECK (reason IN (
		'space-deleted',
		'account-deleted',
		'authority-deleted',
		'denied',
		'unlisted',
		'writer-removed'
	)),
	epoch      INTEGER NOT NULL,
	started_at INTEGER NOT NULL,
	event_seq  INTEGER,

	PRIMARY KEY (scope_kind, space_uri, did)
);

CREATE TABLE IF NOT EXISTS changelog (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	at  INTEGER NOT NULL,

	kind       TEXT NOT NULL,
	scope      TEXT NOT NULL DEFAULT '',
	space_uri  TEXT NOT NULL DEFAULT '',
	did        TEXT NOT NULL DEFAULT '',
	collection TEXT NOT NULL DEFAULT '',
	rkey       TEXT NOT NULL DEFAULT '',
	cid        TEXT NOT NULL DEFAULT '',
	rev        TEXT NOT NULL DEFAULT '',
	reason     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS changelog_by_space_did ON changelog (space_uri, did);
CREATE INDEX IF NOT EXISTS changelog_by_did_seq   ON changelog (did, seq);

CREATE TABLE IF NOT EXISTS accounts (
	did TEXT PRIMARY KEY,

	status            TEXT    NOT NULL DEFAULT '',
	status_checked_at INTEGER,
	pds               TEXT    NOT NULL DEFAULT '',
	kem_multikey      TEXT    NOT NULL DEFAULT '',
	doc_fetched_at    INTEGER,
	purge_epoch       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS blobs (
	cid TEXT PRIMARY KEY,

	size  INTEGER NOT NULL,
	mime  TEXT    NOT NULL DEFAULT '',
	state TEXT    NOT NULL CHECK (state IN ('fetched','deferred','failed','deleting')),
	path  TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS blob_refs (
	space_uri  TEXT    NOT NULL,
	did        TEXT    NOT NULL,
	generation INTEGER NOT NULL,
	collection TEXT    NOT NULL,
	rkey       TEXT    NOT NULL,
	cid        TEXT    NOT NULL,

	PRIMARY KEY (space_uri, did, generation, collection, rkey, cid)
);

CREATE INDEX IF NOT EXISTS blob_refs_by_cid ON blob_refs (cid);

CREATE TABLE IF NOT EXISTS labels (
	space_uri  TEXT    NOT NULL,
	src_did    TEXT    NOT NULL,
	generation INTEGER NOT NULL,
	collection TEXT    NOT NULL,
	rkey       TEXT    NOT NULL,

	uri TEXT NOT NULL,
	cid TEXT NOT NULL DEFAULT '',
	val TEXT NOT NULL,
	cts TEXT NOT NULL,
	exp TEXT,

	PRIMARY KEY (space_uri, src_did, generation, collection, rkey)
);

CREATE INDEX IF NOT EXISTS labels_by_subject ON labels (uri, cid, val);
`

// CreateDatabase creates the space-mirror's database table(s) and index(es), if they don't exist yet.
//
// It is idempotent.
func CreateDatabase(logger log.Logger, db *sql.DB) error {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		log.Alert(field.S("nil db"))
		var err error = ErrNilDB
		err = erorr.Wrap(err, "failed to create database table(s)")
		return err
	}

	_, err := db.Exec(codeCreate)
	if nil != err {
		const msg string = "failed to create database table(s)"
		log.Error(
			field.S(msg),
			field.E(err),
		)
		err = erorr.Wrap(err, msg)
		return err
	}

	return nil
}
