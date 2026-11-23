package libxrpc

import (
	"encoding/json"
	"io"
	"net/http"
)

// MaxBodySize is the largest request body (in bytes) an XRPC procedure accepts.
const MaxBodySize = 64 * 1024

// RequirePOST sends a 405 error response, and returns false, if the request is not a POST.
// (XRPC procedures are POST requests.)
func RequirePOST(responseWriter http.ResponseWriter, request *http.Request) bool {
	if nil == request {
		WriteInternalServerError(responseWriter)
		return false
	}

	if http.MethodPost != request.Method {
		responseWriter.Header().Set("Allow", "POST")
		WriteError(responseWriter, http.StatusMethodNotAllowed, ErrorMethodNotAllowed, "XRPC procedures use POST")
		return false
	}
	return true
}

// ReadJSONBody decodes the request's JSON body (at most [MaxBodySize] bytes; unknown fields are an error) into 'dst'.
// If it can't, it sends a 400 InvalidRequest error response and returns false.
func ReadJSONBody(responseWriter http.ResponseWriter, request *http.Request, dst any) bool {
	var decoder *json.Decoder = json.NewDecoder(io.LimitReader(request.Body, MaxBodySize+1))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); nil != err {
		WriteError(responseWriter, http.StatusBadRequest, ErrorInvalidRequest, "invalid JSON body")
		return false
	}
	if decoder.More() {
		WriteError(responseWriter, http.StatusBadRequest, ErrorInvalidRequest, "invalid JSON body")
		return false
	}
	return true
}
