package libdb

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"strings"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
)

// KnownSpaces returns every space in the database, and whether it is listed.
func KnownSpaces(logger log.Logger, db *sql.DB) (map[string]bool, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return nil, erorr.Wrap(ErrNilDB, "failed to get known spaces")
	}

	rows, err := db.Query(`SELECT space_uri, listed FROM spaces`)
	if nil != err {
		const msg string = "failed to get known spaces"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}
	defer rows.Close()

	var known = map[string]bool{}
	for rows.Next() {
		var spaceURI string
		var listed bool
		if err := rows.Scan(&spaceURI, &listed); nil != err {
			const msg string = "failed to scan space row"
			log.Error(field.S(msg), field.E(err))
			return nil, erorr.Wrap(err, msg)
		}
		known[spaceURI] = listed
	}
	if err := rows.Err(); nil != err {
		const msg string = "failed after the space rows loop"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}

	return known, nil
}

// ApplySpacesList applies a change to the list of mirrored spaces, in one transaction:
//
//	• 'added' spaces are created — lifecycle "unavailable" (until they are authorized), with a new epoch and a new notify token,
//	• 'removed' spaces are unlisted — given the "unlisted" hide reason, to be purged at 'purgeAfter' (unix seconds),
//	• 'relisted' spaces (unlisted, and now listed again) lose the "unlisted" hide reason.
//
// It returns whether anything was appended to the change log.
func ApplySpacesList(logger log.Logger, db *sql.DB, added []string, removed []string, relisted []string, now int64, purgeAfter int64) (changelogged bool, err error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return false, erorr.Wrap(ErrNilDB, "failed to apply spaces list")
	}

	tx, err := db.Begin()
	if nil != err {
		const msg string = "failed to begin transaction"
		log.Error(field.S(msg), field.E(err))
		return false, erorr.Wrap(err, msg)
	}
	defer tx.Rollback()

	var seqBefore int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM changelog`).Scan(&seqBefore); nil != err {
		const msg string = "failed to read the change-log high-water mark"
		log.Error(field.S(msg), field.E(err))
		return false, erorr.Wrap(err, msg)
	}

	for _, spaceURI := range added {
		authority, spaceType, skey, valid := splitSpaceRef(spaceURI)
		if !valid {
			return false, erorr.Wrap(ErrBadSpaceRef, "cannot add space", field.String("space-uri", spaceURI))
		}

		epoch, err := nextEpoch(tx)
		if nil != err {
			log.Error(field.S("failed to get a new epoch"), field.E(err))
			return false, err
		}

		notifyToken, err := newNotifyToken()
		if nil != err {
			log.Error(field.S("failed to make a notify token"), field.E(err))
			return false, err
		}

		_, err = tx.Exec(
			`INSERT INTO spaces (space_uri, authority, type, skey, listed, lifecycle, epoch, notify_token) VALUES (?, ?, ?, ?, 1, 'unavailable', ?, ?)`,
			spaceURI, authority, spaceType, skey, epoch, notifyToken,
		)
		if nil != err {
			const msg string = "failed to add space"
			log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI))
			return false, erorr.Wrap(err, msg, field.String("space-uri", spaceURI))
		}
	}

	for _, spaceURI := range removed {
		if _, err := tx.Exec(`UPDATE spaces SET listed = 0 WHERE space_uri = ?`, spaceURI); nil != err {
			const msg string = "failed to unlist space"
			log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI))
			return false, erorr.Wrap(err, msg, field.String("space-uri", spaceURI))
		}

		var scope = HideScope{Kind: ScopeSpace, SpaceURI: spaceURI}
		if _, err := changeHideTx(log, tx, scope, ReasonUnlisted, now, true, sql.NullInt64{Int64: purgeAfter, Valid: true}); nil != err {
			return false, err
		}
	}

	for _, spaceURI := range relisted {
		if _, err := tx.Exec(`UPDATE spaces SET listed = 1, lifecycle = CASE WHEN 'purged' = lifecycle THEN 'unavailable' ELSE lifecycle END WHERE space_uri = ?`, spaceURI); nil != err {
			const msg string = "failed to re-list space"
			log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI))
			return false, erorr.Wrap(err, msg, field.String("space-uri", spaceURI))
		}

		var scope = HideScope{Kind: ScopeSpace, SpaceURI: spaceURI}
		if _, err := changeHideTx(log, tx, scope, ReasonUnlisted, now, false, sql.NullInt64{}); nil != err {
			return false, err
		}
	}

	var seqAfter int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM changelog`).Scan(&seqAfter); nil != err {
		const msg string = "failed to read the change-log high-water mark"
		log.Error(field.S(msg), field.E(err))
		return false, erorr.Wrap(err, msg)
	}

	if err := tx.Commit(); nil != err {
		const msg string = "failed to commit transaction"
		log.Error(field.S(msg), field.E(err))
		return false, erorr.Wrap(err, msg)
	}

	return seqBefore != seqAfter, nil
}

// splitSpaceRef splits "at://{authority}/space/{type}/{skey}".
func splitSpaceRef(spaceURI string) (authority string, spaceType string, skey string, valid bool) {
	const prefix string = "at://"
	if !strings.HasPrefix(spaceURI, prefix) {
		return "", "", "", false
	}

	var parts []string = strings.Split(spaceURI[len(prefix):], "/")
	if 4 != len(parts) || "space" != parts[1] || "" == parts[0] || "" == parts[2] || "" == parts[3] {
		return "", "", "", false
	}

	return parts[0], parts[2], parts[3], true
}

// nextEpoch takes the next value from the mirror-wide epoch counter (so epochs never repeat).
func nextEpoch(tx *sql.Tx) (int64, error) {
	if _, err := tx.Exec(`UPDATE meta SET epoch_counter = epoch_counter + 1 WHERE id = 1`); nil != err {
		return 0, erorr.Wrap(err, "failed to advance the epoch counter")
	}

	var epoch int64
	if err := tx.QueryRow(`SELECT epoch_counter FROM meta WHERE id = 1`).Scan(&epoch); nil != err {
		return 0, erorr.Wrap(err, "failed to read the epoch counter")
	}

	return epoch, nil
}

// newNotifyToken makes a random notify token (32 bytes, base64url) — what the space host must send back with notifications.
func newNotifyToken() (string, error) {
	var p [32]byte
	if _, err := rand.Read(p[:]); nil != err {
		return "", erorr.Wrap(err, "failed to read random bytes")
	}
	return base64.RawURLEncoding.EncodeToString(p[:]), nil
}
