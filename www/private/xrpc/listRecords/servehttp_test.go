package verboten_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"protocosm-space-mirror/lib/testfixtures"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/www/private/xrpc/listRecords"
)

type internalResponse struct {
	Cursor  string `json:"cursor"`
	Records []struct{
		Collection string          `json:"collection"`
		RKey       string          `json:"rkey"`
		CID        string          `json:"cid"`
		Value      json.RawMessage `json:"value"`
	} `json:"records"`
	Error string `json:"error"`
}

func list(t *testing.T, handler http.Handler, params url.Values) (int, internalResponse) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, verboten.Path + "?" + params.Encode(), nil)
	req.Header.Set("Authorization", "Bearer " + libtestfixtures.ReadToken)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	var response internalResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); nil != err {
		t.Fatalf("Did not expect an error parsing the body but actually got one: (%T) %s\nBODY: %s", err, err, recorder.Body.String())
	}
	return recorder.Code, response
}

func paths(response internalResponse) []string {
	var result []string
	for _, record := range response.Records {
		result = append(result, record.Collection + "/" + record.RKey)
	}
	return result
}

func TestServeHTTP_paging(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

	base := url.Values{"space": {libtestfixtures.Space}, "repo": {libtestfixtures.Alice}, "limit": {"2"}}

	code, page1 := list(t, handler, base)
	if http.StatusOK != code {
		t.Fatalf("Expected status 200 but actually got %d (%s).", code, page1.Error)
	}
	if expected := []string{"com.atmoboards.reply/3ccc", "com.atmoboards.thread/3aaa"}; !reflect.DeepEqual(expected, paths(page1)) {
		t.Errorf("The actual first page is not what was expected.")
		t.Logf("EXPECTED: %v", expected)
		t.Logf("ACTUAL:   %v", paths(page1))
	}
	if "com.atmoboards.thread/3aaa" != page1.Cursor {
		t.Errorf("The actual cursor is not what was expected: %q", page1.Cursor)
	}
	for _, record := range page1.Records {
		if 0 == len(record.Value) {
			t.Errorf("Expected values by default.")
		}
	}

	next := url.Values{"space": base["space"], "repo": base["repo"], "limit": base["limit"], "cursor": {page1.Cursor}}
	code, page2 := list(t, handler, next)
	if http.StatusOK != code {
		t.Fatalf("Expected status 200 but actually got %d (%s).", code, page2.Error)
	}
	if expected := []string{"com.atmoboards.thread/3bbb"}; !reflect.DeepEqual(expected, paths(page2)) {
		t.Errorf("The actual second page is not what was expected.")
		t.Logf("EXPECTED: %v", expected)
		t.Logf("ACTUAL:   %v", paths(page2))
	}
	if "" != page2.Cursor {
		t.Errorf("Did not expect a cursor on the last page, but got %q.", page2.Cursor)
	}
}

func TestServeHTTP_filtersAndOptions(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	forum.Exec(t, `INSERT INTO hides (scope_kind, space_uri, did, collection, rkey, reason, since) VALUES ('record', ?, ?, 'com.atmoboards.thread', '3bbb', 'operator', 1)`, libtestfixtures.Space, libtestfixtures.Alice)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

	code, response := list(t, handler, url.Values{
		"space":         {libtestfixtures.Space},
		"repo":          {libtestfixtures.Alice},
		"collection":    {"com.atmoboards.thread"},
		"excludeValues": {"true"},
	})
	if http.StatusOK != code {
		t.Fatalf("Expected status 200 but actually got %d (%s).", code, response.Error)
	}

	// 3bbb is hidden; 3ccc is a reply.
	if expected := []string{"com.atmoboards.thread/3aaa"}; !reflect.DeepEqual(expected, paths(response)) {
		t.Errorf("The actual records are not what was expected.")
		t.Logf("EXPECTED: %v", expected)
		t.Logf("ACTUAL:   %v", paths(response))
	}
	for _, record := range response.Records {
		if 0 != len(record.Value) {
			t.Errorf("Did not expect values with excludeValues=true.")
		}
	}
}

func TestServeHTTP_errors(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

	tests := []struct{
		Params url.Values
		ExpectedError string
	}{
		{
			Params:        url.Values{"space": {libtestfixtures.Space}, "repo": {libtestfixtures.Alice}, "cursor": {"no-slash"}},
			ExpectedError: libxrpc.ErrorInvalidCursor,
		},
		{
			Params:        url.Values{"space": {libtestfixtures.Space}, "repo": {libtestfixtures.Alice}, "limit": {"0"}},
			ExpectedError: libxrpc.ErrorInvalidRequest,
		},
		{
			Params:        url.Values{"space": {libtestfixtures.Space}, "repo": {libtestfixtures.Alice}, "limit": {"101"}},
			ExpectedError: libxrpc.ErrorInvalidRequest,
		},
		{
			Params:        url.Values{"space": {libtestfixtures.Space}, "repo": {libtestfixtures.Alice}, "excludeValues": {"yes"}},
			ExpectedError: libxrpc.ErrorInvalidRequest,
		},
		{
			Params:        url.Values{"space": {libtestfixtures.Space}, "repo": {"did:example:nobody"}},
			ExpectedError: libxrpc.ErrorRepoNotFound,
		},
	}

	for testNumber, test := range tests {

		code, response := list(t, handler, test.Params)

		if http.StatusBadRequest != code || test.ExpectedError != response.Error {
			t.Errorf("For test #%d, the actual error is not what was expected.", testNumber)
			t.Logf("EXPECTED: 400 %s", test.ExpectedError)
			t.Logf("ACTUAL:   %d %s", code, response.Error)
			continue
		}
	}
}
