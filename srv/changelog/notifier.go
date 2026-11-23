package changelogsrv

import (
	"protocosm-space-mirror/lib/changelog"
)

var notifier libchangelog.Notifier

// Notifier returns the process-wide change-log notifier.
func Notifier() *libchangelog.Notifier {
	return &notifier
}
