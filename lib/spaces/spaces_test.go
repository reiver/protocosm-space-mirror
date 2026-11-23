package libspaces_test

import (
	"reflect"
	"testing"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/spaces"
	"protocosm-space-mirror/lib/testfixtures"
)

const (
	spaceA = "at://did:example:forum/space/com.atmoboards.forum/general"
	spaceB = "at://did:example:forum/space/com.atmoboards.forum/off-topic"
	spaceC = "at://did:example:other/space/com.atmoboards.forum/main"
)

func TestParse(t *testing.T) {
	text := "# forums\n" +
		"\n" +
		"  " + spaceA + "  \n" +
		spaceB + "\n" +
		spaceA + "\n" + // duplicate
		"# the end\n"

	actual, err := libspaces.Parse(text)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	if expected := []string{spaceA, spaceB}; !reflect.DeepEqual(expected, actual) {
		t.Errorf("The actual spaces are not what was expected.")
		t.Logf("EXPECTED: %v", expected)
		t.Logf("ACTUAL:   %v", actual)
	}
}

func TestParse_errors(t *testing.T) {

	tests := []string{
		"not a space",
		spaceA + "\n" + "at://did:example:forum/com.atmoboards.thread/3abc",
		spaceA + "/did:example:alice/com.atmoboards.thread/3abc",
		"at://did:example:forum/space/com.atmoboards.forum/general extra",
	}

	for testNumber, test := range tests {
		_, err := libspaces.Parse(test)
		if nil == err {
			t.Errorf("For test #%d, expected an error but did not actually get one.", testNumber)
			t.Logf("TEXT: %q", test)
		}
	}
}

func TestParse_empty(t *testing.T) {
	actual, err := libspaces.Parse("# nothing here\n\n")
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if 0 != len(actual) {
		t.Errorf("Expected no spaces: %v", actual)
	}
}

func TestCompare(t *testing.T) {
	known := map[string]bool{
		spaceA: true,  // listed, still in file
		spaceB: true,  // listed, not in file → removed
		spaceC: false, // unlisted, back in file → relisted
	}
	const spaceD string = "at://did:example:new/space/com.atmoboards.forum/fresh"

	actual := libspaces.Compare([]string{spaceA, spaceC, spaceD}, known)

	expected := libspaces.Diff{
		Added:    []string{spaceD},
		Removed:  []string{spaceB},
		Relisted: []string{spaceC},
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Errorf("The actual diff is not what was expected.")
		t.Logf("EXPECTED: %#v", expected)
		t.Logf("ACTUAL:   %#v", actual)
	}
}

func TestCheckUnlist(t *testing.T) {

	removing := func(n int) libspaces.Diff {
		var diff libspaces.Diff
		for i:=0; i<n; i++ {
			diff.Removed = append(diff.Removed, "x")
		}
		return diff
	}

	tests := []struct{
		Removing    int
		Listed      int
		UnlistMax   int
		Force       bool
		ExpectError bool
	}{
		{
			// nothing to remove
			Removing: 0, Listed: 0, UnlistMax: 10,
		},
		{
			Removing: 1, Listed: 100, UnlistMax: 10,
		},
		{
			Removing: 10, Listed: 100, UnlistMax: 10,
		},
		{
			// more than UNLIST_MAX
			Removing: 11, Listed: 100, UnlistMax: 10,
			ExpectError: true,
		},
		{
			// exactly half is allowed
			Removing: 2, Listed: 4, UnlistMax: 10,
		},
		{
			// more than half
			Removing: 3, Listed: 4, UnlistMax: 10,
			ExpectError: true,
		},
		{
			// an empty file, on a small mirror
			Removing: 1, Listed: 1, UnlistMax: 10,
			ExpectError: true,
		},
		{
			// forced
			Removing: 1, Listed: 1, UnlistMax: 10, Force: true,
		},
		{
			Removing: 50, Listed: 100, UnlistMax: 10, Force: true,
		},
	}

	for testNumber, test := range tests {
		err := libspaces.CheckUnlist(removing(test.Removing), test.Listed, test.UnlistMax, test.Force)

		if test.ExpectError != (nil != err) {
			t.Errorf("For test #%d, whether there was an error is not what was expected.", testNumber)
			t.Logf("EXPECTED: %t", test.ExpectError)
			t.Logf("ACTUAL:   %v", err)
			t.Logf("TEST: %#v", test)
		}
	}
}

func TestLoad(t *testing.T) {
	forum := libtestfixtures.NewForum(t) // has one listed space: spaceA (= libtestfixtures.Space)

	const now int64 = 1795435200
	const grace int64 = 72 * 60 * 60

	changes := func() []string {
		t.Helper()
		list, err := libdb.ChangesList(forum.Logger, forum.DB, 0, 100)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		var result []string
		for _, change := range list {
			result = append(result, change.Kind+" "+change.SpaceURI+" "+change.Reason)
		}
		return result
	}

	// 1. Add a space; keep the existing one.
	{
		diff, changelogged, err := libspaces.Load(forum.Logger, forum.DB, spaceA+"\n"+spaceB+"\n", 10, false, now, grace)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if !reflect.DeepEqual([]string{spaceB}, diff.Added) || 0 != len(diff.Removed) || changelogged {
			t.Errorf("Expected only spaceB added, and nothing in the change log: %#v %t", diff, changelogged)
		}

		var lifecycle string
		var epoch int64
		var token string
		if err := forum.DB.QueryRow(`SELECT lifecycle, epoch, notify_token FROM spaces WHERE space_uri = ?`, spaceB).Scan(&lifecycle, &epoch, &token); nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if "unavailable" != lifecycle || epoch <= 0 || len(token) < 40 {
			t.Errorf("The new space's row is not what was expected: %q %d %q", lifecycle, epoch, token)
		}
	}

	// 2. An invalid file changes nothing.
	{
		_, _, err := libspaces.Load(forum.Logger, forum.DB, spaceA+"\nnonsense\n", 10, false, now, grace)
		if nil == err {
			t.Errorf("Expected an error for an invalid file.")
		}
		known, _ := libdb.KnownSpaces(forum.Logger, forum.DB)
		if !known[spaceA] || !known[spaceB] {
			t.Errorf("Expected nothing to change after an invalid file: %v", known)
		}
	}

	// 3. An empty file would unlist both — more than half — refused.
	{
		_, _, err := libspaces.Load(forum.Logger, forum.DB, "# empty\n", 10, false, now, grace)
		if nil == err {
			t.Errorf("Expected an empty file to be refused.")
		}
		known, _ := libdb.KnownSpaces(forum.Logger, forum.DB)
		if !known[spaceA] || !known[spaceB] {
			t.Errorf("Expected nothing to change after a refused file: %v", known)
		}
		if 0 != len(changes()) {
			t.Errorf("Expected nothing in the change log: %v", changes())
		}
	}

	// 4. Unlisting one of two (exactly half) is allowed: spaceA is hidden, with a purge time.
	{
		diff, changelogged, err := libspaces.Load(forum.Logger, forum.DB, spaceB+"\n", 10, false, now, grace)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if !reflect.DeepEqual([]string{spaceA}, diff.Removed) || !changelogged {
			t.Errorf("Expected spaceA unlisted, with a change-log entry: %#v %t", diff, changelogged)
		}

		var purgeAfter int64
		if err := forum.DB.QueryRow(`SELECT purge_after FROM hides WHERE scope_kind = 'space' AND space_uri = ? AND reason = 'unlisted'`, spaceA).Scan(&purgeAfter); nil != err {
			t.Fatalf("Expected an 'unlisted' hide: (%T) %s", err, err)
		}
		if now+grace != purgeAfter {
			t.Errorf("The actual purge-after is not what was expected: %d", purgeAfter)
		}

		if visible, _ := libdb.SpaceVisible(forum.Logger, forum.DB, spaceA); visible {
			t.Errorf("Expected the unlisted space to be hidden.")
		}

		if expected := []string{"hidden " + spaceA + " unlisted"}; !reflect.DeepEqual(expected, changes()) {
			t.Errorf("The actual change log is not what was expected.")
			t.Logf("EXPECTED: %v", expected)
			t.Logf("ACTUAL:   %v", changes())
		}
	}

	// 5. Re-listing it removes the hide, and announces it.
	{
		diff, changelogged, err := libspaces.Load(forum.Logger, forum.DB, spaceA+"\n"+spaceB+"\n", 10, false, now+10, grace)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if !reflect.DeepEqual([]string{spaceA}, diff.Relisted) || !changelogged {
			t.Errorf("Expected spaceA relisted, with a change-log entry: %#v %t", diff, changelogged)
		}
		if visible, _ := libdb.SpaceVisible(forum.Logger, forum.DB, spaceA); !visible {
			t.Errorf("Expected the relisted space to be visible again.")
		}
		if expected := []string{"hidden " + spaceA + " unlisted", "visible " + spaceA + " unlisted"}; !reflect.DeepEqual(expected, changes()) {
			t.Errorf("The actual change log is not what was expected.")
			t.Logf("EXPECTED: %v", expected)
			t.Logf("ACTUAL:   %v", changes())
		}
	}

	// 6. Forced: an empty file unlists everything.
	{
		diff, _, err := libspaces.Load(forum.Logger, forum.DB, "", 10, true, now+20, grace)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if 2 != len(diff.Removed) {
			t.Errorf("Expected both spaces unlisted when forced: %#v", diff)
		}
	}
}
