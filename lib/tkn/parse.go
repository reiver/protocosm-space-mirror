package libtkn

import (
	"encoding/hex"
	"strings"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
)

// ParseTokens parses the contents of a tokens file.
//
// Each line is:
//
//	<service-name> <permissions> <sha256-hex-of-token>
//
// where <permissions> is a comma-separated list of "read" and "admin".
// Blank lines, and lines starting with '#', are ignored.
//
// For example:
//
//	# service   permissions  sha256 of the token
//	appview     read         9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
//	ops-console read,admin   60303ae22b998861bce3b28f33eec1be758a213c86c93c076dbe9f558c11c752
//
// A digest can be made with: printf '%s' "$TOKEN" | shasum -a 256
func ParseTokens(text string) ([]Token, error) {
	var tokens []Token

	for index, line := range strings.Split(text, "\n") {
		var lineNumber int = index + 1

		line = strings.TrimSpace(line)
		if "" == line || strings.HasPrefix(line, "#") {
			continue
		}

		var fields []string = strings.Fields(line)
		if 3 != len(fields) {
			return nil, erorr.Wrap(ErrBadTokensLine, "expected 3 fields", field.Int("line", lineNumber))
		}

		var token Token
		token.Name = fields[0]

		for _, permission := range strings.Split(fields[1], ",") {
			switch permission {
			case PermissionRead, PermissionAdmin:
				token.Permissions = append(token.Permissions, permission)
			default:
				return nil, erorr.Wrap(ErrBadTokensLine, "unknown permission", field.Int("line", lineNumber), field.String("permission", permission))
			}
		}

		digest, err := hex.DecodeString(fields[2])
		if nil != err || 32 != len(digest) {
			return nil, erorr.Wrap(ErrBadTokensLine, "the digest must be 64 hexadecimal characters (SHA-256)", field.Int("line", lineNumber))
		}
		copy(token.Digest[:], digest)

		tokens = append(tokens, token)
	}

	return tokens, nil
}
