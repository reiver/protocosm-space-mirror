package verboten_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"protocosm-space-mirror/lib/testfixtures"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/www/private/xrpc/getStatus"
)

type internalResponse struct {
	Cursor string `json:"cursor"`
	Seq    int64  `json:"seq"`
	Spaces []struct{
		Space        string   `json:"space"`
		Lifecycle    string   `json:"lifecycle"`
		Serving      bool     `json:"serving"`
		VerifiedAt   string   `json:"verifiedAt"`
		StaleReasons []string `json:"staleReasons"`
		HideReasons  []string `json:"hideReasons"`
		Repos        []struct{
			DID         string   `json:"did"`
			Visible     bool     `json:"visible"`
			HideReasons []string `json:"hideReasons"`
		} `json:"repos"`
	} `json:"spaces"`
	Error string `json:"error"`
}

func status(t *testing.T, handler http.Handler, params url.Values, token string) (int, internalResponse) {
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
	forum.Exec(t, `INSERT INTO hides (scope_kind, did, reason, since) VALUES ('account', ?, 'operator', 1)`, libtestfixtures.Bob)
	forum.Exec(t, `INSERT INTO changelog (at, kind) VALUES (1, 'record')`)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

	{
		code, response := status(t, handler, url.Values{}, libtestfixtures.ReadToken)
		if http.StatusOK != code {
			t.Fatalf("Expected status 200 but actually got %d (%s).", code, response.Error)
		}
		if 1 != response.Seq {
			t.Errorf("Expected seq 1 but actually got %d.", response.Seq)
		}
		if 1 != len(response.Spaces) {
			t.Fatalf("Expected 1 space but actually got %d.", len(response.Spaces))
		}
		space := response.Spaces[0]
		if libtestfixtures.Space != space.Space || "active" != space.Lifecycle || !space.Serving || "2026-11-23T12:00:00Z" != space.VerifiedAt {
			t.Errorf("The actual space status is not what was expected: %#v", space)
		}
		if nil != space.Repos {
			t.Errorf("Did not expect repos without includeRepos.")
		}
	}

	{
		// With includeRepos, a read token sees only visible repos.
		code, response := status(t, handler, url.Values{"space": {libtestfixtures.Space}, "includeRepos": {"true"}}, libtestfixtures.ReadToken)
		if http.StatusOK != code {
			t.Fatalf("Expected status 200 but actually got %d (%s).", code, response.Error)
		}
		repos := response.Spaces[0].Repos
		if 1 != len(repos) || libtestfixtures.Alice != repos[0].DID {
			t.Errorf("Expected only alice: %#v", repos)
		}
	}

	{
		// An admin token sees hidden repos too, with reasons.
		code, response := status(t, handler, url.Values{"space": {libtestfixtures.Space}, "includeRepos": {"true"}}, libtestfixtures.AdminToken)
		if http.StatusOK != code {
			t.Fatalf("Expected status 200 but actually got %d (%s).", code, response.Error)
		}
		repos := response.Spaces[0].Repos
		if 2 != len(repos) || libtestfixtures.Bob != repos[1].DID || repos[1].Visible || 1 != len(repos[1].HideReasons) {
			t.Errorf("Expected alice and (hidden) bob: %#v", repos)
		}
	}

	{
		code, response := status(t, handler, url.Values{"includeRepos": {"true"}}, libtestfixtures.ReadToken)
		if http.StatusBadRequest != code || libxrpc.ErrorInvalidRequest != response.Error {
			t.Errorf("Expected includeRepos without space to be rejected: %d %q", code, response.Error)
		}
	}
}
