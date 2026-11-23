package libdb

import (
	"database/sql"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
)

// The scopes a hide can apply to.
const (
	ScopeSpace   = "space"
	ScopeAccount = "account"
	ScopeRepo    = "repo"
	ScopeRecord  = "record"
)

// The hide reasons.
const (
	ReasonPurging                   = "purging"
	ReasonDenied                    = "denied"
	ReasonDenialUnresolved          = "denial-unresolved"
	ReasonUnlisted                  = "unlisted"
	ReasonAuthoritySuspended        = "authority-suspended"
	ReasonAccountInactiveHosting    = "account-inactive:hosting"
	ReasonAccountInactiveRelay      = "account-inactive:relay"
	ReasonAccountDeletedUnconfirmed = "account-deleted-unconfirmed"
	ReasonOperator                  = "operator"
	ReasonLabelTakedown             = "label-takedown"
	ReasonOverQuota                 = "over-quota"
)

// HideScope is what a hide applies to.
//
//	• space:   SpaceURI
//	• account: DID — in every space
//	• repo:    SpaceURI + DID
//	• record:  SpaceURI + DID + Collection + RKey (+ CID, to hide only that version)
type HideScope struct {
	Kind       string
	SpaceURI   string
	DID        string
	Collection string
	RKey       string
	CID        string
}

// Validate returns an error if the scope's fields don't fit its kind.
func (receiver HideScope) Validate() error {
	var has = func(s string) bool { return "" != s }

	var valid bool
	switch receiver.Kind {
	case ScopeSpace:
		valid = has(receiver.SpaceURI) && !has(receiver.DID) && !has(receiver.Collection) && !has(receiver.RKey) && !has(receiver.CID)
	case ScopeAccount:
		valid = !has(receiver.SpaceURI) && has(receiver.DID) && !has(receiver.Collection) && !has(receiver.RKey) && !has(receiver.CID)
	case ScopeRepo:
		valid = has(receiver.SpaceURI) && has(receiver.DID) && !has(receiver.Collection) && !has(receiver.RKey) && !has(receiver.CID)
	case ScopeRecord:
		valid = has(receiver.SpaceURI) && has(receiver.DID) && has(receiver.Collection) && has(receiver.RKey)
	}

	if !valid {
		return erorr.Wrap(ErrBadHideScope, "bad hide scope", field.String("kind", receiver.Kind))
	}
	return nil
}

// AddHide adds a hide reason to a scope — in one transaction — and returns whether anything changed
// (false if the scope already had that reason).
//
// If the scope was visible before, and is now hidden, a "hidden" entry is appended to the change log
// (so consumers drop what they hold for the scope).
// Adding a second reason to something already hidden appends nothing.
func AddHide(logger log.Logger, db *sql.DB, scope HideScope, reason string, now int64) (changed bool, err error) {
	return changeHide(logger, db, scope, reason, now, true)
}

// RemoveHide removes a hide reason from a scope — in one transaction — and returns whether anything changed
// (false if the scope didn't have that reason).
//
// If the scope was hidden before, and is now visible (no other reason, at it or at an enclosing scope, still hides it),
// a "visible" entry is appended to the change log (so consumers re-list the scope).
func RemoveHide(logger log.Logger, db *sql.DB, scope HideScope, reason string, now int64) (changed bool, err error) {
	return changeHide(logger, db, scope, reason, now, false)
}

func changeHide(logger log.Logger, db *sql.DB, scope HideScope, reason string, now int64, add bool) (bool, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return false, erorr.Wrap(ErrNilDB, "failed to change hide")
	}

	tx, err := db.Begin()
	if nil != err {
		const msg string = "failed to begin transaction"
		log.Error(field.S(msg), field.E(err))
		return false, erorr.Wrap(err, msg)
	}
	defer tx.Rollback()

	changed, err := changeHideTx(log, tx, scope, reason, now, add, sql.NullInt64{})
	if nil != err {
		return false, err
	}
	if !changed {
		return false, nil
	}

	if err := tx.Commit(); nil != err {
		const msg string = "failed to commit transaction"
		log.Error(field.S(msg), field.E(err))
		return false, erorr.Wrap(err, msg)
	}

	return true, nil
}

// changeHideTx adds (or removes) a hide reason to (or from) a scope, inside the caller's transaction,
// appending a "hidden" / "visible" change-log entry if — and only if — the scope's visibility flips.
// 'purgeAfter' (when valid) is stored with an added hide (for reasons that lead to a purge).
func changeHideTx(log log.Logger, tx *sql.Tx, scope HideScope, reason string, now int64, add bool, purgeAfter sql.NullInt64) (bool, error) {
	if err := scope.Validate(); nil != err {
		return false, err
	}

	var fields = []field.StringlyField{
		field.String("scope-kind", scope.Kind),
		field.String("space-uri", scope.SpaceURI),
		field.String("did", scope.DID),
		field.String("collection", scope.Collection),
		field.String("rkey", scope.RKey),
		field.String("cid", scope.CID),
		field.String("reason", reason),
		field.Bool("add", add),
	}

	before, err := scopeVisible(tx, scope)
	if nil != err {
		log.Error(append([]field.StringlyField{field.S("failed to check visibility"), field.E(err)}, fields...)...)
		return false, erorr.Wrap(err, "failed to check visibility", fields...)
	}

	var result sql.Result
	if add {
		result, err = tx.Exec(
			`INSERT OR IGNORE INTO hides (scope_kind, space_uri, did, collection, rkey, cid, reason, since, purge_after) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			scope.Kind, scope.SpaceURI, scope.DID, scope.Collection, scope.RKey, scope.CID, reason, now, purgeAfter,
		)
	} else {
		result, err = tx.Exec(
			`DELETE FROM hides WHERE scope_kind = ? AND space_uri = ? AND did = ? AND collection = ? AND rkey = ? AND cid = ? AND reason = ?`,
			scope.Kind, scope.SpaceURI, scope.DID, scope.Collection, scope.RKey, scope.CID, reason,
		)
	}
	if nil != err {
		const msg string = "failed to change hide"
		log.Error(append([]field.StringlyField{field.S(msg), field.E(err)}, fields...)...)
		return false, erorr.Wrap(err, msg, fields...)
	}

	affected, err := result.RowsAffected()
	if nil != err {
		const msg string = "failed to get rows affected"
		log.Error(append([]field.StringlyField{field.S(msg), field.E(err)}, fields...)...)
		return false, erorr.Wrap(err, msg, fields...)
	}
	if 0 == affected {
		return false, nil
	}

	after, err := scopeVisible(tx, scope)
	if nil != err {
		log.Error(append([]field.StringlyField{field.S("failed to check visibility"), field.E(err)}, fields...)...)
		return false, erorr.Wrap(err, "failed to check visibility", fields...)
	}

	if before != after {
		var kind string = ChangeKindHidden
		if after {
			kind = ChangeKindVisible
		}

		_, err := appendChange(tx, Change{
			At:         now,
			Kind:       kind,
			Scope:      scope.Kind,
			SpaceURI:   scope.SpaceURI,
			DID:        scope.DID,
			Collection: scope.Collection,
			RKey:       scope.RKey,
			CID:        scope.CID,
			Reason:     reason,
		})
		if nil != err {
			log.Error(append([]field.StringlyField{field.S("failed to append change"), field.E(err)}, fields...)...)
			return false, erorr.Wrap(err, "failed to append change", fields...)
		}
	}

	return true, nil
}

// scopeVisible returns whether a scope is visible — counting hides at it and at every enclosing scope.
//
// (For a record scope, it is the record as served — see below — whatever the scope's CID.)
func scopeVisible(tx *sql.Tx, scope HideScope) (bool, error) {
	var query string
	var args []any

	const spaceVisible string =
		`EXISTS (SELECT 1 FROM spaces AS s WHERE s.space_uri = ? AND ` + sqlSpaceVisible + `)`
	const accountVisible string =
		`NOT EXISTS (SELECT 1 FROM hides AS h WHERE h.scope_kind = 'account' AND h.space_uri = '' AND h.did = ? AND h.collection = '' AND h.rkey = '' AND h.cid = '')`
	const repoNotHidden string =
		`NOT EXISTS (SELECT 1 FROM hides AS h WHERE h.scope_kind = 'repo' AND h.space_uri = ? AND h.did = ? AND h.collection = '' AND h.rkey = '' AND h.cid = '')`
	const currentRecordVisible string =
		`EXISTS (SELECT 1 FROM records AS r JOIN repos AS rp ON rp.space_uri = r.space_uri AND rp.did = r.did` +
		` WHERE r.space_uri = ? AND r.did = ? AND r.collection = ? AND r.rkey = ? AND ` + sqlRecordVisible + `)`

	switch scope.Kind {
	case ScopeSpace:
		query = `SELECT ` + spaceVisible
		args = []any{scope.SpaceURI}
	case ScopeAccount:
		query = `SELECT ` + accountVisible
		args = []any{scope.DID}
	case ScopeRepo:
		query = `SELECT ` + spaceVisible + ` AND ` + accountVisible + ` AND ` + repoNotHidden
		args = []any{scope.SpaceURI, scope.DID, scope.SpaceURI, scope.DID}
	case ScopeRecord:
		// A record is judged as it is served: its current version, in its repo's current generation.
		// (So a hide of a version that is not the current one — or of a record not here (yet) — flips nothing.)
		query = `SELECT ` + spaceVisible + ` AND ` + accountVisible + ` AND ` + repoNotHidden + ` AND ` + currentRecordVisible
		args = []any{scope.SpaceURI, scope.DID, scope.SpaceURI, scope.DID, scope.SpaceURI, scope.DID, scope.Collection, scope.RKey}
	default:
		return false, ErrBadHideScope
	}

	var visible bool
	err := tx.QueryRow(query, args...).Scan(&visible)
	if nil != err {
		return false, err
	}

	return visible, nil
}
