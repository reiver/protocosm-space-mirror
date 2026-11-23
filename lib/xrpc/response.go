package libxrpc

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
)

// MarshalJSON is like json.Marshal, except it does not escape HTML characters (<, >, &),
// and does not add a trailing newline.
func MarshalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer

	var encoder *json.Encoder = json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)

	err := encoder.Encode(value)
	if nil != err {
		return nil, err
	}

	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

// WriteJSON sends 'body' (already JSON) as the HTTP response.
func WriteJSON(responseWriter http.ResponseWriter, statusCode int, body []byte) {
	if nil == responseWriter {
		return
	}

	var header http.Header = responseWriter.Header()
	header.Set("Content-Type", "application/json; charset=utf-8")
	header.Set("Content-Length", strconv.Itoa(len(body)))

	responseWriter.WriteHeader(statusCode)
	responseWriter.Write(body)
}

// WriteError sends an XRPC error response: {"error": <name>, "message": <message>}.
func WriteError(responseWriter http.ResponseWriter, statusCode int, name string, message string) {
	body, err := MarshalJSON(struct{
		Error   string `json:"error"`
		Message string `json:"message"`
	}{
		Error:   name,
		Message: message,
	})
	if nil != err {
		http.Error(responseWriter, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	WriteJSON(responseWriter, statusCode, body)
}

// The XRPC error names used by the internal end-points.
const (
	ErrorAuthRequired        = "AuthRequired"
	ErrorBlobNotAvailable    = "BlobNotAvailable"
	ErrorBlobNotFound        = "BlobNotFound"
	ErrorCursorTooFarBehind  = "CursorTooFarBehind"
	ErrorCursorTooOld        = "CursorTooOld"
	ErrorForbidden           = "Forbidden"
	ErrorInternalServerError = "InternalServerError"
	ErrorInvalidCursor       = "InvalidCursor"
	ErrorInvalidRequest      = "InvalidRequest"
	ErrorMethodNotAllowed    = "MethodNotAllowed"
	ErrorRecordNotFound      = "RecordNotFound"
	ErrorRepoNotFound        = "RepoNotFound"
	ErrorSpaceNotFound       = "SpaceNotFound"
)

// WriteInternalServerError sends a 500 XRPC error response.
func WriteInternalServerError(responseWriter http.ResponseWriter) {
	WriteError(responseWriter, http.StatusInternalServerError, ErrorInternalServerError, "internal server error")
}
