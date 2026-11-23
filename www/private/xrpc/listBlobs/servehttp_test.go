package verboten_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"protocosm-space-mirror/lib/testfixtures"
	"protocosm-space-mirror/www/private/xrpc/listBlobs"
)

func TestServeHTTP(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens}

	tests := []struct{
		Repo     string
		Expected []string
	}{
		{
			Repo:     libtestfixtures.Alice,
			Expected: []string{forum.BlobCID},
		},
		{
			Repo:     libtestfixtures.Bob,
			Expected: []string{},
		},
	}

	for testNumber, test := range tests {

		params := url.Values{"space": {libtestfixtures.Space}, "repo": {test.Repo}}
		req := httptest.NewRequest(http.MethodGet, verboten.Path + "?" + params.Encode(), nil)
		req.Header.Set("Authorization", "Bearer " + libtestfixtures.ReadToken)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if http.StatusOK != recorder.Code {
			t.Errorf("For test #%d, expected status 200 but actually got %d.\nBODY: %s", testNumber, recorder.Code, recorder.Body.String())
			continue
		}

		var body struct{
			CIDs []string `json:"cids"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); nil != err {
			t.Errorf("For test #%d, did not expect an error but actually got one: (%T) %s", testNumber, err, err)
			continue
		}

		if !reflect.DeepEqual(test.Expected, body.CIDs) {
			t.Errorf("For test #%d, the actual CIDs are not what was expected.", testNumber)
			t.Logf("EXPECTED: %v", test.Expected)
			t.Logf("ACTUAL:   %v", body.CIDs)
			t.Logf("BODY: %s", recorder.Body.String())
			continue
		}
	}
}
