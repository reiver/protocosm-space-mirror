package verboten_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/testfixtures"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/www/private/xrpc/getChanges"
)

type internalResponse struct {
	Cursor  string `json:"cursor"`
	Changes []struct{
		Seq   int64  `json:"seq"`
		Kind  string `json:"kind"`
		Scope string `json:"scope"`
		Space string `json:"space"`
		Repo  string `json:"repo"`
	} `json:"changes"`
	Error string `json:"error"`
}

func get(t *testing.T, handler http.Handler, params url.Values) (int, internalResponse) {
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

func TestServeHTTP(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

	{
		// An empty change log.
		code, response := get(t, handler, url.Values{})
		if http.StatusOK != code || "0" != response.Cursor || 0 != len(response.Changes) {
			t.Errorf("Expected an empty page with cursor \"0\": %d %#v", code, response)
		}
	}

	for _, scope := range []libdb.HideScope{
		{Kind: libdb.ScopeSpace, SpaceURI: libtestfixtures.Space},
		{Kind: libdb.ScopeAccount, DID: libtestfixtures.Bob},
		{Kind: libdb.ScopeRepo, SpaceURI: libtestfixtures.Space, DID: libtestfixtures.Alice},
	} {
		if _, err := libdb.AddHide(forum.Logger, forum.DB, scope, libdb.ReasonOperator, 1795435200); nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
	}
	// (The repo hide is inside a hidden space, so it adds no entry: 2 entries in all.)

	{
		code, response := get(t, handler, url.Values{"limit": {"1"}})
		if http.StatusOK != code || 1 != len(response.Changes) || "1" != response.Cursor || "hidden" != response.Changes[0].Kind || "space" != response.Changes[0].Scope {
			t.Errorf("The actual first page is not what was expected: %d %#v", code, response)
		}
	}
	{
		code, response := get(t, handler, url.Values{"since": {"1"}})
		if http.StatusOK != code || 1 != len(response.Changes) || "2" != response.Cursor || "account" != response.Changes[0].Scope || libtestfixtures.Bob != response.Changes[0].Repo {
			t.Errorf("The actual second page is not what was expected: %d %#v", code, response)
		}
	}
	{
		code, response := get(t, handler, url.Values{"since": {"2"}})
		if http.StatusOK != code || 0 != len(response.Changes) || "2" != response.Cursor {
			t.Errorf("Expected nothing new, and the cursor to stay at 2: %d %#v", code, response)
		}
	}

	forum.Exec(t, `UPDATE meta SET changelog_floor_seq = 2`)
	{
		code, response := get(t, handler, url.Values{"since": {"1"}})
		if http.StatusBadRequest != code || libxrpc.ErrorCursorTooOld != response.Error {
			t.Errorf("Expected CursorTooOld: %d %#v", code, response)
		}
	}
	{
		code, response := get(t, handler, url.Values{"since": {"-1"}})
		if http.StatusBadRequest != code || libxrpc.ErrorInvalidRequest != response.Error {
			t.Errorf("Expected InvalidRequest: %d %#v", code, response)
		}
	}
}
