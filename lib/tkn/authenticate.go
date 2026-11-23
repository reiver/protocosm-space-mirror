package libtkn

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"
)

// Authenticate finds the token that matches an HTTP "Authorization" header value of the form "Bearer <token>".
// 'found' is false if the header is missing, malformed, or matches no token.
//
// The comparison is constant-time, over SHA-256 digests.
//
// Example usage:
//
//	token, found := libtkn.Authenticate(tokens, request.Header.Get("Authorization"))
func Authenticate(tokens []Token, authorization string) (token Token, found bool) {
	var nada Token

	const prefix string = "Bearer "
	if len(authorization) <= len(prefix) || !strings.EqualFold(prefix, authorization[:len(prefix)]) {
		return nada, false
	}

	var digest [32]byte = sha256.Sum256([]byte(authorization[len(prefix):]))

	var matched int = -1
	for index, candidate := range tokens {
		// Check every token, so the time taken doesn't depend on which one matched.
		if 1 == subtle.ConstantTimeCompare(candidate.Digest[:], digest[:]) {
			matched = index
		}
	}
	if matched < 0 {
		return nada, false
	}

	return tokens[matched], true
}
