package cfg

import (
	"codeberg.org/reiver/go-env"
)

// InternalTokensFileName returns the path of the file listing our own services' tokens for the internal listener.
//
// Each line is: <service-name> <permissions> <sha256-hex-of-token>.
func InternalTokensFileName() string {
	return env.GetElse[string]("INTERNAL_TOKENS_FILE", "./.space-mirror.tokens")
}
