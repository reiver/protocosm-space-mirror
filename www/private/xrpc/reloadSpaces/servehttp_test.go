package verboten_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codeberg.org/reiver/go-erorr"

	"protocosm-space-mirror/lib/spaces"
	"protocosm-space-mirror/lib/testfixtures"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/www/private/xrpc/reloadSpaces"
)

func TestServeHTTP(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	tests := []struct{
		Token         string
		Body          string
		ReloadDiff    libspaces.Diff
		ReloadErr     error
		ExpectedForce bool
		ExpectedCalled bool
		ExpectedCode  int
		ExpectedError string
		ExpectedBody  string
	}{
		{
			Token:          libtestfixtures.AdminToken,
			ReloadDiff:     libspaces.Diff{Added: []string{"a"}, Removed: []string{"b"}},
			ExpectedCalled: true,
			ExpectedCode:   http.StatusOK,
			ExpectedBody:   `{"added":["a"],"unlisted":["b"],"relisted":[]}`,
		},
		{
			Token:          libtestfixtures.AdminToken,
			Body:           `{"force":true}`,
			ExpectedForce:  true,
			ExpectedCalled: true,
			ExpectedCode:   http.StatusOK,
			ExpectedBody:   `{"added":[],"unlisted":[],"relisted":[]}`,
		},
		{
			Token:          libtestfixtures.AdminToken,
			ReloadErr:      erorr.Wrap(libspaces.ErrTooManyUnlisted, "refusing"),
			ExpectedCalled: true,
			ExpectedCode:   http.StatusBadRequest,
			ExpectedError:  verboten.ErrorTooManyUnlisted,
		},
		{
			Token:          libtestfixtures.AdminToken,
			ReloadErr:      erorr.Wrap(libspaces.ErrBadLine, "bad"),
			ExpectedCalled: true,
			ExpectedCode:   http.StatusBadRequest,
			ExpectedError:  verboten.ErrorInvalidSpacesFile,
		},
		{
			Token:          libtestfixtures.AdminToken,
			ReloadErr:      errors.New("no such file"),
			ExpectedCalled: true,
			ExpectedCode:   http.StatusInternalServerError,
			ExpectedError:  verboten.ErrorSpacesFileUnreadable,
		},
		{
			// read is not enough
			Token:         libtestfixtures.ReadToken,
			ExpectedCode:  http.StatusForbidden,
			ExpectedError: libxrpc.ErrorForbidden,
		},
		{
			Token:         libtestfixtures.AdminToken,
			Body:          `{"force":"yes"}`,
			ExpectedCode:  http.StatusBadRequest,
			ExpectedError: libxrpc.ErrorInvalidRequest,
		},
	}

	for testNumber, test := range tests {

		var called bool
		var gotForce bool

		handler := verboten.Handler{
			Logger: forum.Logger,
			Tokens: forum.Tokens,
			Reload: func(force bool) (libspaces.Diff, error) {
				called = true
				gotForce = force
				return test.ReloadDiff, test.ReloadErr
			},
		}

		req := httptest.NewRequest(http.MethodPost, verboten.Path, strings.NewReader(test.Body))
		req.Header.Set("Authorization", "Bearer " + test.Token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if test.ExpectedCode != recorder.Code || test.ExpectedCalled != called || test.ExpectedForce != gotForce {
			t.Errorf("For test #%d, the actual result is not what was expected.", testNumber)
			t.Logf("EXPECTED: code=%d called=%t force=%t", test.ExpectedCode, test.ExpectedCalled, test.ExpectedForce)
			t.Logf("ACTUAL:   code=%d called=%t force=%t", recorder.Code, called, gotForce)
			t.Logf("BODY: %s", recorder.Body.String())
			continue
		}

		if "" != test.ExpectedBody && test.ExpectedBody != recorder.Body.String() {
			t.Errorf("For test #%d, the actual body is not what was expected.", testNumber)
			t.Logf("EXPECTED: %s", test.ExpectedBody)
			t.Logf("ACTUAL:   %s", recorder.Body.String())
			continue
		}

		if "" != test.ExpectedError {
			var body struct{
				Error string `json:"error"`
			}
			json.Unmarshal(recorder.Body.Bytes(), &body)
			if test.ExpectedError != body.Error {
				t.Errorf("For test #%d, the actual error is not what was expected: %q", testNumber, body.Error)
			}
		}
	}
}
