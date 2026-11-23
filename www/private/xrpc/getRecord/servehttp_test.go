package verboten_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"protocosm-space-mirror/lib/testfixtures"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/www/private/xrpc/getRecord"
)

func request(handler http.Handler, method string, params url.Values, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, verboten.Path + "?" + params.Encode(), nil)
	if "" != token {
		req.Header.Set("Authorization", "Bearer " + token)
	}
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

func params(space string, repo string, collection string, rkey string) url.Values {
	return url.Values{
		"space":      []string{space},
		"repo":       []string{repo},
		"collection": []string{collection},
		"rkey":       []string{rkey},
	}
}

func TestServeHTTP_ok(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

	recorder := request(handler, http.MethodGet, params(libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"), libtestfixtures.ReadToken)

	if http.StatusOK != recorder.Code {
		t.Fatalf("Expected status 200 but actually got %d.\nBODY: %s", recorder.Code, recorder.Body.String())
	}

	var body struct{
		URI   string         `json:"uri"`
		CID   string         `json:"cid"`
		Value map[string]any `json:"value"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	{
		const expected string = libtestfixtures.Space + "/" + libtestfixtures.Alice + "/com.atmoboards.thread/3aaa"
		if expected != body.URI {
			t.Errorf("The actual URI is not what was expected.")
			t.Logf("EXPECTED: %s", expected)
			t.Logf("ACTUAL:   %s", body.URI)
		}
	}
	if !strings.HasPrefix(body.CID, "bafyrei") {
		t.Errorf("Expected a DRISL (dag-cbor) SHA-256 CID but actually got %q.", body.CID)
	}
	if "Hello <everyone> & welcome" != body.Value["title"] {
		t.Errorf("The actual title is not what was expected: %#v", body.Value["title"])
	}
	{
		image, _ := body.Value["image"].(map[string]any)
		ref, _ := image["ref"].(map[string]any)
		if forum.BlobCID != ref["$link"] {
			t.Errorf("Expected the image ref to be rendered as {\"$link\": …}.")
			t.Logf("IMAGE: %#v", image)
		}
	}

	if !strings.Contains(recorder.Body.String(), "<everyone> & welcome") {
		t.Errorf("HTML characters should not be escaped in the response.")
		t.Logf("BODY: %s", recorder.Body.String())
	}

	if "2026-11-23T12:00:00Z" != recorder.Header().Get(libxrpc.HeaderVerifiedAt) {
		t.Errorf("The actual %s header is not what was expected: %q", libxrpc.HeaderVerifiedAt, recorder.Header().Get(libxrpc.HeaderVerifiedAt))
	}
	if "" != recorder.Header().Get(libxrpc.HeaderStale) {
		t.Errorf("Did not expect a %s header.", libxrpc.HeaderStale)
	}
}

func TestServeHTTP_stale(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	forum.Exec(t, `UPDATE repos SET stale_reasons = 'repo-host-unreachable' WHERE did = ?`, libtestfixtures.Alice)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

	recorder := request(handler, http.MethodGet, params(libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"), libtestfixtures.ReadToken)

	if http.StatusOK != recorder.Code {
		t.Fatalf("Expected status 200 but actually got %d.", recorder.Code)
	}
	if expected, actual := `?1; reason="repo-host-unreachable"`, recorder.Header().Get(libxrpc.HeaderStale); expected != actual {
		t.Errorf("The actual %s header is not what was expected.", libxrpc.HeaderStale)
		t.Logf("EXPECTED: %q", expected)
		t.Logf("ACTUAL:   %q", actual)
	}
}

func TestServeHTTP_errors(t *testing.T) {

	tests := []struct{
		Setup         string
		Method        string
		Params        url.Values
		Token         string
		ExpectedCode  int
		ExpectedError string
	}{
		{
			Method:        http.MethodPost,
			Params:        params(libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"),
			Token:         libtestfixtures.ReadToken,
			ExpectedCode:  http.StatusMethodNotAllowed,
			ExpectedError: libxrpc.ErrorMethodNotAllowed,
		},
		{
			Params:        params(libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"),
			Token:         "",
			ExpectedCode:  http.StatusUnauthorized,
			ExpectedError: libxrpc.ErrorAuthRequired,
		},
		{
			Params:        params(libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"),
			Token:         "wrong-token",
			ExpectedCode:  http.StatusUnauthorized,
			ExpectedError: libxrpc.ErrorAuthRequired,
		},
		{
			Params:        params(libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"),
			Token:         libtestfixtures.NoReadToken,
			ExpectedCode:  http.StatusForbidden,
			ExpectedError: libxrpc.ErrorForbidden,
		},
		{
			Params:        params("not-a-space", libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"),
			Token:         libtestfixtures.ReadToken,
			ExpectedCode:  http.StatusBadRequest,
			ExpectedError: libxrpc.ErrorInvalidRequest,
		},
		{
			Params:        params(libtestfixtures.Space, libtestfixtures.Alice, "notansid", "3aaa"),
			Token:         libtestfixtures.ReadToken,
			ExpectedCode:  http.StatusBadRequest,
			ExpectedError: libxrpc.ErrorInvalidRequest,
		},
		{
			Params:        params("at://did:example:other/space/com.atmoboards.forum/general", libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"),
			Token:         libtestfixtures.ReadToken,
			ExpectedCode:  http.StatusBadRequest,
			ExpectedError: libxrpc.ErrorSpaceNotFound,
		},
		{
			Params:        params(libtestfixtures.Space, "did:example:nobody", "com.atmoboards.thread", "3aaa"),
			Token:         libtestfixtures.ReadToken,
			ExpectedCode:  http.StatusBadRequest,
			ExpectedError: libxrpc.ErrorRepoNotFound,
		},
		{
			Params:        params(libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "nope"),
			Token:         libtestfixtures.ReadToken,
			ExpectedCode:  http.StatusBadRequest,
			ExpectedError: libxrpc.ErrorRecordNotFound,
		},



		// Hidden is the same as absent.
		{
			Setup:         `INSERT INTO hides (scope_kind, space_uri, reason, since) VALUES ('space', '` + libtestfixtures.Space + `', 'denied', 1)`,
			Params:        params(libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"),
			Token:         libtestfixtures.ReadToken,
			ExpectedCode:  http.StatusBadRequest,
			ExpectedError: libxrpc.ErrorSpaceNotFound,
		},
		{
			Setup:         `INSERT INTO hides (scope_kind, did, reason, since) VALUES ('account', '` + libtestfixtures.Alice + `', 'account-inactive:hosting', 1)`,
			Params:        params(libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"),
			Token:         libtestfixtures.ReadToken,
			ExpectedCode:  http.StatusBadRequest,
			ExpectedError: libxrpc.ErrorRepoNotFound,
		},
		{
			Setup:         `INSERT INTO hides (scope_kind, space_uri, did, collection, rkey, reason, since) VALUES ('record', '` + libtestfixtures.Space + `', '` + libtestfixtures.Alice + `', 'com.atmoboards.thread', '3aaa', 'label-takedown', 1)`,
			Params:        params(libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa"),
			Token:         libtestfixtures.ReadToken,
			ExpectedCode:  http.StatusBadRequest,
			ExpectedError: libxrpc.ErrorRecordNotFound,
		},
	}

	for testNumber, test := range tests {

		forum := libtestfixtures.NewForum(t)
		if "" != test.Setup {
			forum.Exec(t, test.Setup)
		}
		handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

		var method string = test.Method
		if "" == method {
			method = http.MethodGet
		}

		recorder := request(handler, method, test.Params, test.Token)

		if test.ExpectedCode != recorder.Code {
			t.Errorf("For test #%d, the actual status code is not what was expected.", testNumber)
			t.Logf("EXPECTED: %d", test.ExpectedCode)
			t.Logf("ACTUAL:   %d", recorder.Code)
			t.Logf("BODY: %s", recorder.Body.String())
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
