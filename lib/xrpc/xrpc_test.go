package libxrpc_test

import (
	"net/http/httptest"
	"testing"

	"github.com/reiver/go-opt"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/xrpc"
)

func TestValidSpaceRef(t *testing.T) {

	tests := []struct{
		Value string
		Expected bool
	}{
		{
		},
		{
			Value:    "at://did:example:forum/space/com.atmoboards.forum/general",
			Expected: true,
		},
		{
			Value:    "at://did:plc:abc123/space/com.example.bookmarks/self",
			Expected: true,
		},
		{
			// a record URI, not a space reference
			Value:    "at://did:example:forum/space/com.atmoboards.forum/general/did:example:alice/com.atmoboards.thread/3abc",
		},
		{
			Value:    "at://did:example:forum/com.atmoboards.thread/3abc",
		},
		{
			Value:    "https://did:example:forum/space/com.atmoboards.forum/general",
		},
		{
			Value:    "at://notadid/space/com.atmoboards.forum/general",
		},
		{
			Value:    "at://did:example:forum/space/notansid/general",
		},
		{
			Value:    "at://did:example:forum/space/com.atmoboards.forum/..",
		},
	}

	for testNumber, test := range tests {
		actual := libxrpc.ValidSpaceRef(test.Value)
		if test.Expected != actual {
			t.Errorf("For test #%d, the actual result is not what was expected.", testNumber)
			t.Logf("EXPECTED: %t", test.Expected)
			t.Logf("ACTUAL:   %t", actual)
			t.Logf("VALUE: %q", test.Value)
			continue
		}
	}
}

func TestSetFreshnessHeaders(t *testing.T) {

	tests := []struct{
		Freshness libdb.Freshness
		ExpectedVerifiedAt string
		ExpectedStale string
	}{
		{
		},
		{
			Freshness:          libdb.Freshness{VerifiedAt: opt.Something(int64(1795435200))},
			ExpectedVerifiedAt: "2026-11-23T12:00:00Z",
		},
		{
			Freshness:          libdb.Freshness{VerifiedAt: opt.Something(int64(1795435200)), StaleReasons: []string{"registration-lapsed", "repo-host-unreachable"}},
			ExpectedVerifiedAt: "2026-11-23T12:00:00Z",
			ExpectedStale:      `?1; reason="registration-lapsed repo-host-unreachable"`,
		},
	}

	for testNumber, test := range tests {
		recorder := httptest.NewRecorder()
		libxrpc.SetFreshnessHeaders(recorder, test.Freshness)

		if actual := recorder.Header().Get(libxrpc.HeaderVerifiedAt); test.ExpectedVerifiedAt != actual {
			t.Errorf("For test #%d, the actual %s is not what was expected.", testNumber, libxrpc.HeaderVerifiedAt)
			t.Logf("EXPECTED: %q", test.ExpectedVerifiedAt)
			t.Logf("ACTUAL:   %q", actual)
		}
		if actual := recorder.Header().Get(libxrpc.HeaderStale); test.ExpectedStale != actual {
			t.Errorf("For test #%d, the actual %s is not what was expected.", testNumber, libxrpc.HeaderStale)
			t.Logf("EXPECTED: %q", test.ExpectedStale)
			t.Logf("ACTUAL:   %q", actual)
		}
	}
}
