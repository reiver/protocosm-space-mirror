package libnsid_test

import (
	"testing"

	"github.com/reiver/go-nsid"

	"protocosm-space-mirror/lib/nsid"
)

// TestNSIDs makes sure every XRPC method name built from the (configurable) prefixes is a valid NSID,
// so that a typo in a prefix constant fails the tests.
func TestNSIDs(t *testing.T) {

	tests := []struct{
		Prefix string
		Name   string
	}{
		{
			Prefix: libnsid.NSIDPrefixRepo,
			Name:   "getRecord",
		},
		{
			Prefix: libnsid.NSIDPrefixRepo,
			Name:   "listRecords",
		},
		{
			Prefix: libnsid.NSIDPrefixRepo,
			Name:   "getBlob",
		},
		{
			Prefix: libnsid.NSIDPrefixRepo,
			Name:   "listBlobs",
		},



		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "listWriters",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "listSpaceRecords",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "getChanges",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "subscribeChanges",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "getStatus",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "hideSpace",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "unhideSpace",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "hideAccount",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "unhideAccount",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "hideRecord",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "unhideRecord",
		},
		{
			Prefix: libnsid.NSIDPrefix,
			Name:   "reloadSpaces",
		},
	}

	for testNumber, test := range tests {

		var method string = test.Prefix + "." + test.Name

		err := nsid.Validate(method)
		if nil != err {
			t.Errorf("For test #%d, did not expect an error but actually got one.", testNumber)
			t.Logf("ERROR: (%T) %s", err, err)
			t.Logf("METHOD: %q", method)
			continue
		}
	}
}
