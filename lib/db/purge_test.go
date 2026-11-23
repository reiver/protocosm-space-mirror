package libdb_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"codeberg.org/reiver/go-erorr"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/testfixtures"
)

const purgeNow int64 = 1795500000

var (
	purgeSpace   = libdb.PurgeScope{Kind: libdb.ScopeSpace, SpaceURI: libtestfixtures.Space}
	purgeRepo    = libdb.PurgeScope{Kind: libdb.ScopeRepo, SpaceURI: libtestfixtures.Space, DID: libtestfixtures.Alice}
	purgeAccount = libdb.PurgeScope{Kind: libdb.ScopeAccount, DID: libtestfixtures.Alice}
)

func count(t *testing.T, forum libtestfixtures.Forum, query string, args ...any) int {
	t.Helper()

	var n int
	if err := forum.DB.QueryRow(query, args...).Scan(&n); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s\nQUERY: %s", err, err, query)
	}
	return n
}

// changeKinds returns "<kind> <scope>" for every change-log entry, in seq order.
func changeKinds(t *testing.T, forum libtestfixtures.Forum) []string {
	t.Helper()

	changes, err := libdb.ChangesList(forum.Logger, forum.DB, 0, 1000)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	var kinds []string
	for _, change := range changes {
		kinds = append(kinds, change.Kind+" "+change.Scope)
	}
	return kinds
}

func onlyJob(t *testing.T, forum libtestfixtures.Forum) libdb.PurgeJob {
	t.Helper()

	jobs, err := libdb.PurgeJobsList(forum.Logger, forum.DB)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if 1 != len(jobs) {
		t.Fatalf("Expected exactly 1 purge job but actually got %d: %#v", len(jobs), jobs)
	}
	return jobs[0]
}

func startPurge(t *testing.T, forum libtestfixtures.Forum, scope libdb.PurgeScope, reason string) {
	t.Helper()

	if err := libdb.StartPurge(forum.Logger, forum.DB, scope, reason, purgeNow); nil != err {
		t.Fatalf("Did not expect an error from StartPurge but actually got one: (%T) %s", err, err)
	}
}

func runPurge(t *testing.T, forum libtestfixtures.Forum, job libdb.PurgeJob) {
	t.Helper()

	if _, err := libdb.RunPurge(forum.Logger, forum.DB, forum.BlobDir, job, purgeNow); nil != err {
		t.Fatalf("Did not expect an error from RunPurge but actually got one: (%T) %s", err, err)
	}
}

func TestPurge_space(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	var blobFile string = filepath.Join(forum.BlobDir, forum.BlobCID)
	if _, err := os.Stat(blobFile); nil != err {
		t.Fatalf("Expected the fixture's blob file to exist: (%T) %s", err, err)
	}

	// Some change-log history about the space.
	if _, err := libdb.AddHide(forum.Logger, forum.DB, scopeRecord, libdb.ReasonLabelTakedown, purgeNow); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	startPurge(t, forum, purgeSpace, libdb.PurgeReasonUnlisted)

	// Hidden at once.
	{
		visible, err := libdb.SpaceVisible(forum.Logger, forum.DB, libtestfixtures.Space)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if visible {
			t.Errorf("Expected the space to be hidden as soon as the purge started.")
		}

		if expected, actual := []string{"hidden record", "hidden space"}, changeKinds(t, forum); !reflect.DeepEqual(expected, actual) {
			t.Errorf("The actual change log is not what was expected.")
			t.Logf("EXPECTED: %#v", expected)
			t.Logf("ACTUAL:   %#v", actual)
		}
	}

	job := onlyJob(t, forum)
	runPurge(t, forum, job)

	for _, table := range []string{"records", "blob_refs", "blobs", "labels", "repos", "hides", "purge_jobs"} {
		if n := count(t, forum, `SELECT COUNT(*) FROM `+table); 0 != n {
			t.Errorf("Expected table %q to be empty after the purge but it has %d row(s).", table, n)
		}
	}

	if _, err := os.Stat(blobFile); !os.IsNotExist(err) {
		t.Errorf("Expected the blob file to be deleted, but: %v", err)
	}

	// The earlier entries are gone; only the purge event is left — and no "visible" entry.
	if expected, actual := []string{"spacePurged space"}, changeKinds(t, forum); !reflect.DeepEqual(expected, actual) {
		t.Errorf("The actual change log is not what was expected.")
		t.Logf("EXPECTED: %#v", expected)
		t.Logf("ACTUAL:   %#v", actual)
	}

	// Still listed (the fixture's space is), so it can come back.
	if n := count(t, forum, `SELECT COUNT(*) FROM spaces WHERE space_uri = ? AND 'unavailable' = lifecycle AND 0 = had_success AND verified_at IS NULL`, libtestfixtures.Space); 1 != n {
		t.Errorf("Expected the purged (still listed) space to be reset to unavailable.")
	}
}

func TestPurge_unlistedSpaceBecomesPurged(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	forum.Exec(t, `UPDATE spaces SET listed = 0`)

	startPurge(t, forum, purgeSpace, libdb.PurgeReasonUnlisted)
	runPurge(t, forum, onlyJob(t, forum))

	if n := count(t, forum, `SELECT COUNT(*) FROM spaces WHERE 'purged' = lifecycle`); 1 != n {
		t.Errorf("Expected the unlisted space's lifecycle to be 'purged'.")
	}
}

func TestPurge_repo(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	startPurge(t, forum, purgeRepo, libdb.PurgeReasonWriterRemoved)

	{
		visible, err := libdb.RepoVisible(forum.Logger, forum.DB, libtestfixtures.Space, libtestfixtures.Alice)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if visible {
			t.Errorf("Expected the repo to be hidden as soon as the purge started.")
		}
	}

	runPurge(t, forum, onlyJob(t, forum))

	if n := count(t, forum, `SELECT COUNT(*) FROM records WHERE did = ?`, libtestfixtures.Alice); 0 != n {
		t.Errorf("Expected none of Alice's records to be left, but %d are.", n)
	}
	if n := count(t, forum, `SELECT COUNT(*) FROM records WHERE did = ?`, libtestfixtures.Bob); 1 != n {
		t.Errorf("Expected Bob's record to be kept, but %d are left.", n)
	}
	if n := count(t, forum, `SELECT COUNT(*) FROM repos`); 1 != n {
		t.Errorf("Expected only Bob's repo to be left, but %d repos are.", n)
	}
	if n := count(t, forum, `SELECT COUNT(*) FROM blobs`); 0 != n {
		t.Errorf("Expected Alice's (no longer referenced) blob to be deleted.")
	}
	if expected, actual := []string{"writerRemoved repo"}, changeKinds(t, forum); !reflect.DeepEqual(expected, actual) {
		t.Errorf("The actual change log is not what was expected.")
		t.Logf("EXPECTED: %#v", expected)
		t.Logf("ACTUAL:   %#v", actual)
	}
}

// A blob still referenced from somewhere else is kept.
func TestPurge_sharedBlobKept(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	forum.Exec(t, `INSERT INTO blob_refs (space_uri, did, generation, collection, rkey, cid) VALUES (?, ?, 0, 'com.atmoboards.thread', '3ddd', ?)`,
		libtestfixtures.Space, libtestfixtures.Bob, forum.BlobCID)

	startPurge(t, forum, purgeRepo, libdb.PurgeReasonWriterRemoved)
	runPurge(t, forum, onlyJob(t, forum))

	if n := count(t, forum, `SELECT COUNT(*) FROM blobs WHERE 'fetched' = state`); 1 != n {
		t.Errorf("Expected the shared blob to be kept.")
	}
	if _, err := os.Stat(filepath.Join(forum.BlobDir, forum.BlobCID)); nil != err {
		t.Errorf("Expected the shared blob's file to be kept: (%T) %s", err, err)
	}
}

func TestPurge_accountThatIsAnAuthority(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	startPurge(t, forum, libdb.PurgeScope{Kind: libdb.ScopeAccount, DID: libtestfixtures.Authority}, libdb.PurgeReasonAccountDeleted)

	jobs, err := libdb.PurgeJobsList(forum.Logger, forum.DB)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	var reasons = map[string]string{}
	for _, job := range jobs {
		reasons[job.Kind] = job.Reason
	}
	if expected := map[string]string{"account": "account-deleted", "space": "authority-deleted"}; !reflect.DeepEqual(expected, reasons) {
		t.Errorf("The actual purge jobs are not what was expected.")
		t.Logf("EXPECTED: %#v", expected)
		t.Logf("ACTUAL:   %#v", reasons)
	}

	for _, job := range jobs {
		runPurge(t, forum, job)
	}

	if n := count(t, forum, `SELECT COUNT(*) FROM records`); 0 != n {
		t.Errorf("Expected the authority's space to be purged, but %d records are left.", n)
	}
}

// A second trigger for a scope already being purged keeps the job (epoch, start), and a deletion reason wins.
func TestPurge_overlappingTriggers(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	startPurge(t, forum, purgeSpace, libdb.PurgeReasonUnlisted)
	first := onlyJob(t, forum)

	if err := libdb.StartPurge(forum.Logger, forum.DB, purgeSpace, libdb.PurgeReasonSpaceDeleted, purgeNow+60); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	second := onlyJob(t, forum)

	if libdb.PurgeReasonSpaceDeleted != second.Reason {
		t.Errorf("Expected the deletion reason to win, but the reason is %q.", second.Reason)
	}
	if first.Epoch != second.Epoch || first.StartedAt != second.StartedAt {
		t.Errorf("Expected the job's epoch and start time to be kept: first %#v; second %#v", first, second)
	}

	if err := libdb.StartPurge(forum.Logger, forum.DB, purgeSpace, libdb.PurgeReasonUnlisted, purgeNow+120); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if third := onlyJob(t, forum); libdb.PurgeReasonSpaceDeleted != third.Reason {
		t.Errorf("Expected a weaker reason not to replace a deletion reason, but the reason is %q.", third.Reason)
	}
}

// Operator hides are policy, not data: kept — except record ones, when the data was deleted.
func TestPurge_operatorHides(t *testing.T) {
	tests := []struct {
		Reason           string
		ExpectedRecord   int
		ExpectedRepo     int
	}{
		{Reason: libdb.PurgeReasonUnlisted, ExpectedRecord: 1, ExpectedRepo: 1},
		{Reason: libdb.PurgeReasonSpaceDeleted, ExpectedRecord: 0, ExpectedRepo: 1},
	}

	for testNumber, test := range tests {
		forum := libtestfixtures.NewForum(t)

		for _, scope := range []libdb.HideScope{scopeRecord, scopeRepo} {
			if _, err := libdb.AddHide(forum.Logger, forum.DB, scope, libdb.ReasonOperator, purgeNow); nil != err {
				t.Fatalf("For test #%d, did not expect an error but actually got one: (%T) %s", testNumber, err, err)
			}
		}
		if _, err := libdb.AddHide(forum.Logger, forum.DB, scopeRepo, libdb.ReasonOverQuota, purgeNow); nil != err {
			t.Fatalf("For test #%d, did not expect an error but actually got one: (%T) %s", testNumber, err, err)
		}

		startPurge(t, forum, purgeSpace, test.Reason)
		runPurge(t, forum, onlyJob(t, forum))

		if actual := count(t, forum, `SELECT COUNT(*) FROM hides WHERE 'record' = scope_kind`); test.ExpectedRecord != actual {
			t.Errorf("For test #%d (%s), expected %d record hide(s) but actually got %d.", testNumber, test.Reason, test.ExpectedRecord, actual)
		}
		if actual := count(t, forum, `SELECT COUNT(*) FROM hides WHERE 'repo' = scope_kind`); test.ExpectedRepo != actual {
			t.Errorf("For test #%d (%s), expected %d repo hide(s) but actually got %d.", testNumber, test.Reason, test.ExpectedRepo, actual)
		}
		if actual := count(t, forum, `SELECT COUNT(*) FROM hides WHERE 'operator' <> reason`); 0 != actual {
			t.Errorf("For test #%d (%s), expected no non-operator hides to be left but actually got %d.", testNumber, test.Reason, actual)
		}
	}
}

// A writer re-added while its repo was being purged (a target recorded after the purge started) keeps its repo row — reset.
func TestPurge_targetAfterPurgeStartKept(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	startPurge(t, forum, purgeSpace, libdb.PurgeReasonDenied)
	job := onlyJob(t, forum)

	forum.Exec(t, `UPDATE repos SET target_epoch = ?, target_rev = 'rev-new', rev = 'rev-old' WHERE did = ?`, job.Epoch+1, libtestfixtures.Bob)

	runPurge(t, forum, job)

	var generation int
	var rev, targetRev string
	if err := forum.DB.QueryRow(`SELECT generation, rev, target_rev FROM repos WHERE did = ?`, libtestfixtures.Bob).Scan(&generation, &rev, &targetRev); nil != err {
		t.Fatalf("Expected Bob's repo row to be kept, but: (%T) %s", err, err)
	}
	if 1 != generation || "" != rev || "rev-new" != targetRev {
		t.Errorf("Expected Bob's repo to be reset (new generation, no rev) with its target kept; got generation=%d rev=%q target_rev=%q.", generation, rev, targetRev)
	}
	if n := count(t, forum, `SELECT COUNT(*) FROM repos WHERE did = ?`, libtestfixtures.Alice); 0 != n {
		t.Errorf("Expected Alice's repo row to be deleted.")
	}
}

// A purge interrupted part way — here: after its event was written, and after a blob row was marked "deleting" and its file deleted —
// finishes when run again, with exactly one purge event.
func TestPurge_resume(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	startPurge(t, forum, purgeSpace, libdb.PurgeReasonUnlisted)

	forum.Exec(t, `DELETE FROM records`)
	forum.Exec(t, `DELETE FROM blob_refs`)
	forum.Exec(t, `UPDATE blobs SET state = 'deleting'`)
	if err := os.Remove(filepath.Join(forum.BlobDir, forum.BlobCID)); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	forum.Exec(t, `INSERT INTO changelog (at, kind, scope, space_uri, reason) VALUES (?, 'spacePurged', 'space', ?, 'unlisted')`, purgeNow, libtestfixtures.Space)
	forum.Exec(t, `UPDATE purge_jobs SET event_seq = (SELECT MAX(seq) FROM changelog)`)

	job := onlyJob(t, forum)
	runPurge(t, forum, job)

	if expected, actual := []string{"spacePurged space"}, changeKinds(t, forum); !reflect.DeepEqual(expected, actual) {
		t.Errorf("The actual change log is not what was expected.")
		t.Logf("EXPECTED: %#v", expected)
		t.Logf("ACTUAL:   %#v", actual)
	}
	for _, table := range []string{"blobs", "purge_jobs", "hides"} {
		if n := count(t, forum, `SELECT COUNT(*) FROM `+table); 0 != n {
			t.Errorf("Expected table %q to be empty after the resumed purge but it has %d row(s).", table, n)
		}
	}
}

func TestPurge_fence(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	check := func(fence libdb.Fence) error {
		t.Helper()

		tx, err := forum.DB.Begin()
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		defer tx.Rollback()

		return libdb.CheckFence(tx, fence)
	}

	fence, err := libdb.ReadFence(forum.Logger, forum.DB, libtestfixtures.Space, libtestfixtures.Bob)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if err := check(fence); nil != err {
		t.Fatalf("Did not expect a fresh fence to be fenced, but: (%T) %s", err, err)
	}

	// A purge of Alice's account does not fence work on Bob's repo.
	startPurge(t, forum, purgeAccount, libdb.PurgeReasonAccountDeleted)
	if err := check(fence); nil != err {
		t.Errorf("Did not expect work on Bob's repo to be fenced by a purge of Alice's account, but: (%T) %s", err, err)
	}

	// Work on Alice's repo, started before the purge, is fenced — during the purge, and after it.
	{
		aliceFence := libdb.Fence{SpaceURI: libtestfixtures.Space, DID: libtestfixtures.Alice, SpaceEpoch: 1, RepoEpoch: 2, AccountEpoch: 0}
		if err := check(aliceFence); !erorr.Is(err, libdb.ErrFenced) {
			t.Errorf("Expected work on Alice's repo to be fenced during the purge, but got: %v", err)
		}

		runPurge(t, forum, onlyJob(t, forum))

		if err := check(aliceFence); !erorr.Is(err, libdb.ErrFenced) {
			t.Errorf("Expected work on Alice's repo, started before the purge, to be fenced after it, but got: %v", err)
		}
	}

	// A purge of the space fences everything in it.
	startPurge(t, forum, purgeSpace, libdb.PurgeReasonUnlisted)
	if err := check(fence); !erorr.Is(err, libdb.ErrFenced) {
		t.Errorf("Expected work on Bob's repo to be fenced by a purge of the space, but got: %v", err)
	}
}

func TestPurge_due(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	if _, err := libdb.ApplySpacesList(forum.Logger, forum.DB, nil, []string{libtestfixtures.Space}, nil, purgeNow, purgeNow+3600); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	for _, test := range []struct {
		Now      int64
		Expected []libdb.DuePurge
	}{
		{Now: purgeNow + 3599, Expected: nil},
		{Now: purgeNow + 3600, Expected: []libdb.DuePurge{{PurgeScope: purgeSpace, Reason: libdb.PurgeReasonUnlisted}}},
	} {
		actual, err := libdb.DuePurgesList(forum.Logger, forum.DB, test.Now)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		if !reflect.DeepEqual(test.Expected, actual) {
			t.Errorf("At %d, the actual due purges are not what was expected.", test.Now)
			t.Logf("EXPECTED: %#v", test.Expected)
			t.Logf("ACTUAL:   %#v", actual)
		}
	}

	// Once started, it is not due any more; once done, its hide is gone and the space is 'purged'.
	startPurge(t, forum, purgeSpace, libdb.PurgeReasonUnlisted)
	if actual, _ := libdb.DuePurgesList(forum.Logger, forum.DB, purgeNow+3600); 0 != len(actual) {
		t.Errorf("Expected nothing due while the purge runs, but got: %#v", actual)
	}
	runPurge(t, forum, onlyJob(t, forum))
	if actual, _ := libdb.DuePurgesList(forum.Logger, forum.DB, purgeNow+7200); 0 != len(actual) {
		t.Errorf("Expected nothing due after the purge, but got: %#v", actual)
	}
	if n := count(t, forum, `SELECT COUNT(*) FROM spaces WHERE 'purged' = lifecycle`); 1 != n {
		t.Errorf("Expected the unlisted space to be 'purged'.")
	}

	// Re-listed: it can come back.
	if _, err := libdb.ApplySpacesList(forum.Logger, forum.DB, nil, nil, []string{libtestfixtures.Space}, purgeNow+7200, 0); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if n := count(t, forum, `SELECT COUNT(*) FROM spaces WHERE 'unavailable' = lifecycle AND 1 = listed`); 1 != n {
		t.Errorf("Expected the re-listed space to be 'unavailable' (so it is synced again).")
	}
}

func TestSweepBlobDir(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	var stray string = filepath.Join(forum.BlobDir, "partial-download.tmp")
	if err := os.WriteFile(stray, []byte("half"), 0600); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	if err := libdb.SweepBlobDir(forum.Logger, forum.DB, forum.BlobDir); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Errorf("Expected the stray file to be deleted, but: %v", err)
	}
	if _, err := os.Stat(filepath.Join(forum.BlobDir, forum.BlobCID)); nil != err {
		t.Errorf("Expected the referenced blob's file to be kept: (%T) %s", err, err)
	}

	// A missing blob directory is fine.
	if err := libdb.SweepBlobDir(forum.Logger, forum.DB, filepath.Join(forum.BlobDir, "nope")); nil != err {
		t.Errorf("Did not expect an error for a missing blob directory but actually got one: (%T) %s", err, err)
	}
}
