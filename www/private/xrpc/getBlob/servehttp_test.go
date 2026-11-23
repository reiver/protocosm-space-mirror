package verboten_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"protocosm-space-mirror/lib/testfixtures"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/www/private/xrpc/getBlob"
)

func get(handler http.Handler, repo string, cid string) *httptest.ResponseRecorder {
	params := url.Values{"space": {libtestfixtures.Space}, "repo": {repo}, "cid": {cid}}
	req := httptest.NewRequest(http.MethodGet, verboten.Path + "?" + params.Encode(), nil)
	req.Header.Set("Authorization", "Bearer " + libtestfixtures.ReadToken)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func errorName(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct{
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); nil != err {
		t.Fatalf("Did not expect an error parsing the error body but actually got one: (%T) %s\nBODY: %s", err, err, recorder.Body.String())
	}
	return body.Error
}

func TestServeHTTP_ok(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens, BlobDir: forum.BlobDir}

	recorder := get(handler, libtestfixtures.Alice, forum.BlobCID)

	if http.StatusOK != recorder.Code {
		t.Fatalf("Expected status 200 but actually got %d.\nBODY: %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Equal(libtestfixtures.BlobBytes, recorder.Body.Bytes()) {
		t.Errorf("The actual blob bytes are not what was expected.")
	}

	expectedHeaders := map[string]string{
		"Content-Type":            "image/png",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'",
		libxrpc.HeaderVerifiedAt:  "2026-11-23T12:00:00Z",
	}
	for name, expected := range expectedHeaders {
		if actual := recorder.Header().Get(name); expected != actual {
			t.Errorf("The actual %s header is not what was expected.", name)
			t.Logf("EXPECTED: %q", expected)
			t.Logf("ACTUAL:   %q", actual)
		}
	}
}

func TestServeHTTP_errors(t *testing.T) {

	tests := []struct{
		Setup         string
		Repo          string
		CID           string
		ExpectedError string
	}{
		{
			// Bob's records don't refer to it.
			Repo:          libtestfixtures.Bob,
			ExpectedError: libxrpc.ErrorBlobNotFound,
		},
		{
			Repo:          libtestfixtures.Alice,
			CID:           "bafkreinotreal",
			ExpectedError: libxrpc.ErrorBlobNotFound,
		},
		{
			// The only record that refers to it is hidden.
			Setup:         `INSERT INTO hides (scope_kind, space_uri, did, collection, rkey, reason, since) VALUES ('record', '` + libtestfixtures.Space + `', '` + libtestfixtures.Alice + `', 'com.atmoboards.thread', '3aaa', 'label-takedown', 1)`,
			Repo:          libtestfixtures.Alice,
			ExpectedError: libxrpc.ErrorBlobNotFound,
		},
		{
			// Not fetched (yet).
			Setup:         `UPDATE blobs SET state = 'deferred'`,
			Repo:          libtestfixtures.Alice,
			ExpectedError: libxrpc.ErrorBlobNotAvailable,
		},
	}

	for testNumber, test := range tests {

		forum := libtestfixtures.NewForum(t)
		if "" != test.Setup {
			forum.Exec(t, test.Setup)
		}
		handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens, BlobDir: forum.BlobDir}

		var cid string = test.CID
		if "" == cid {
			cid = forum.BlobCID
		}

		recorder := get(handler, test.Repo, cid)

		if http.StatusBadRequest != recorder.Code {
			t.Errorf("For test #%d, expected status 400 but actually got %d.", testNumber, recorder.Code)
			continue
		}
		if actual := errorName(t, recorder); test.ExpectedError != actual {
			t.Errorf("For test #%d, the actual error name is not what was expected.", testNumber)
			t.Logf("EXPECTED: %q", test.ExpectedError)
			t.Logf("ACTUAL:   %q", actual)
			continue
		}
	}
}
