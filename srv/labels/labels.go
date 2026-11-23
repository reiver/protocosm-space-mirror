package labelssrv

import (
	"time"

	"codeberg.org/reiver/go-field"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/srv/changelog"
	"protocosm-space-mirror/srv/db"
	"protocosm-space-mirror/srv/log"
)

// interval is how often expired takedown labels are swept.
const interval = 60 * time.Second

// Timer un-hides records whose takedown labels have expired. It does not return.
func Timer() {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		sweep()
	}
}

func sweep() {
	log := logsrv.Begin()
	defer log.End()

	changed, err := libdb.SweepExpiredLabels(log, dbsrv.WriteDB(), time.Now().Unix())
	if nil != err {
		log.Error(field.S("failed to sweep expired labels — will try again"), field.E(err))
	}
	if changed {
		changelogsrv.Notifier().Notify()
	}
}
