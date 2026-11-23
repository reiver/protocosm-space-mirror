package main

import (
	"protocosm-space-mirror/srv/labels"
	"protocosm-space-mirror/srv/purge"
)

// purgeOnSchedule starts the purge timer: it purges spaces (and repos, and accounts) whose grace period is over
// — ex: spaces unlisted from the spaces file — and retries purges that failed part way.
func purgeOnSchedule() {
	go purgesrv.Timer()
}

// sweepLabelsOnSchedule starts the label timer: it un-hides records whose takedown labels have expired.
func sweepLabelsOnSchedule() {
	go labelssrv.Timer()
}
