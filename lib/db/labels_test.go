package libdb_test

import (
	"database/sql"
	"reflect"
	"testing"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/testfixtures"
)

const labelNow int64 = 1795500000 // 2026-11-24T06:00:00Z

const (
	subject3bbb = "at://" + libtestfixtures.Alice + "/com.atmoboards.thread/3bbb"
	subject3zzz = "at://" + libtestfixtures.Alice + "/com.atmoboards.thread/3zzz"
)

// newLabelForum is the forum fixture, plus a repo for the space's authority (where its labels live).
func newLabelForum(t *testing.T) libtestfixtures.Forum {
	t.Helper()

	forum := libtestfixtures.NewForum(t)
	forum.Exec(t, `INSERT INTO repos (space_uri, did, epoch, generation, verified_at) VALUES (?, ?, 4, 0, 1795435200)`, libtestfixtures.Space, libtestfixtures.Authority)
	return forum
}

func labelKey(src string, rkey string) libdb.LabelKey {
	return libdb.LabelKey{SpaceURI: libtestfixtures.Space, SrcDID: src, Generation: 0, Collection: "com.atproto.label.record", RKey: rkey}
}

// inTx runs 'fn' in a write transaction, and returns whether it said a hide changed.
func inTx(t *testing.T, forum libtestfixtures.Forum, fn func(*sql.Tx) (bool, error)) bool {
	t.Helper()

	tx, err := forum.DB.Begin()
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	defer tx.Rollback()

	changed, err := fn(tx)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if err := tx.Commit(); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	return changed
}

func putLabel(t *testing.T, forum libtestfixtures.Forum, key libdb.LabelKey, label libdb.Label) bool {
	t.Helper()

	return inTx(t, forum, func(tx *sql.Tx) (bool, error) {
		return libdb.PutLabelTx(forum.Logger, tx, key, label, labelNow)
	})
}

func recordServed(t *testing.T, forum libtestfixtures.Forum, rkey string) bool {
	t.Helper()

	_, found, err := libdb.RecordSelect(forum.Logger, forum.DB, libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", rkey)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	return found
}

func currentCID(t *testing.T, forum libtestfixtures.Forum, rkey string) string {
	t.Helper()

	record, found, err := libdb.RecordSelect(forum.Logger, forum.DB, libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", rkey)
	if nil != err || !found {
		t.Fatalf("Expected record %q to be found (err: %v).", rkey, err)
	}
	return record.CID
}

func TestLabels_takedown(t *testing.T) {
	tests := []struct {
		Name            string
		Src             string
		Label           func(forum libtestfixtures.Forum) libdb.Label
		ExpectedChanged bool
		ExpectedServed  bool
		ExpectedEvents  []string
	}{
		{
			Name: "the authority's takedown hides the record",
			Src:  libtestfixtures.Authority,
			Label: func(libtestfixtures.Forum) libdb.Label {
				return libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-24T00:00:00Z"}
			},
			ExpectedChanged: true, ExpectedServed: false, ExpectedEvents: []string{"hidden record"},
		},
		{
			Name: "the authority's takedown of the current version hides the record",
			Src:  libtestfixtures.Authority,
			Label: func(forum libtestfixtures.Forum) libdb.Label {
				return libdb.Label{URI: subject3bbb, CID: currentCID(t, forum, "3bbb"), Val: "!takedown", CTS: "2026-11-24T00:00:00Z"}
			},
			ExpectedChanged: true, ExpectedServed: false, ExpectedEvents: []string{"hidden record"},
		},
		{
			Name: "the authority's takedown of another version hides only that version — nothing visible flips",
			Src:  libtestfixtures.Authority,
			Label: func(libtestfixtures.Forum) libdb.Label {
				return libdb.Label{URI: subject3bbb, CID: "bafyreiolderversion", Val: "!takedown", CTS: "2026-11-24T00:00:00Z"}
			},
			ExpectedChanged: true, ExpectedServed: true, ExpectedEvents: nil,
		},
		{
			Name: "someone else's takedown is passed through",
			Src:  libtestfixtures.Bob,
			Label: func(libtestfixtures.Forum) libdb.Label {
				return libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-24T00:00:00Z"}
			},
			ExpectedChanged: false, ExpectedServed: true, ExpectedEvents: nil,
		},
		{
			Name: "another label value is passed through",
			Src:  libtestfixtures.Authority,
			Label: func(libtestfixtures.Forum) libdb.Label {
				return libdb.Label{URI: subject3bbb, Val: "spoiler", CTS: "2026-11-24T00:00:00Z"}
			},
			ExpectedChanged: false, ExpectedServed: true, ExpectedEvents: nil,
		},
		{
			Name: "an account-level subject is passed through",
			Src:  libtestfixtures.Authority,
			Label: func(libtestfixtures.Forum) libdb.Label {
				return libdb.Label{URI: "at://" + libtestfixtures.Alice, Val: "!takedown", CTS: "2026-11-24T00:00:00Z"}
			},
			ExpectedChanged: false, ExpectedServed: true, ExpectedEvents: nil,
		},
		{
			Name: "an expired takedown hides nothing",
			Src:  libtestfixtures.Authority,
			Label: func(libtestfixtures.Forum) libdb.Label {
				return libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-24T00:00:00Z", Exp: "2026-11-24T01:00:00Z"}
			},
			ExpectedChanged: false, ExpectedServed: true, ExpectedEvents: nil,
		},
		{
			Name: "a takedown that has not expired yet hides the record",
			Src:  libtestfixtures.Authority,
			Label: func(libtestfixtures.Forum) libdb.Label {
				return libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-24T00:00:00Z", Exp: "2026-11-25T00:00:00Z"}
			},
			ExpectedChanged: true, ExpectedServed: false, ExpectedEvents: []string{"hidden record"},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			forum := newLabelForum(t)

			if actual := putLabel(t, forum, labelKey(test.Src, "3lab"), test.Label(forum)); test.ExpectedChanged != actual {
				t.Errorf("Expected changed = %t but actually got %t.", test.ExpectedChanged, actual)
			}
			if actual := recordServed(t, forum, "3bbb"); test.ExpectedServed != actual {
				t.Errorf("Expected served = %t but actually got %t.", test.ExpectedServed, actual)
			}
			if actual := changeKinds(t, forum); !reflect.DeepEqual(test.ExpectedEvents, actual) {
				t.Errorf("The actual change log is not what was expected.")
				t.Logf("EXPECTED: %#v", test.ExpectedEvents)
				t.Logf("ACTUAL:   %#v", actual)
			}
		})
	}
}

func TestLabels_deleteUnhides(t *testing.T) {
	forum := newLabelForum(t)

	putLabel(t, forum, labelKey(libtestfixtures.Authority, "3lab"), libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-24T00:00:00Z"})

	changed := inTx(t, forum, func(tx *sql.Tx) (bool, error) {
		return libdb.DeleteLabelTx(forum.Logger, tx, labelKey(libtestfixtures.Authority, "3lab"), labelNow)
	})
	if !changed {
		t.Errorf("Expected deleting the label to change a hide.")
	}
	if !recordServed(t, forum, "3bbb") {
		t.Errorf("Expected the record to be served again.")
	}
	if expected, actual := []string{"hidden record", "visible record"}, changeKinds(t, forum); !reflect.DeepEqual(expected, actual) {
		t.Errorf("The actual change log is not what was expected.")
		t.Logf("EXPECTED: %#v", expected)
		t.Logf("ACTUAL:   %#v", actual)
	}

	// Deleting a label that is not there is fine.
	if inTx(t, forum, func(tx *sql.Tx) (bool, error) {
		return libdb.DeleteLabelTx(forum.Logger, tx, labelKey(libtestfixtures.Authority, "3nope"), labelNow)
	}) {
		t.Errorf("Did not expect deleting a missing label to change anything.")
	}
}

// A label record re-pointed at another subject un-hides the old one and hides the new one.
func TestLabels_replacedWithAnotherSubject(t *testing.T) {
	forum := newLabelForum(t)

	putLabel(t, forum, labelKey(libtestfixtures.Authority, "3lab"), libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-24T00:00:00Z"})
	putLabel(t, forum, labelKey(libtestfixtures.Authority, "3lab"), libdb.Label{URI: "at://" + libtestfixtures.Alice + "/com.atmoboards.thread/3aaa", Val: "!takedown", CTS: "2026-11-24T00:01:00Z"})

	if !recordServed(t, forum, "3bbb") {
		t.Errorf("Expected the old subject to be served again.")
	}
	if recordServed(t, forum, "3aaa") {
		t.Errorf("Expected the new subject to be hidden.")
	}
}

// For one version, the newest label (by "cts") decides.
func TestLabels_newestWins(t *testing.T) {
	forum := newLabelForum(t)

	putLabel(t, forum, labelKey(libtestfixtures.Authority, "3old"), libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-01T00:00:00Z"})
	if recordServed(t, forum, "3bbb") {
		t.Fatalf("Expected the record to be hidden by the older, non-expiring label.")
	}

	putLabel(t, forum, labelKey(libtestfixtures.Authority, "3new"), libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-20T00:00:00.000Z", Exp: "2026-11-21T00:00:00Z"})
	if !recordServed(t, forum, "3bbb") {
		t.Errorf("Expected the newer (expired) label to win, so the record is served.")
	}
}

// A label can arrive before its subject; and a purge of the subject's repo deletes its hides, but not the label.
func TestLabels_reevaluateOnRecordUpsert(t *testing.T) {
	forum := newLabelForum(t)

	// Before the subject: the hide is recorded, but nothing visible flips.
	putLabel(t, forum, labelKey(libtestfixtures.Authority, "3lab"), libdb.Label{URI: subject3zzz, Val: "!takedown", CTS: "2026-11-24T00:00:00Z"})
	if actual := changeKinds(t, forum); 0 != len(actual) {
		t.Errorf("Did not expect any change-log entry for a label of a record not here, but got: %#v", actual)
	}

	forum.Exec(t, `INSERT INTO records (space_uri, did, generation, collection, rkey, cid, drisl) VALUES (?, ?, 1, 'com.atmoboards.thread', '3zzz', 'bafyreizzz', x'a0')`,
		libtestfixtures.Space, libtestfixtures.Alice)
	if recordServed(t, forum, "3zzz") {
		t.Errorf("Expected the record that arrived after its takedown label to be hidden.")
	}

	// The subject's hides are gone (as after a purge of the subject's repo); the record comes back; re-evaluation restores the hide.
	forum.Exec(t, `DELETE FROM hides`)
	changed := inTx(t, forum, func(tx *sql.Tx) (bool, error) {
		return libdb.ReevaluateRecordTx(forum.Logger, tx, libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3zzz", labelNow)
	})
	if !changed {
		t.Errorf("Expected re-evaluation to restore the hide.")
	}
	if recordServed(t, forum, "3zzz") {
		t.Errorf("Expected the record to be hidden after re-evaluation.")
	}
}

// After the authority's repo is replaced (a new generation without the label), the hide goes.
func TestLabels_generationFlip(t *testing.T) {
	forum := newLabelForum(t)

	putLabel(t, forum, labelKey(libtestfixtures.Authority, "3lab"), libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-24T00:00:00Z"})

	// A label staged in the new generation, for another record, takes effect at the flip — not before.
	{
		key := labelKey(libtestfixtures.Authority, "3lab2")
		key.Generation = 1
		putLabel(t, forum, key, libdb.Label{URI: "at://" + libtestfixtures.Alice + "/com.atmoboards.thread/3aaa", Val: "!takedown", CTS: "2026-11-24T00:00:00Z"})
	}
	if !recordServed(t, forum, "3aaa") {
		t.Fatalf("Did not expect a label still being staged to take effect.")
	}

	changed := inTx(t, forum, func(tx *sql.Tx) (bool, error) {
		if _, err := tx.Exec(`UPDATE repos SET generation = 1 WHERE did = ?`, libtestfixtures.Authority); nil != err {
			return false, err
		}
		return libdb.ReevaluateLabelsAfterFlipTx(forum.Logger, tx, libtestfixtures.Space, libtestfixtures.Authority, 0, labelNow)
	})
	if !changed {
		t.Errorf("Expected the flip to change hides.")
	}
	if !recordServed(t, forum, "3bbb") {
		t.Errorf("Expected the record labelled only in the old generation to be served again.")
	}
	if recordServed(t, forum, "3aaa") {
		t.Errorf("Expected the record labelled in the new generation to be hidden.")
	}
}

func TestLabels_sweepExpired(t *testing.T) {
	forum := newLabelForum(t)

	putLabel(t, forum, labelKey(libtestfixtures.Authority, "3lab"), libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-24T00:00:00Z", Exp: "2026-11-24T06:30:00Z"})
	if recordServed(t, forum, "3bbb") {
		t.Fatalf("Expected the record to be hidden before its label expires.")
	}

	for _, test := range []struct {
		Now             int64
		ExpectedChanged bool
		ExpectedServed  bool
	}{
		{Now: labelNow, ExpectedChanged: false, ExpectedServed: false},
		{Now: labelNow + 3600, ExpectedChanged: true, ExpectedServed: true},
		{Now: labelNow + 7200, ExpectedChanged: false, ExpectedServed: true},
	} {
		changed, err := libdb.SweepExpiredLabels(forum.Logger, forum.DB, test.Now)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if test.ExpectedChanged != changed {
			t.Errorf("At %d, expected changed = %t but actually got %t.", test.Now, test.ExpectedChanged, changed)
		}
		if actual := recordServed(t, forum, "3bbb"); test.ExpectedServed != actual {
			t.Errorf("At %d, expected served = %t but actually got %t.", test.Now, test.ExpectedServed, actual)
		}
	}

	if expected, actual := []string{"hidden record", "visible record"}, changeKinds(t, forum); !reflect.DeepEqual(expected, actual) {
		t.Errorf("The actual change log is not what was expected.")
		t.Logf("EXPECTED: %#v", expected)
		t.Logf("ACTUAL:   %#v", actual)
	}
}

// An operator hide and a label hide on the same record: the record stays hidden until both are gone.
func TestLabels_withOperatorHide(t *testing.T) {
	forum := newLabelForum(t)

	if _, err := libdb.AddHide(forum.Logger, forum.DB, libdb.HideScope{Kind: libdb.ScopeRecord, SpaceURI: libtestfixtures.Space, DID: libtestfixtures.Alice, Collection: "com.atmoboards.thread", RKey: "3bbb"}, libdb.ReasonOperator, labelNow); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	putLabel(t, forum, labelKey(libtestfixtures.Authority, "3lab"), libdb.Label{URI: subject3bbb, Val: "!takedown", CTS: "2026-11-24T00:00:00Z"})
	inTx(t, forum, func(tx *sql.Tx) (bool, error) {
		return libdb.DeleteLabelTx(forum.Logger, tx, labelKey(libtestfixtures.Authority, "3lab"), labelNow)
	})

	if recordServed(t, forum, "3bbb") {
		t.Errorf("Expected the operator hide to keep the record hidden.")
	}
	if expected, actual := []string{"hidden record"}, changeKinds(t, forum); !reflect.DeepEqual(expected, actual) {
		t.Errorf("The actual change log is not what was expected.")
		t.Logf("EXPECTED: %#v", expected)
		t.Logf("ACTUAL:   %#v", actual)
	}
}
