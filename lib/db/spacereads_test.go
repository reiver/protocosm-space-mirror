package libdb_test

import (
	"reflect"
	"testing"

	"github.com/reiver/go-opt"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/testfixtures"
)

func TestWritersList(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	forum.Exec(t, `INSERT INTO hides (scope_kind, did, reason, since) VALUES ('account', ?, 'account-inactive:hosting', 1)`, libtestfixtures.Bob)
	forum.Exec(t, `INSERT INTO hides (scope_kind, space_uri, did, reason, since) VALUES ('repo', ?, ?, 'operator', 1)`, libtestfixtures.Space, libtestfixtures.Bob)

	{
		writers, err := libdb.WritersList(forum.Logger, forum.DB, libtestfixtures.Space, "", 100, false)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if 1 != len(writers) || libtestfixtures.Alice != writers[0].DID || !writers[0].Visible {
			t.Errorf("Expected only alice (visible).")
			t.Logf("ACTUAL: %#v", writers)
		}
	}

	{
		writers, err := libdb.WritersList(forum.Logger, forum.DB, libtestfixtures.Space, "", 100, true)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if 2 != len(writers) {
			t.Fatalf("Expected 2 writers but actually got %d: %#v", len(writers), writers)
		}

		var bob libdb.Writer = writers[1]
		if libtestfixtures.Bob != bob.DID || bob.Visible {
			t.Errorf("Expected bob, hidden.")
			t.Logf("ACTUAL: %#v", bob)
		}
		{
			expected := []string{"account-inactive:hosting", "operator"}
			actual := bob.HideReasons
			if !reflect.DeepEqual(expected, actual) && !reflect.DeepEqual([]string{"operator", "account-inactive:hosting"}, actual) {
				t.Errorf("The actual hide reasons are not what was expected.")
				t.Logf("EXPECTED: %v (in any order)", expected)
				t.Logf("ACTUAL:   %v", actual)
			}
		}
	}

	{
		// paging
		writers, err := libdb.WritersList(forum.Logger, forum.DB, libtestfixtures.Space, libtestfixtures.Alice, 100, true)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if 1 != len(writers) || libtestfixtures.Bob != writers[0].DID {
			t.Errorf("Expected only bob after alice.")
			t.Logf("ACTUAL: %#v", writers)
		}
	}
}

func TestSpaceRecordsList(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	forum.Exec(t, `INSERT INTO changelog (at, kind) VALUES (1, 'record'), (2, 'record'), (3, 'record')`)

	paths := func(records []libdb.SpaceRecord) []string {
		var result []string
		for _, record := range records {
			result = append(result, record.DID+" "+record.Collection+"/"+record.RKey)
		}
		return result
	}

	tests := []struct{
		Setup string
		Collection opt.Optional[string]
		AfterDID, AfterCollection, AfterRKey string
		Limit int
		Expected []string
	}{
		{
			Limit: 100,
			Expected: []string{
				libtestfixtures.Alice + " com.atmoboards.reply/3ccc",
				libtestfixtures.Alice + " com.atmoboards.thread/3aaa",
				libtestfixtures.Alice + " com.atmoboards.thread/3bbb",
				libtestfixtures.Bob   + " com.atmoboards.thread/3ddd",
			},
		},
		{
			AfterDID: libtestfixtures.Alice, AfterCollection: "com.atmoboards.thread", AfterRKey: "3aaa",
			Limit: 100,
			Expected: []string{
				libtestfixtures.Alice + " com.atmoboards.thread/3bbb",
				libtestfixtures.Bob   + " com.atmoboards.thread/3ddd",
			},
		},
		{
			Collection: opt.Something("com.atmoboards.thread"),
			Limit: 2,
			Expected: []string{
				libtestfixtures.Alice + " com.atmoboards.thread/3aaa",
				libtestfixtures.Alice + " com.atmoboards.thread/3bbb",
			},
		},
		{
			// A hidden repo's records are not listed.
			Setup: `INSERT INTO hides (scope_kind, did, reason, since) VALUES ('account', '` + libtestfixtures.Alice + `', 'operator', 1)`,
			Limit: 100,
			Expected: []string{
				libtestfixtures.Bob + " com.atmoboards.thread/3ddd",
			},
		},
	}

	for testNumber, test := range tests {

		f := forum
		if "" != test.Setup {
			f = libtestfixtures.NewForum(t)
			f.Exec(t, `INSERT INTO changelog (at, kind) VALUES (1, 'record'), (2, 'record'), (3, 'record')`)
			f.Exec(t, test.Setup)
		}

		records, seq, err := libdb.SpaceRecordsList(f.Logger, f.DB, libtestfixtures.Space, test.Collection, test.AfterDID, test.AfterCollection, test.AfterRKey, test.Limit)
		if nil != err {
			t.Errorf("For test #%d, did not expect an error but actually got one: (%T) %s", testNumber, err, err)
			continue
		}

		if 3 != seq {
			t.Errorf("For test #%d, the actual high-water seq is not what was expected: %d", testNumber, seq)
		}

		if actual := paths(records); !reflect.DeepEqual(test.Expected, actual) {
			t.Errorf("For test #%d, the actual records are not what was expected.", testNumber)
			t.Logf("EXPECTED: %v", test.Expected)
			t.Logf("ACTUAL:   %v", actual)
			continue
		}
	}
}

func TestSpacesStatusList(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	const other string = "at://did:example:other/space/com.atmoboards.forum/main"
	forum.Exec(t, `INSERT INTO spaces (space_uri, authority, type, skey, lifecycle, epoch, notify_token, stale_reasons) VALUES (?, 'did:example:other', 'com.atmoboards.forum', 'main', 'unavailable', 5, 't2', '')`, other)
	const unlisted string = "at://did:example:gone/space/com.atmoboards.forum/old"
	forum.Exec(t, `INSERT INTO spaces (space_uri, authority, type, skey, listed, lifecycle, epoch, notify_token) VALUES (?, 'did:example:gone', 'com.atmoboards.forum', 'old', 0, 'active', 6, 't3')`, unlisted)
	forum.Exec(t, `INSERT INTO hides (scope_kind, space_uri, reason, since) VALUES ('space', ?, 'denied', 1)`, libtestfixtures.Space)

	statuses, seq, err := libdb.SpacesStatusList(forum.Logger, forum.DB, opt.Nothing[string](), "", 100)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if 0 != seq {
		t.Errorf("Expected a high-water seq of 0 (empty change log) but actually got %d.", seq)
	}

	// Only listed spaces, ordered by URI.
	if 2 != len(statuses) {
		t.Fatalf("Expected 2 statuses but actually got %d: %#v", len(statuses), statuses)
	}

	{
		var status libdb.SpaceStatus = statuses[0] // did:example:forum
		if libtestfixtures.Space != status.SpaceURI || "active" != status.Lifecycle || status.Serving {
			t.Errorf("The actual status of the forum space is not what was expected: %#v", status)
		}
		if !reflect.DeepEqual([]string{"denied"}, status.HideReasons) {
			t.Errorf("The actual hide reasons are not what was expected: %v", status.HideReasons)
		}
		if verifiedAt, found := status.Freshness.VerifiedAt.Get(); !found || 1795435200 != verifiedAt {
			t.Errorf("The actual verified-at is not what was expected: %d (found=%t)", verifiedAt, found)
		}
	}
	{
		var status libdb.SpaceStatus = statuses[1] // did:example:other
		if other != status.SpaceURI || "unavailable" != status.Lifecycle || status.Serving {
			t.Errorf("The actual status of the other space is not what was expected: %#v", status)
		}
		if status.Freshness.VerifiedAt.IsSomething() {
			t.Errorf("Did not expect a verified-at for a never-authorized space.")
		}
	}

	{
		only, _, err := libdb.SpacesStatusList(forum.Logger, forum.DB, opt.Something(other), "", 100)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if 1 != len(only) || other != only[0].SpaceURI {
			t.Errorf("Expected only the other space: %#v", only)
		}
	}
}

func TestSpaceFreshness(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	forum.Exec(t, `UPDATE repos SET verified_at = 1795430000, stale_reasons = 'sync-failing' WHERE did = ?`, libtestfixtures.Bob)
	forum.Exec(t, `UPDATE spaces SET stale_reasons = 'registration-lapsed'`)

	freshness, err := libdb.SpaceFreshness(forum.Logger, forum.DB, libtestfixtures.Space)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	if verifiedAt, found := freshness.VerifiedAt.Get(); !found || 1795430000 != verifiedAt {
		t.Errorf("The actual verified-at is not what was expected: %d (found=%t)", verifiedAt, found)
	}
	if expected := []string{"registration-lapsed", "sync-failing"}; !reflect.DeepEqual(expected, freshness.StaleReasons) {
		t.Errorf("The actual stale reasons are not what was expected.")
		t.Logf("EXPECTED: %v", expected)
		t.Logf("ACTUAL:   %v", freshness.StaleReasons)
	}

	// A hidden repo doesn't count.
	forum.Exec(t, `INSERT INTO hides (scope_kind, did, reason, since) VALUES ('account', ?, 'operator', 1)`, libtestfixtures.Bob)

	freshness, err = libdb.SpaceFreshness(forum.Logger, forum.DB, libtestfixtures.Space)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if verifiedAt, found := freshness.VerifiedAt.Get(); !found || 1795435200 != verifiedAt {
		t.Errorf("With bob hidden, the actual verified-at is not what was expected: %d (found=%t)", verifiedAt, found)
	}
	if expected := []string{"registration-lapsed"}; !reflect.DeepEqual(expected, freshness.StaleReasons) {
		t.Errorf("With bob hidden, the actual stale reasons are not what was expected: %v", freshness.StaleReasons)
	}
}
