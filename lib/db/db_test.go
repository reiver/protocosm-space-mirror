package libdb_test

import (
	"database/sql"
	"io"
	"path/filepath"
	"testing"

	"codeberg.org/reiver/go-log"

	_ "github.com/mattn/go-sqlite3"

	"protocosm-space-mirror/lib/db"
)

func openTestDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", dsn)
	if nil != err {
		t.Fatalf("Did not expect an error when opening the database but actually got one: (%T) %s", err, err)
	}
	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func TestCreateDatabase(t *testing.T) {
	var logger log.Logger = log.CreateLogger(io.Discard, "v")

	var filename string = filepath.Join(t.TempDir(), "test.sqlite3")
	var db *sql.DB = openTestDB(t, libdb.DSN(filename))

	// Calling it more than once must be fine (idempotent).
	for attempt := 0; attempt < 2; attempt++ {
		err := libdb.CreateDatabase(logger, db)
		if nil != err {
			t.Errorf("For attempt #%d, did not expect an error but actually got one.", attempt)
			t.Logf("ERROR: (%T) %s", err, err)
			continue
		}
	}

	tables := []string{
		"meta",
		"spaces",
		"repos",
		"records",
		"hides",
		"purge_jobs",
		"changelog",
		"accounts",
		"blobs",
		"blob_refs",
		"labels",
	}

	for testNumber, table := range tables {
		var count int

		err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count)
		if nil != err {
			t.Errorf("For test #%d, did not expect an error but actually got one.", testNumber)
			t.Logf("ERROR: (%T) %s", err, err)
			t.Logf("TABLE: %q", table)
			continue
		}

		{
			const expected int = 1
			actual := count

			if expected != actual {
				t.Errorf("For test #%d, the actual number of tables is not what was expected.", testNumber)
				t.Logf("EXPECTED: %d", expected)
				t.Logf("ACTUAL:   %d", actual)
				t.Logf("TABLE: %q", table)
				continue
			}
		}
	}

	{
		var count int

		err := db.QueryRow(`SELECT COUNT(*) FROM meta`).Scan(&count)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}

		const expected int = 1
		actual := count

		if expected != actual {
			t.Errorf("The actual number of rows in the 'meta' table is not what was expected.")
			t.Logf("EXPECTED: %d", expected)
			t.Logf("ACTUAL:   %d", actual)
		}
	}
}

// TestCreateDatabase_hidesNoDuplicates makes sure the 'hides' table can't hold duplicate rows.
// (This is why its key columns are NOT NULL with '' sentinels, rather than NULL.)
func TestCreateDatabase_hidesNoDuplicates(t *testing.T) {
	var logger log.Logger = log.CreateLogger(io.Discard, "v")

	var filename string = filepath.Join(t.TempDir(), "test.sqlite3")
	var db *sql.DB = openTestDB(t, libdb.DSN(filename))

	{
		err := libdb.CreateDatabase(logger, db)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
	}

	const insert string = `INSERT INTO hides (scope_kind, did, reason, since) VALUES ('account', 'did:example:alice', 'operator', 1)`

	{
		_, err := db.Exec(insert)
		if nil != err {
			t.Fatalf("Did not expect an error on the first insert but actually got one: (%T) %s", err, err)
		}
	}

	{
		_, err := db.Exec(insert)
		if nil == err {
			t.Errorf("Expected an error on the duplicate insert but did not actually get one.")
		}
	}
}

func TestVerifySettings(t *testing.T) {
	var logger log.Logger = log.CreateLogger(io.Discard, "v")

	tests := []struct{
		UseDSN      bool
		ExpectError bool
	}{
		{
			UseDSN:      true,
			ExpectError: false,
		},
		{
			// Without the DSN settings, auto_vacuum (and the rest) are not in effect.
			UseDSN:      false,
			ExpectError: true,
		},
	}

	for testNumber, test := range tests {

		var filename string = filepath.Join(t.TempDir(), "test.sqlite3")

		var dsn string = filename
		if test.UseDSN {
			dsn = libdb.DSN(filename)
		}

		var db *sql.DB = openTestDB(t, dsn)

		{
			err := libdb.CreateDatabase(logger, db)
			if nil != err {
				t.Errorf("For test #%d, did not expect an error from CreateDatabase but actually got one.", testNumber)
				t.Logf("ERROR: (%T) %s", err, err)
				continue
			}
		}

		err := libdb.VerifySettings(logger, db)

		{
			expected := test.ExpectError
			actual   := nil != err

			if expected != actual {
				t.Errorf("For test #%d, whether there was an error is not what was expected.", testNumber)
				t.Logf("EXPECTED: %t", expected)
				t.Logf("ACTUAL:   %t", actual)
				t.Logf("ERROR: %v", err)
				t.Logf("USE-DSN: %t", test.UseDSN)
				continue
			}
		}
	}
}
