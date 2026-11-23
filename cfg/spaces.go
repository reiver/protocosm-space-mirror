package cfg

import (
	"os"
	"time"

	"codeberg.org/reiver/go-env"
)

// SpacesFileName returns the path of the spaces file — the list of spaces the space-mirror mirrors (one space reference per line).
func SpacesFileName() string {
	return env.GetElse[string]("SPACES_FILE", "./.space-mirror.spaces")
}

// UnlistMax returns the most spaces one start (or one reload) of the spaces file may unlist, without forcing.
// (Unlisting more than half of the listed spaces also needs forcing.)
func UnlistMax() int {
	return env.GetElse[int]("UNLIST_MAX", 10)
}

// PurgeGrace returns how long data is kept (hidden) after its space is unlisted (or access is denied), before it is purged.
func PurgeGrace() time.Duration {
	return env.GetElse("PURGE_GRACE", 72 * time.Hour)
}

// ForceUnlist returns whether the space-mirror was started with the --force-unlist flag,
// which allows this one start to unlist more spaces than the limits allow.
//
// There is deliberately no environment variable for this — so it can't stay on for later starts.
func ForceUnlist() bool {
	for _, arg := range os.Args[1:] {
		if "--force-unlist" == arg {
			return true
		}
	}
	return false
}
