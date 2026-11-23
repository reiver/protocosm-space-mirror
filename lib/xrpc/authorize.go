package libxrpc

import (
	"net/http"

	"protocosm-space-mirror/lib/tkn"
)

// Authorize checks the request's "Authorization: Bearer <token>" header against 'tokens',
// and that the token has 'permission'.
//
// If not, it sends the error response (401 AuthRequired, or 403 Forbidden) and returns false.
//
// Example usage:
//
//	token, authorized := libxrpc.Authorize(responseWriter, request, tokens, libtkn.PermissionRead)
//	if !authorized {
//		return
//	}
func Authorize(responseWriter http.ResponseWriter, request *http.Request, tokens []libtkn.Token, permission string) (libtkn.Token, bool) {
	var nada libtkn.Token

	if nil == request {
		WriteInternalServerError(responseWriter)
		return nada, false
	}

	token, found := libtkn.Authenticate(tokens, request.Header.Get("Authorization"))
	if !found {
		WriteError(responseWriter, http.StatusUnauthorized, ErrorAuthRequired, "a valid bearer token is required")
		return nada, false
	}

	if !token.Has(permission) {
		WriteError(responseWriter, http.StatusForbidden, ErrorForbidden, "the token does not have the '"+permission+"' permission")
		return nada, false
	}

	return token, true
}

// RequireGET sends a 405 error response, and returns false, if the request is not a GET (or HEAD).
// (XRPC queries are GET requests.)
func RequireGET(responseWriter http.ResponseWriter, request *http.Request) bool {
	if nil == request {
		WriteInternalServerError(responseWriter)
		return false
	}

	switch request.Method {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		responseWriter.Header().Set("Allow", "GET, HEAD")
		WriteError(responseWriter, http.StatusMethodNotAllowed, ErrorMethodNotAllowed, "XRPC queries use GET")
		return false
	}
}
