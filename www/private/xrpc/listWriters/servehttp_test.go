package verboten_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"protocosm-space-mirror/lib/testfixtures"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/www/private/xrpc/listWriters"
)

type internalResponse struct {
	Cursor string `json:"cursor"`
	Repos  []struct{
		DID         string   `json:"did"`
		Rev         string   `json:"rev"`
		Visible     *bool    `json:"visible"`
		HideReasons []string `json:"hideReasons"`
	} `json:"repos"`
	Error string `json:"error"`
}

func list(t *testing.T, handler http.Handler, params url.Values, token string) (int, internalResponse) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, verboten.Path + "?" + params.Encode(), nil)
	req.Header.Set("Authorization", "Bearer " + token)
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
	forum.Exec(t, `INSERT INTO hides (scope_kind, did, reason, since) VALUES ('account', ?, 'account-inactive:hosting', 1)`, libtestfixtures.Bob)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

	params := url.Values{"space": {libtestfixtures.Space}}

	{
		code, response := list(t, handler, params, libtestfixtures.ReadToken)
		if http.StatusOK != code {
			t.Fatalf("Expected status 200 but actually got %d (%s).", code, response.Error)
		}
		if 1 != len(response.Repos) || libtestfixtures.Alice != response.Repos[0].DID {
			t.Errorf("With a read token, expected only the visible repo (alice): %#v", response.Repos)
		}
		if nil != response.Repos[0].Visible || nil != response.Repos[0].HideReasons {
			t.Errorf("With a read token, did not expect visible/hideReasons fields.")
		}
	}

	{
		code, response := list(t, handler, params, libtestfixtures.AdminToken)
		if http.StatusOK != code {
			t.Fatalf("Expected status 200 but actually got %d (%s).", code, response.Error)
		}
		if 2 != len(response.Repos) {
			t.Fatalf("With an admin token, expected both repos: %#v", response.Repos)
		}
		bob := response.Repos[1]
		if libtestfixtures.Bob != bob.DID || nil == bob.Visible || *bob.Visible || 1 != len(bob.HideReasons) || "account-inactive:hosting" != bob.HideReasons[0] {
			t.Errorf("With an admin token, expected bob hidden with its reason: %#v", bob)
		}
	}

	{
		code, response := list(t, handler, url.Values{"space": {libtestfixtures.Space}, "limit": {"1"}}, libtestfixtures.AdminToken)
		if http.StatusOK != code || libtestfixtures.Alice != response.Cursor {
			t.Errorf("Expected a cursor after the first page: %d %q", code, response.Cursor)
		}
	}

	{
		code, response := list(t, handler, url.Values{"space": {"at://did:example:other/space/com.atmoboards.forum/general"}}, libtestfixtures.ReadToken)
		if http.StatusBadRequest != code || libxrpc.ErrorSpaceNotFound != response.Error {
			t.Errorf("Expected SpaceNotFound: %d %q", code, response.Error)
		}
	}
}
