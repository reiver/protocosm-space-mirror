package librecords_test

import (
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/reiver/go-drisl"
	"github.com/reiver/go-opt"

	"protocosm-space-mirror/lib/records"
)

func mustCID(t *testing.T, s string) drisl.CID {
	t.Helper()

	p, err := hex.DecodeString(s)
	if nil != err {
		t.Fatalf("Did not expect an error decoding hex but actually got one: (%T) %s", err, err)
	}
	return drisl.CID(p)
}

func TestToJSON(t *testing.T) {
	p, err := drisl.Marshal(map[string]any{
		"$type": "com.atmoboards.thread",
		"title": "Hello",
	})
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	actual, err := librecords.ToJSON(p)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	const expected string = `{"$type":"com.atmoboards.thread","title":"Hello"}`

	if expected != string(actual) {
		t.Errorf("The actual JSON is not what was expected.")
		t.Logf("EXPECTED: %s", expected)
		t.Logf("ACTUAL:   %s", actual)
	}
}

func TestToJSON_notDRISL(t *testing.T) {
	_, err := librecords.ToJSON([]byte{0x18, 0x01}) // not minimal
	if nil == err {
		t.Errorf("Expected an error but did not actually get one.")
	}
}

func TestBlobCIDs(t *testing.T) {
	cid1 := mustCID(t, "015512205891b5b522d5df086d0ff0b110fbd9d21bb4fc7163af34d08286a2e846f6be03")
	cid2 := mustCID(t, "01551e208e4c7c1b99dbfd50e7a95185fead5ee1448fa904a2fdd778eaf5f2dbfd629a99")

	tests := []struct{
		Record any
		Expected []drisl.CID
	}{
		{
		},
		{
			Record:   map[string]any{"text": "no blobs"},
			Expected: nil,
		},
		{
			Record: map[string]any{
				"image": map[string]any{"$type": "blob", "ref": cid1, "mimeType": "image/png", "size": int64(1)},
			},
			Expected: []drisl.CID{cid1},
		},
		{
			// nested in an array, and a duplicate
			Record: map[string]any{
				"images": []any{
					map[string]any{"alt": "one", "image": map[string]any{"$type": "blob", "ref": cid2}},
					map[string]any{"alt": "two", "image": map[string]any{"$type": "blob", "ref": cid2}},
				},
			},
			Expected: []drisl.CID{cid2},
		},
		{
			// a CID that is not in a blob reference is not a blob
			Record:   map[string]any{"link": cid1},
			Expected: nil,
		},
	}

	for testNumber, test := range tests {

		actual := librecords.BlobCIDs(test.Record)

		if !reflect.DeepEqual(test.Expected, actual) {
			t.Errorf("For test #%d, the actual CIDs are not what was expected.", testNumber)
			t.Logf("EXPECTED: %v", test.Expected)
			t.Logf("ACTUAL:   %v", actual)
			continue
		}
	}
}

func TestParseLabel(t *testing.T) {

	tests := []struct{
		Record any
		Expected librecords.Label
	}{
		{
			Record: map[string]any{
				"$type": "com.atproto.label.record",
				"uri":   "at://did:example:forum/space/com.atmoboards.forum/general/did:example:alice/com.atmoboards.thread/3mwcj3b6uak2f",
				"val":   "!takedown",
				"cts":   "2026-11-23T12:00:00.000Z",
			},
			Expected: librecords.Label{
				URI: "at://did:example:forum/space/com.atmoboards.forum/general/did:example:alice/com.atmoboards.thread/3mwcj3b6uak2f",
				Val: "!takedown",
				CTS: "2026-11-23T12:00:00.000Z",
			},
		},
		{
			Record: map[string]any{
				"uri": "at://x",
				"cid": "bafyrei",
				"val": "spam",
				"cts": "2026-11-23T12:00:00.000Z",
				"exp": "2026-12-23T12:00:00.000Z",
			},
			Expected: librecords.Label{
				URI: "at://x",
				CID: opt.Something("bafyrei"),
				Val: "spam",
				CTS: "2026-11-23T12:00:00.000Z",
				Exp: opt.Something("2026-12-23T12:00:00.000Z"),
			},
		},
	}

	for testNumber, test := range tests {

		actual, err := librecords.ParseLabel(test.Record)
		if nil != err {
			t.Errorf("For test #%d, did not expect an error but actually got one: (%T) %s", testNumber, err, err)
			continue
		}

		if !reflect.DeepEqual(test.Expected, actual) {
			t.Errorf("For test #%d, the actual label is not what was expected.", testNumber)
			t.Logf("EXPECTED: %#v", test.Expected)
			t.Logf("ACTUAL:   %#v", actual)
			continue
		}
	}
}

func TestParseLabel_errors(t *testing.T) {

	tests := []any{
		nil,
		"not a map",
		map[string]any{},
		map[string]any{"uri": "at://x", "val": "spam"},                     // no cts
		map[string]any{"uri": "at://x", "cts": "2026"},                     // no val
		map[string]any{"val": "spam", "cts": "2026"},                       // no uri
		map[string]any{"uri": int64(1), "val": "spam", "cts": "2026"},      // wrong type
		map[string]any{"uri": "at://x", "val": "spam", "cts": "2026", "cid": int64(5)},
	}

	for testNumber, test := range tests {

		_, err := librecords.ParseLabel(test)
		if librecords.ErrNotLabel != err {
			t.Errorf("For test #%d, the actual error is not what was expected.", testNumber)
			t.Logf("EXPECTED: %v", librecords.ErrNotLabel)
			t.Logf("ACTUAL:   %v", err)
			continue
		}
	}
}
