package libdb

import (
	"database/sql"
	"strings"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
)

// VerifySettings checks that the SQLite settings the purge procedure depends on are actually in effect
// on a connection from 'db'. If any is not, it returns an error, and the caller should fail fast.
//
// In particular, auto_vacuum only takes effect if it was set before the first table was created.
// A database file created some other way would silently never return the pages a purge frees.
//
// See also:
//
//	• [DSN]
func VerifySettings(logger log.Logger, db *sql.DB) error {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		log.Alert(field.S("nil db"))
		var err error = ErrNilDB
		err = erorr.Wrap(err, "failed to verify database settings")
		return err
	}

	var expectations = []struct{
		Pragma string
		Expected string
	}{
		{
			Pragma: "auto_vacuum",
			Expected: "2", // 2 = INCREMENTAL
		},
		{
			Pragma: "secure_delete",
			Expected: "1",
		},
		{
			Pragma: "journal_mode",
			Expected: "wal",
		},
	}

	for _, expectation := range expectations {
		var actual string

		err := db.QueryRow(`PRAGMA ` + expectation.Pragma).Scan(&actual)
		if nil != err {
			const msg string = "failed to query database setting"
			log.Error(
				field.S(msg),
				field.E(err),
				field.String("pragma", expectation.Pragma),
			)
			err = erorr.Wrap(err, msg,
				field.String("pragma", expectation.Pragma),
			)
			return err
		}

		if strings.ToLower(actual) != expectation.Expected {
			var err error = ErrSettingNotApplied
			log.Alert(
				field.S("database setting not applied"),
				field.String("pragma", expectation.Pragma),
				field.String("expected", expectation.Expected),
				field.String("actual", actual),
			)
			err = erorr.Wrap(err, "database setting not applied",
				field.String("pragma", expectation.Pragma),
				field.String("expected", expectation.Expected),
				field.String("actual", actual),
			)
			return err
		}
	}

	return nil
}
