package libxrpc

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/reiver/go-nsid"
)

// ValidSpaceRef returns whether 's' looks like a space reference: at://{authority-did}/space/{space-type}/{skey}.
//
// This is a light check — the space must also be one we mirror.
func ValidSpaceRef(s string) bool {
	const prefix string = "at://"
	if !strings.HasPrefix(s, prefix) {
		return false
	}

	var parts []string = strings.Split(s[len(prefix):], "/")
	if 4 != len(parts) {
		return false
	}

	return ValidDID(parts[0]) && "space" == parts[1] && nil == nsid.Validate(parts[2]) && ValidRecordKey(parts[3])
}

// ValidDID returns whether 's' looks like a DID: did:{method}:{identifier}.
func ValidDID(s string) bool {
	var parts []string = strings.SplitN(s, ":", 3)
	if 3 != len(parts) {
		return false
	}
	return "did" == parts[0] && "" != parts[1] && "" != parts[2] && len(s) <= 2048
}

// ValidNSID returns whether 's' is a valid NSID (ex: a collection).
func ValidNSID(s string) bool {
	return nil == nsid.Validate(s)
}

// ValidRecordKey returns whether 's' is a valid record key (which is also the syntax of a space key):
// 1 to 512 characters from A-Z a-z 0-9 . - _ : ~, and not "." or "..".
func ValidRecordKey(s string) bool {
	if len(s) < 1 || 512 < len(s) {
		return false
	}
	if "." == s || ".." == s {
		return false
	}

	for _, r := range s {
		switch {
		case 'A' <= r && r <= 'Z',
		     'a' <= r && r <= 'z',
		     '0' <= r && r <= '9',
		     '.' == r, '-' == r, '_' == r, ':' == r, '~' == r:
			// Nothing here.
		default:
			return false
		}
	}

	return true
}

// Limit reads the "limit" query parameter: 'defaultLimit' if absent; an error if not an integer in 1..'maxLimit'.
func Limit(query url.Values, defaultLimit int, maxLimit int) (int, bool) {
	var s string = query.Get("limit")
	if "" == s {
		return defaultLimit, true
	}

	limit, err := strconv.Atoi(s)
	if nil != err || limit < 1 || maxLimit < limit {
		return 0, false
	}

	return limit, true
}

// Bool reads a boolean query parameter: false if absent; an error if not "true" or "false".
func Bool(query url.Values, name string) (bool, bool) {
	switch query.Get(name) {
	case "", "false":
		return false, true
	case "true":
		return true, true
	default:
		return false, false
	}
}
