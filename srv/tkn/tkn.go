package tknsrv

import (
	"flag"
	"os"
	"strings"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"

	"protocosm-space-mirror/cfg"
	"protocosm-space-mirror/lib/tkn"
)

var tokens []libtkn.Token

// Tokens returns our own services' tokens for the internal listener.
func Tokens() []libtkn.Token {
	return tokens
}

func init() {
	// Side-effecting init()s return early under "go test".
	if nil != flag.Lookup("test.v") || strings.HasSuffix(os.Args[0], ".test") {
		return
	}

	var filename string = cfg.InternalTokensFileName()

	p, err := os.ReadFile(filename)
	if nil != err {
		err = erorr.Wrap(err, "failed to read the internal tokens file",
			field.String("file-name", filename),
		)
		panic(err)
	}

	tokens, err = libtkn.ParseTokens(string(p))
	if nil != err {
		err = erorr.Wrap(err, "failed to parse the internal tokens file",
			field.String("file-name", filename),
		)
		panic(err)
	}

	if len(tokens) <= 0 {
		err = erorr.Wrap(libtkn.ErrBadTokensLine, "the internal tokens file has no tokens",
			field.String("file-name", filename),
		)
		panic(err)
	}
}
