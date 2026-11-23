package libxrpc

import (
	"time"

	"github.com/reiver/go-opt"
)

// FormatTime formats a unix-seconds time as RFC 3339 (UTC), or "" if it is nothing.
func FormatTime(unixSeconds opt.Optional[int64]) string {
	value, found := unixSeconds.Get()
	if !found {
		return ""
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}
