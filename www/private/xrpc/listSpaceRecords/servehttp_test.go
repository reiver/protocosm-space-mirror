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
	"protocosm-space-mirror/www/private/xrpc/listSpaceRecords"
)

type internalResponse struct {
	Cursor  string `json:"cursor"`
	Seq     *int64 `json:"seq"`
	Records []struct{
		Repo       string          `json:"repo"`
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
		result = append(result, record.Repo + " " + record.Collection + "/" + record.RKey)
	}
	return result
}

func TestServeHTTP_loadingASpace(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	forum.Exec(t, `INSERT INTO changelog (at, kind) VALUES (1, 'record'), (2, 'record')`)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

	var all []string
	var params = url.Values{"space": {libtestfixtures.Space}, "limit": {"3"}}

	code, page1 := list(t, handler, params)
	if http.StatusOK != code {
		t.Fatalf("Expected status 200 but actually got %d (%s).", code, page1.Error)
	}
	if nil == page1.Seq || 2 != *page1.Seq {
		t.Errorf("Expected the first page to carry seq 2: %v", page1.Seq)
	}
	if "" == page1.Cursor {
		t.Fatalf("Expected a cursor after a full first page.")
	}
	all = append(all, paths(page1)...)

	params.Set("cursor", page1.Cursor)
	code, page2 := list(t, handler, params)
	if http.StatusOK != code {
		t.Fatalf("Expected status 200 but actually got %d (%s).", code, page2.Error)
	}
	if nil != page2.Seq {
		t.Errorf("Did not expect a seq on a later page.")
	}
	if "" != page2.Cursor {
		t.Errorf("Did not expect a cursor on the last page: %q", page2.Cursor)
	}
	all = append(all, paths(page2)...)

	expected := []string{
		libtestfixtures.Alice + " com.atmoboards.reply/3ccc",
		libtestfixtures.Alice + " com.atmoboards.thread/3aaa",
		libtestfixtures.Alice + " com.atmoboards.thread/3bbb",
		libtestfixtures.Bob   + " com.atmoboards.thread/3ddd",
	}
	if !reflect.DeepEqual(expected, all) {
		t.Errorf("The actual records are not what was expected.")
		t.Logf("EXPECTED: %v", expected)
		t.Logf("ACTUAL:   %v", all)
	}

	for _, record := range append(page1.Records, page2.Records...) {
		if "" == record.CID || 0 == len(record.Value) {
			t.Errorf("Expected every record to have a CID and a value: %#v", record)
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
			Params:        url.Values{"space": {libtestfixtures.Space}, "cursor": {"only/two"}},
			ExpectedError: libxrpc.ErrorInvalidCursor,
		},
		{
			Params:        url.Values{"space": {libtestfixtures.Space}, "cursor": {"notadid/com.atmoboards.thread/3aaa"}},
			ExpectedError: libxrpc.ErrorInvalidCursor,
		},
		{
			Params:        url.Values{"space": {"bad"}},
			ExpectedError: libxrpc.ErrorInvalidRequest,
		},
		{
			Params:        url.Values{"space": {"at://did:example:other/space/com.atmoboards.forum/general"}},
			ExpectedError: libxrpc.ErrorSpaceNotFound,
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
