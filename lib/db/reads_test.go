package libdb_test

import (
	"database/sql"
	"io"
	"path/filepath"
	"reflect"
	"testing"

	"codeberg.org/reiver/go-log"
	"github.com/reiver/go-opt"

	"protocosm-space-mirror/lib/db"
)

const (
	testSpace = "at://did:example:forum/space/com.atmoboards.forum/general"
	testAlice = "did:example:alice"
	testBob   = "did:example:bob"
)

// newReadsTestDB creates a database with:
//
//	• an active space, with repos for alice (generation 1) and bob (generation 0),
//	• alice: 2 threads + 1 reply (generation 1), and 1 stale thread in generation 0 (must not be visible),
//	• bob: 1 thread,
//	• a blob referenced by alice's first thread.
func newReadsTestDB(t *testing.T) (log.Logger, *sql.DB) {
	t.Helper()

	var logger log.Logger = log.CreateLogger(io.Discard, "v")

	var filename string = filepath.Join(t.TempDir(), "test.sqlite3")
	db := openTestDB(t, libdb.DSN(filename))

	if err := libdb.CreateDatabase(logger, db); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	statements := []string{
		`INSERT INTO spaces (space_uri, authority, type, skey, lifecycle, epoch, notify_token, verified_at, stale_reasons) VALUES ('` + testSpace + `', 'did:example:forum', 'com.atmoboards.forum', 'general', 'active', 1, 'token', 1000, '')`,
		`INSERT INTO repos (space_uri, did, epoch, generation, verified_at, stale_reasons) VALUES ('` + testSpace + `', '` + testAlice + `', 2, 1, 900, 'repo-host-unreachable')`,
		`INSERT INTO repos (space_uri, did, epoch, generation, verified_at) VALUES ('` + testSpace + `', '` + testBob + `', 3, 0, 1100)`,

		`INSERT INTO records (space_uri, did, generation, collection, rkey, cid, drisl) VALUES ('` + testSpace + `', '` + testAlice + `', 1, 'com.atmoboards.thread', '3aaa', 'cid-a1', x'a0')`,
		`INSERT INTO records (space_uri, did, generation, collection, rkey, cid, drisl) VALUES ('` + testSpace + `', '` + testAlice + `', 1, 'com.atmoboards.thread', '3bbb', 'cid-a2', x'a0')`,
		`INSERT INTO records (space_uri, did, generation, collection, rkey, cid, drisl) VALUES ('` + testSpace + `', '` + testAlice + `', 1, 'com.atmoboards.reply',  '3ccc', 'cid-a3', x'a0')`,
		`INSERT INTO records (space_uri, did, generation, collection, rkey, cid, drisl) VALUES ('` + testSpace + `', '` + testAlice + `', 0, 'com.atmoboards.thread', '3zzz', 'cid-old', x'a0')`,
		`INSERT INTO records (space_uri, did, generation, collection, rkey, cid, drisl) VALUES ('` + testSpace + `', '` + testBob + `', 0, 'com.atmoboards.thread', '3ddd', 'cid-b1', x'a0')`,

		`INSERT INTO blobs (cid, size, mime, state, path) VALUES ('blob-1', 3, 'image/png', 'fetched', 'blob-1')`,
		`INSERT INTO blob_refs (space_uri, did, generation, collection, rkey, cid) VALUES ('` + testSpace + `', '` + testAlice + `', 1, 'com.atmoboards.thread', '3aaa', 'blob-1')`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s\nSTATEMENT: %s", err, err, statement)
		}
	}

	return logger, db
}

func mustExec(t *testing.T, db *sql.DB, statement string, args ...any) {
	t.Helper()

	if _, err := db.Exec(statement, args...); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s\nSTATEMENT: %s", err, err, statement)
	}
}

func TestRecordSelect_visibility(t *testing.T) {

	tests := []struct{
		Setup []string
		DID   string
		Collection string
		RKey  string
		ExpectedFound bool
	}{
		{
			// No hides.
			DID: testAlice, Collection: "com.atmoboards.thread", RKey: "3aaa",
			ExpectedFound: true,
		},
		{
			// Not in the repo's current generation.
			DID: testAlice, Collection: "com.atmoboards.thread", RKey: "3zzz",
			ExpectedFound: false,
		},
		{
			// Does not exist.
			DID: testAlice, Collection: "com.atmoboards.thread", RKey: "nope",
			ExpectedFound: false,
		},
		{
			// A record hide.
			Setup: []string{`INSERT INTO hides (scope_kind, space_uri, did, collection, rkey, reason, since) VALUES ('record', '` + testSpace + `', '` + testAlice + `', 'com.atmoboards.thread', '3aaa', 'label-takedown', 1)`},
			DID: testAlice, Collection: "com.atmoboards.thread", RKey: "3aaa",
			ExpectedFound: false,
		},
		{
			// A record hide for the right version.
			Setup: []string{`INSERT INTO hides (scope_kind, space_uri, did, collection, rkey, cid, reason, since) VALUES ('record', '` + testSpace + `', '` + testAlice + `', 'com.atmoboards.thread', '3aaa', 'cid-a1', 'label-takedown', 1)`},
			DID: testAlice, Collection: "com.atmoboards.thread", RKey: "3aaa",
			ExpectedFound: false,
		},
		{
			// A record hide for some other version does not hide this one.
			Setup: []string{`INSERT INTO hides (scope_kind, space_uri, did, collection, rkey, cid, reason, since) VALUES ('record', '` + testSpace + `', '` + testAlice + `', 'com.atmoboards.thread', '3aaa', 'cid-other', 'label-takedown', 1)`},
			DID: testAlice, Collection: "com.atmoboards.thread", RKey: "3aaa",
			ExpectedFound: true,
		},
		{
			// A record hide on a different record.
			Setup: []string{`INSERT INTO hides (scope_kind, space_uri, did, collection, rkey, reason, since) VALUES ('record', '` + testSpace + `', '` + testAlice + `', 'com.atmoboards.thread', '3bbb', 'operator', 1)`},
			DID: testAlice, Collection: "com.atmoboards.thread", RKey: "3aaa",
			ExpectedFound: true,
		},
	}

	for testNumber, test := range tests {

		logger, db := newReadsTestDB(t)
		for _, statement := range test.Setup {
			mustExec(t, db, statement)
		}

		record, found, err := libdb.RecordSelect(logger, db, testSpace, test.DID, test.Collection, test.RKey)
		if nil != err {
			t.Errorf("For test #%d, did not expect an error but actually got one: (%T) %s", testNumber, err, err)
			continue
		}

		if test.ExpectedFound != found {
			t.Errorf("For test #%d, the actual found is not what was expected.", testNumber)
			t.Logf("EXPECTED: %t", test.ExpectedFound)
			t.Logf("ACTUAL:   %t", found)
			t.Logf("RECORD: %#v", record)
			continue
		}
	}
}

func TestRepoVisible(t *testing.T) {

	tests := []struct{
		Setup []string
		DID   string
		Expected bool
	}{
		{
			DID:      testAlice,
			Expected: true,
		},
		{
			DID:      "did:example:nobody",
			Expected: false,
		},
		{
			Setup:    []string{`UPDATE spaces SET lifecycle = 'unavailable'`},
			DID:      testAlice,
			Expected: false,
		},
		{
			Setup:    []string{`INSERT INTO hides (scope_kind, space_uri, reason, since) VALUES ('space', '` + testSpace + `', 'denied', 1)`},
			DID:      testAlice,
			Expected: false,
		},
		{
			// An account hide applies in every space.
			Setup:    []string{`INSERT INTO hides (scope_kind, did, reason, since) VALUES ('account', '` + testAlice + `', 'account-inactive:hosting', 1)`},
			DID:      testAlice,
			Expected: false,
		},
		{
			Setup:    []string{`INSERT INTO hides (scope_kind, space_uri, did, reason, since) VALUES ('repo', '` + testSpace + `', '` + testAlice + `', 'over-quota', 1)`},
			DID:      testAlice,
			Expected: false,
		},
		{
			// A hide on bob doesn't hide alice.
			Setup:    []string{`INSERT INTO hides (scope_kind, did, reason, since) VALUES ('account', '` + testBob + `', 'operator', 1)`},
			DID:      testAlice,
			Expected: true,
		},
	}

	for testNumber, test := range tests {

		logger, db := newReadsTestDB(t)
		for _, statement := range test.Setup {
			mustExec(t, db, statement)
		}

		actual, err := libdb.RepoVisible(logger, db, testSpace, test.DID)
		if nil != err {
			t.Errorf("For test #%d, did not expect an error but actually got one: (%T) %s", testNumber, err, err)
			continue
		}

		if test.Expected != actual {
			t.Errorf("For test #%d, the actual visibility is not what was expected.", testNumber)
			t.Logf("EXPECTED: %t", test.Expected)
			t.Logf("ACTUAL:   %t", actual)
			continue
		}
	}
}

func TestRecordsList(t *testing.T) {
	logger, db := newReadsTestDB(t)

	paths := func(records []libdb.Record) []string {
		var result []string
		for _, record := range records {
			result = append(result, record.Collection+"/"+record.RKey)
		}
		return result
	}

	tests := []struct{
		Collection opt.Optional[string]
		AfterCollection string
		AfterRKey string
		Limit int
		Expected []string
	}{
		{
			Limit:    100,
			Expected: []string{"com.atmoboards.reply/3ccc", "com.atmoboards.thread/3aaa", "com.atmoboards.thread/3bbb"},
		},
		{
			Limit:    2,
			Expected: []string{"com.atmoboards.reply/3ccc", "com.atmoboards.thread/3aaa"},
		},
		{
			AfterCollection: "com.atmoboards.thread", AfterRKey: "3aaa",
			Limit:    100,
			Expected: []string{"com.atmoboards.thread/3bbb"},
		},
		{
			Collection: opt.Something("com.atmoboards.thread"),
			Limit:    100,
			Expected: []string{"com.atmoboards.thread/3aaa", "com.atmoboards.thread/3bbb"},
		},
	}

	for testNumber, test := range tests {

		records, err := libdb.RecordsList(logger, db, testSpace, testAlice, test.Collection, test.AfterCollection, test.AfterRKey, test.Limit)
		if nil != err {
			t.Errorf("For test #%d, did not expect an error but actually got one: (%T) %s", testNumber, err, err)
			continue
		}

		actual := paths(records)

		if !reflect.DeepEqual(test.Expected, actual) {
			t.Errorf("For test #%d, the actual records are not what was expected.", testNumber)
			t.Logf("EXPECTED: %v", test.Expected)
			t.Logf("ACTUAL:   %v", actual)
			continue
		}
	}
}

func TestBlobs(t *testing.T) {
	logger, db := newReadsTestDB(t)

	{
		blob, found, err := libdb.BlobSelectForRepo(logger, db, testSpace, testAlice, "blob-1")
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if !found || "image/png" != blob.Mime || 3 != blob.Size {
			t.Errorf("Expected alice's blob to be found.")
			t.Logf("FOUND: %t", found)
			t.Logf("BLOB: %#v", blob)
		}
	}

	{
		// Bob's records don't refer to it.
		_, found, err := libdb.BlobSelectForRepo(logger, db, testSpace, testBob, "blob-1")
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if found {
			t.Errorf("Did not expect the blob to be found for bob.")
		}
	}

	{
		cids, err := libdb.BlobsList(logger, db, testSpace, testAlice, "", 100)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if !reflect.DeepEqual([]string{"blob-1"}, cids) {
			t.Errorf("The actual blob list is not what was expected.")
			t.Logf("ACTUAL: %v", cids)
		}
	}

	// Hiding the only record that refers to the blob hides the blob.
	mustExec(t, db, `INSERT INTO hides (scope_kind, space_uri, did, collection, rkey, reason, since) VALUES ('record', ?, ?, 'com.atmoboards.thread', '3aaa', 'operator', 1)`, testSpace, testAlice)

	{
		_, found, err := libdb.BlobSelectForRepo(logger, db, testSpace, testAlice, "blob-1")
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if found {
			t.Errorf("Did not expect the blob of a hidden record to be found.")
		}
	}

	{
		cids, err := libdb.BlobsList(logger, db, testSpace, testAlice, "", 100)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if 0 != len(cids) {
			t.Errorf("Did not expect any blobs to be listed.")
			t.Logf("ACTUAL: %v", cids)
		}
	}
}

func TestRepoFreshness(t *testing.T) {
	logger, db := newReadsTestDB(t)

	mustExec(t, db, `UPDATE spaces SET stale_reasons = 'registration-lapsed repo-host-unreachable'`)

	freshness, err := libdb.RepoFreshness(logger, db, testSpace, testAlice)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	{
		// The older of the space's (1000) and the repo's (900).
		verifiedAt, found := freshness.VerifiedAt.Get()
		if !found || 900 != verifiedAt {
			t.Errorf("The actual verified-at is not what was expected.")
			t.Logf("EXPECTED: 900")
			t.Logf("ACTUAL:   %d (found=%t)", verifiedAt, found)
		}
	}

	{
		expected := []string{"registration-lapsed", "repo-host-unreachable"}
		if !reflect.DeepEqual(expected, freshness.StaleReasons) {
			t.Errorf("The actual stale reasons are not what was expected.")
			t.Logf("EXPECTED: %v", expected)
			t.Logf("ACTUAL:   %v", freshness.StaleReasons)
		}
	}
}
