package libtkn_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"protocosm-space-mirror/lib/tkn"
)

func digestHex(s string) string {
	d := sha256.Sum256([]byte(s))
	return hex.EncodeToString(d[:])
}

func TestParseTokens_and_Authenticate(t *testing.T) {
	var text string =
		"# service permissions digest\n" +
		"\n" +
		"appview read " + digestHex("appview-secret") + "\n" +
		"ops     read,admin " + digestHex("ops-secret") + "\n"

	tokens, err := libtkn.ParseTokens(text)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if 2 != len(tokens) {
		t.Fatalf("Expected 2 tokens but actually got %d.", len(tokens))
	}

	tests := []struct{
		Authorization string
		ExpectedFound bool
		ExpectedName  string
		ExpectedRead  bool
		ExpectedAdmin bool
	}{
		{
		},
		{
			Authorization: "Bearer appview-secret",
			ExpectedFound: true,
			ExpectedName:  "appview",
			ExpectedRead:  true,
			ExpectedAdmin: false,
		},
		{
			Authorization: "bearer ops-secret",
			ExpectedFound: true,
			ExpectedName:  "ops",
			ExpectedRead:  true,
			ExpectedAdmin: true,
		},
		{
			Authorization: "Bearer wrong",
		},
		{
			Authorization: "appview-secret",
		},
		{
			Authorization: "Basic appview-secret",
		},
		{
			Authorization: "Bearer ",
		},
	}

	for testNumber, test := range tests {

		token, found := libtkn.Authenticate(tokens, test.Authorization)

		if test.ExpectedFound != found {
			t.Errorf("For test #%d, the actual found is not what was expected.", testNumber)
			t.Logf("EXPECTED: %t", test.ExpectedFound)
			t.Logf("ACTUAL:   %t", found)
			t.Logf("AUTHORIZATION: %q", test.Authorization)
			continue
		}
		if !found {
			continue
		}

		if test.ExpectedName != token.Name ||
		   test.ExpectedRead != token.Has(libtkn.PermissionRead) ||
		   test.ExpectedAdmin != token.Has(libtkn.PermissionAdmin) {
			t.Errorf("For test #%d, the actual token is not what was expected.", testNumber)
			t.Logf("ACTUAL: %#v", token)
			continue
		}
	}
}

func TestParseTokens_errors(t *testing.T) {

	tests := []string{
		"appview read",
		"appview read " + digestHex("x") + " extra",
		"appview write " + digestHex("x"),
		"appview read nothex",
		"appview read abcd",
	}

	for testNumber, test := range tests {

		_, err := libtkn.ParseTokens(test)
		if nil == err {
			t.Errorf("For test #%d, expected an error but did not actually get one.", testNumber)
			t.Logf("TEXT: %q", test)
			continue
		}
	}
}
