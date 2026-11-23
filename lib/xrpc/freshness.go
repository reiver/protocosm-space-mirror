package libxrpc

import (
	"net/http"
	"strings"
	"time"

	"protocosm-space-mirror/lib/db"
)

// The HTTP response headers that say how up-to-date the served data is.
const (
	// HeaderVerifiedAt is the last time the data was confirmed to match upstream (RFC 3339).
	HeaderVerifiedAt = "Space-Mirror-Verified-At"

	// HeaderStale is present only when the data is known to be out-of-date.
	// It is an RFC 9651 structured-field boolean with a "reason" parameter — ex: ?1; reason="repo-host-unreachable".
	HeaderStale = "Space-Mirror-Stale"
)

// SetFreshnessHeaders sets the [HeaderVerifiedAt] and [HeaderStale] response headers.
func SetFreshnessHeaders(responseWriter http.ResponseWriter, freshness libdb.Freshness) {
	if nil == responseWriter {
		return
	}

	var header http.Header = responseWriter.Header()

	if verifiedAt, found := freshness.VerifiedAt.Get(); found {
		header.Set(HeaderVerifiedAt, time.Unix(verifiedAt, 0).UTC().Format(time.RFC3339))
	}

	if 0 < len(freshness.StaleReasons) {
		// Reasons are tokens like "repo-host-unreachable" — they never contain a quote or a backslash.
		header.Set(HeaderStale, `?1; reason="` + strings.Join(freshness.StaleReasons, " ") + `"`)
	}
}
