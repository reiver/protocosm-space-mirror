package libxrpc

import (
	"strconv"
)

// ParseSeq parses a change-log position (a non-negative decimal integer). "" means 0 (the beginning).
func ParseSeq(s string) (int64, bool) {
	if "" == s {
		return 0, true
	}

	seq, err := strconv.ParseInt(s, 10, 64)
	if nil != err || seq < 0 {
		return 0, false
	}
	return seq, true
}
