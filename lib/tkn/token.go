package libtkn

// The permissions a token can have.
const (
	PermissionRead  string = "read"
	PermissionAdmin string = "admin"
)

// Token is one of our own services' tokens, for the internal listener.
//
// Only the SHA-256 digest of the token is kept — never the token itself.
type Token struct {
	// Name is the name of the service the token belongs to (ex: "appview"). It is what gets logged.
	Name string

	// Permissions are the permissions the token has ("read", "admin").
	Permissions []string

	// Digest is the SHA-256 digest of the token.
	Digest [32]byte
}

// Has returns whether the token has the permission.
func (receiver Token) Has(permission string) bool {
	for _, p := range receiver.Permissions {
		if permission == p {
			return true
		}
	}
	return false
}
