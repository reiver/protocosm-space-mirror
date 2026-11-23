package libdb_test

import (
	"reflect"
	"testing"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/testfixtures"
)

var (
	scopeSpace   = libdb.HideScope{Kind: libdb.ScopeSpace, SpaceURI: libtestfixtures.Space}
	scopeAccount = libdb.HideScope{Kind: libdb.ScopeAccount, DID: libtestfixtures.Alice}
	scopeRepo    = libdb.HideScope{Kind: libdb.ScopeRepo, SpaceURI: libtestfixtures.Space, DID: libtestfixtures.Alice}
	scopeRecord  = libdb.HideScope{Kind: libdb.ScopeRecord, SpaceURI: libtestfixtures.Space, DID: libtestfixtures.Alice, Collection: "com.atmoboards.thread", RKey: "3aaa"}
)

// TestHides_flipOnly steps through a sequence of hide changes, checking after each
// whether anything changed, and which change-log entry (if any) it appended.
func TestHides_flipOnly(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	type step struct {
		Add             bool
		Scope           libdb.HideScope
		Reason          string
		ExpectedChanged bool
		ExpectedEvent   string // "" for none; otherwise "<kind> <scope>"
	}

	steps := []step{
		{
			// A visible record becomes hidden.
			Add: true, Scope: scopeRecord, Reason: libdb.ReasonOperator,
			ExpectedChanged: true, ExpectedEvent: "hidden record",
		},
		{
			// Again — nothing changes.
			Add: true, Scope: scopeRecord, Reason: libdb.ReasonOperator,
			ExpectedChanged: false, ExpectedEvent: "",
		},
		{
			// A second reason — changed, but it was already hidden, so no event.
			Add: true, Scope: scopeRecord, Reason: libdb.ReasonLabelTakedown,
			ExpectedChanged: true, ExpectedEvent: "",
		},
		{
			// Removing one of two reasons — still hidden, so no event.
			Add: false, Scope: scopeRecord, Reason: libdb.ReasonOperator,
			ExpectedChanged: true, ExpectedEvent: "",
		},
		{
			// Removing the last reason — visible again.
			Add: false, Scope: scopeRecord, Reason: libdb.ReasonLabelTakedown,
			ExpectedChanged: true, ExpectedEvent: "visible record",
		},
		{
			// Removing a reason that isn't there — nothing changes.
			Add: false, Scope: scopeRecord, Reason: libdb.ReasonLabelTakedown,
			ExpectedChanged: false, ExpectedEvent: "",
		},



		{
			// Hide the space.
			Add: true, Scope: scopeSpace, Reason: libdb.ReasonDenied,
			ExpectedChanged: true, ExpectedEvent: "hidden space",
		},
		{
			// A record inside a hidden space — already hidden, so no event.
			Add: true, Scope: scopeRecord, Reason: libdb.ReasonOperator,
			ExpectedChanged: true, ExpectedEvent: "",
		},
		{
			// The repo inside a hidden space — no event.
			Add: true, Scope: scopeRepo, Reason: libdb.ReasonOverQuota,
			ExpectedChanged: true, ExpectedEvent: "",
		},
		{
			// The space becomes visible again (the repo and record inside stay hidden — re-listing the space won't return them).
			Add: false, Scope: scopeSpace, Reason: libdb.ReasonDenied,
			ExpectedChanged: true, ExpectedEvent: "visible space",
		},
		{
			// The repo becomes visible again — but its record is still hidden; the event is for the repo.
			Add: false, Scope: scopeRepo, Reason: libdb.ReasonOverQuota,
			ExpectedChanged: true, ExpectedEvent: "visible repo",
		},
		{
			Add: false, Scope: scopeRecord, Reason: libdb.ReasonOperator,
			ExpectedChanged: true, ExpectedEvent: "visible record",
		},



		{
			// An account hide spans spaces.
			Add: true, Scope: scopeAccount, Reason: libdb.ReasonAccountInactiveHosting,
			ExpectedChanged: true, ExpectedEvent: "hidden account",
		},
		{
			// A repo hide on a hidden account — no event.
			Add: true, Scope: scopeRepo, Reason: libdb.ReasonOperator,
			ExpectedChanged: true, ExpectedEvent: "",
		},
		{
			// A relay takedown is a separate reason on the same account — no event.
			Add: true, Scope: scopeAccount, Reason: libdb.ReasonAccountInactiveRelay,
			ExpectedChanged: true, ExpectedEvent: "",
		},
		{
			// The hosting reason clears — the relay reason still hides the account.
			Add: false, Scope: scopeAccount, Reason: libdb.ReasonAccountInactiveHosting,
			ExpectedChanged: true, ExpectedEvent: "",
		},
		{
			// The relay reason clears — the account is visible (its repo in this space is still hidden by the operator).
			Add: false, Scope: scopeAccount, Reason: libdb.ReasonAccountInactiveRelay,
			ExpectedChanged: true, ExpectedEvent: "visible account",
		},
		{
			Add: false, Scope: scopeRepo, Reason: libdb.ReasonOperator,
			ExpectedChanged: true, ExpectedEvent: "visible repo",
		},
	}

	var lastSeq int64
	for stepNumber, s := range steps {

		var changed bool
		var err error
		if s.Add {
			changed, err = libdb.AddHide(forum.Logger, forum.DB, s.Scope, s.Reason, 100+int64(stepNumber))
		} else {
			changed, err = libdb.RemoveHide(forum.Logger, forum.DB, s.Scope, s.Reason, 100+int64(stepNumber))
		}
		if nil != err {
			t.Fatalf("For step #%d, did not expect an error but actually got one: (%T) %s", stepNumber, err, err)
		}

		if s.ExpectedChanged != changed {
			t.Errorf("For step #%d, the actual changed is not what was expected.", stepNumber)
			t.Logf("EXPECTED: %t", s.ExpectedChanged)
			t.Logf("ACTUAL:   %t", changed)
		}

		changes, err := libdb.ChangesList(forum.Logger, forum.DB, lastSeq, 100)
		if nil != err {
			t.Fatalf("For step #%d, did not expect an error but actually got one: (%T) %s", stepNumber, err, err)
		}

		var actual []string
		for _, change := range changes {
			actual = append(actual, change.Kind + " " + change.Scope)
			lastSeq = change.Seq
		}

		var expected []string
		if "" != s.ExpectedEvent {
			expected = []string{s.ExpectedEvent}
		}

		if !reflect.DeepEqual(expected, actual) {
			t.Errorf("For step #%d, the actual change-log entries are not what was expected.", stepNumber)
			t.Logf("EXPECTED: %v", expected)
			t.Logf("ACTUAL:   %v", actual)
		}
	}
}

func TestHides_versionSpecific(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	var other = scopeRecord
	other.CID = "bafyreiOTHERVERSION"

	// Hiding some other version of the record does not hide the current one.
	if _, err := libdb.AddHide(forum.Logger, forum.DB, other, libdb.ReasonLabelTakedown, 1); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	_, found, err := libdb.RecordSelect(forum.Logger, forum.DB, libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa")
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if !found {
		t.Errorf("Expected the current version to stay visible.")
	}
}

func TestHides_badScope(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	tests := []libdb.HideScope{
		{},
		{Kind: libdb.ScopeSpace},
		{Kind: libdb.ScopeSpace, SpaceURI: libtestfixtures.Space, DID: libtestfixtures.Alice},
		{Kind: libdb.ScopeAccount, SpaceURI: libtestfixtures.Space, DID: libtestfixtures.Alice},
		{Kind: libdb.ScopeRepo, DID: libtestfixtures.Alice},
		{Kind: libdb.ScopeRecord, SpaceURI: libtestfixtures.Space, DID: libtestfixtures.Alice, Collection: "com.atmoboards.thread"},
		{Kind: "bogus", SpaceURI: libtestfixtures.Space},
	}

	for testNumber, test := range tests {
		_, err := libdb.AddHide(forum.Logger, forum.DB, test, libdb.ReasonOperator, 1)
		if nil == err {
			t.Errorf("For test #%d, expected an error but did not actually get one.", testNumber)
			t.Logf("SCOPE: %#v", test)
		}
	}
}

func TestChangelogBounds(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	floor, max, err := libdb.ChangelogBounds(forum.Logger, forum.DB)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if 0 != floor || 0 != max {
		t.Errorf("Expected (0, 0) for an empty change log, but actually got (%d, %d).", floor, max)
	}

	if _, err := libdb.AddHide(forum.Logger, forum.DB, scopeSpace, libdb.ReasonOperator, 1); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	forum.Exec(t, `UPDATE meta SET changelog_floor_seq = 1`)

	floor, max, err = libdb.ChangelogBounds(forum.Logger, forum.DB)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if 1 != floor || 1 != max {
		t.Errorf("Expected (1, 1), but actually got (%d, %d).", floor, max)
	}
}
