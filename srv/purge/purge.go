package purgesrv

import (
	"flag"
	"os"
	"strings"
	"sync"
	"time"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"

	"protocosm-space-mirror/cfg"
	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/srv/changelog"
	"protocosm-space-mirror/srv/db"
	"protocosm-space-mirror/srv/log"
)

// interval is how often the purge timer looks for purges that are due.
const interval = time.Minute

// mutex makes purges run one at a time.
var mutex sync.Mutex

func init() {
	// Side-effecting init()s return early under "go test".
	if nil != flag.Lookup("test.v") || strings.HasSuffix(os.Args[0], ".test") {
		return
	}

	// At start: finish any purge a crash interrupted, then clean up the blob directory
	// — before anything else is synced or downloaded.
	if err := resume(); nil != err {
		panic(erorr.Wrap(err, "failed to resume purges at start"))
	}
	if err := libdb.SweepBlobDir(logsrv.Logger(), dbsrv.WriteDB(), cfg.BlobDir()); nil != err {
		panic(erorr.Wrap(err, "failed to sweep the blob directory at start"))
	}
}

// Start purges a scope: it is hidden at once, and its data is deleted.
func Start(scope libdb.PurgeScope, reason string) error {
	mutex.Lock()
	defer mutex.Unlock()

	if err := libdb.StartPurge(logsrv.Logger(), dbsrv.WriteDB(), scope, reason, time.Now().Unix()); nil != err {
		return err
	}
	changelogsrv.Notifier().Notify()

	return resumeLocked()
}

// Timer starts purges whose grace period is over (ex: of unlisted spaces), and retries purges that failed part way.
// It does not return.
func Timer() {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		tick()
	}
}

func tick() {
	mutex.Lock()
	defer mutex.Unlock()

	log := logsrv.Begin()
	defer log.End()

	due, err := libdb.DuePurgesList(log, dbsrv.WriteDB(), time.Now().Unix())
	if nil != err {
		return
	}

	for _, purge := range due {
		if err := libdb.StartPurge(log, dbsrv.WriteDB(), purge.PurgeScope, purge.Reason, time.Now().Unix()); nil != err {
			log.Error(field.S("failed to start due purge"), field.E(err),
				field.String("scope-kind", purge.Kind),
				field.String("space-uri", purge.SpaceURI),
				field.String("did", purge.DID),
				field.String("reason", purge.Reason),
			)
		}
	}
	if 0 < len(due) {
		changelogsrv.Notifier().Notify()
	}

	if err := resumeLocked(); nil != err {
		log.Error(field.S("failed to run purges"), field.E(err))
	}
}

func resume() error {
	mutex.Lock()
	defer mutex.Unlock()

	return resumeLocked()
}

// resumeLocked runs every purge job there is. The caller holds the mutex.
func resumeLocked() error {
	log := logsrv.Begin()
	defer log.End()

	jobs, err := libdb.PurgeJobsList(log, dbsrv.WriteDB())
	if nil != err {
		return err
	}

	var firstErr error
	for _, job := range jobs {
		_, err := libdb.RunPurge(log, dbsrv.WriteDB(), cfg.BlobDir(), job, time.Now().Unix())
		if nil != err {
			log.Error(field.S("purge failed — it will be retried"), field.E(err),
				field.String("scope-kind", job.Kind),
				field.String("space-uri", job.SpaceURI),
				field.String("did", job.DID),
			)
			if nil == firstErr {
				firstErr = err
			}
			continue
		}
		changelogsrv.Notifier().Notify()
	}

	return firstErr
}
