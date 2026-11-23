package libtakedown_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"protocosm-space-mirror/lib/changelog"
	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/takedown"
	"protocosm-space-mirror/lib/testfixtures"
	"protocosm-space-mirror/lib/xrpc"
)

func serve(forum libtestfixtures.Forum, notifier *libchangelog.Notifier, scopeKind string, hide bool, method string, body string, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/xrpc/test", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer " + token)
	recorder := httptest.NewRecorder()

	libtakedown.Serve(libtakedown.Config{
		Logger:    forum.Logger,
		DB:        forum.DB,
		Tokens:    forum.Tokens,
		Notifier:  notifier,
		ScopeKind: scopeKind,
		Hide:      hide,
		Path:      "/xrpc/test",
	}, recorder, req)

	return recorder
}

func changed(t *testing.T, recorder *httptest.ResponseRecorder) bool {
	t.Helper()

	if http.StatusOK != recorder.Code {
		t.Fatalf("Expected status 200 but actually got %d.\nBODY: %s", recorder.Code, recorder.Body.String())
	}
	var body struct{
		Changed bool `json:"changed"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	return body.Changed
}

func TestServe_record(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	var notifier libchangelog.Notifier
	var wait <-chan struct{} = notifier.Wait()

	const body string = `{"space":"` + libtestfixtures.Space + `","repo":"` + libtestfixtures.Alice + `","collection":"com.atmoboards.thread","rkey":"3aaa"}`

	if !changed(t, serve(forum, &notifier, libdb.ScopeRecord, true, http.MethodPost, body, libtestfixtures.AdminToken)) {
		t.Errorf("Expected the first hide to change something.")
	}

	select {
	case <-wait:
	default:
		t.Errorf("Expected the notifier to be notified after a change.")
	}

	_, found, err := libdb.RecordSelect(forum.Logger, forum.DB, libtestfixtures.Space, libtestfixtures.Alice, "com.atmoboards.thread", "3aaa")
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if found {
		t.Errorf("Expected the record to be hidden.")
	}

	if changed(t, serve(forum, &notifier, libdb.ScopeRecord, true, http.MethodPost, body, libtestfixtures.AdminToken)) {
		t.Errorf("Expected the second hide to change nothing.")
	}

	if !changed(t, serve(forum, &notifier, libdb.ScopeRecord, false, http.MethodPost, body, libtestfixtures.AdminToken)) {
		t.Errorf("Expected the unhide to change something.")
	}

	changes, err := libdb.ChangesList(forum.Logger, forum.DB, 0, 100)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	if 2 != len(changes) || libdb.ChangeKindHidden != changes[0].Kind || libdb.ChangeKindVisible != changes[1].Kind || libdb.ReasonOperator != changes[0].Reason {
		t.Errorf("Expected a hidden then a visible change-log entry: %#v", changes)
	}
}

func TestServe_spaceAndAccount(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	if !changed(t, serve(forum, nil, libdb.ScopeSpace, true, http.MethodPost, `{"space":"` + libtestfixtures.Space + `"}`, libtestfixtures.AdminToken)) {
		t.Errorf("Expected hideSpace to change something.")
	}
	if !changed(t, serve(forum, nil, libdb.ScopeAccount, true, http.MethodPost, `{"did":"` + libtestfixtures.Bob + `"}`, libtestfixtures.AdminToken)) {
		t.Errorf("Expected hideAccount to change something.")
	}

	if visible, _ := libdb.SpaceVisible(forum.Logger, forum.DB, libtestfixtures.Space); visible {
		t.Errorf("Expected the space to be hidden.")
	}
}

func TestServe_errors(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	tests := []struct{
		ScopeKind     string
		Method        string
		Body          string
		Token         string
		ExpectedCode  int
		ExpectedError string
	}{
		{
			ScopeKind: libdb.ScopeSpace, Method: http.MethodGet, Body: ``, Token: libtestfixtures.AdminToken,
			ExpectedCode: http.StatusMethodNotAllowed, ExpectedError: libxrpc.ErrorMethodNotAllowed,
		},
		{
			// read is not enough
			ScopeKind: libdb.ScopeSpace, Method: http.MethodPost, Body: `{"space":"` + libtestfixtures.Space + `"}`, Token: libtestfixtures.ReadToken,
			ExpectedCode: http.StatusForbidden, ExpectedError: libxrpc.ErrorForbidden,
		},
		{
			ScopeKind: libdb.ScopeSpace, Method: http.MethodPost, Body: `not json`, Token: libtestfixtures.AdminToken,
			ExpectedCode: http.StatusBadRequest, ExpectedError: libxrpc.ErrorInvalidRequest,
		},
		{
			// unknown field
			ScopeKind: libdb.ScopeSpace, Method: http.MethodPost, Body: `{"space":"` + libtestfixtures.Space + `","oops":1}`, Token: libtestfixtures.AdminToken,
			ExpectedCode: http.StatusBadRequest, ExpectedError: libxrpc.ErrorInvalidRequest,
		},
		{
			// a field that doesn't belong to this scope
			ScopeKind: libdb.ScopeSpace, Method: http.MethodPost, Body: `{"space":"` + libtestfixtures.Space + `","did":"` + libtestfixtures.Alice + `"}`, Token: libtestfixtures.AdminToken,
			ExpectedCode: http.StatusBadRequest, ExpectedError: libxrpc.ErrorInvalidRequest,
		},
		{
			ScopeKind: libdb.ScopeAccount, Method: http.MethodPost, Body: `{"did":"not-a-did"}`, Token: libtestfixtures.AdminToken,
			ExpectedCode: http.StatusBadRequest, ExpectedError: libxrpc.ErrorInvalidRequest,
		},
		{
			ScopeKind: libdb.ScopeRecord, Method: http.MethodPost, Body: `{"space":"` + libtestfixtures.Space + `","repo":"` + libtestfixtures.Alice + `","collection":"com.atmoboards.thread"}`, Token: libtestfixtures.AdminToken,
			ExpectedCode: http.StatusBadRequest, ExpectedError: libxrpc.ErrorInvalidRequest,
		},
	}

	for testNumber, test := range tests {

		recorder := serve(forum, nil, test.ScopeKind, true, test.Method, test.Body, test.Token)

		var body struct{
			Error string `json:"error"`
		}
		json.Unmarshal(recorder.Body.Bytes(), &body)

		if test.ExpectedCode != recorder.Code || test.ExpectedError != body.Error {
			t.Errorf("For test #%d, the actual response is not what was expected.", testNumber)
			t.Logf("EXPECTED: %d %s", test.ExpectedCode, test.ExpectedError)
			t.Logf("ACTUAL:   %d %s", recorder.Code, body.Error)
			continue
		}
	}
}
