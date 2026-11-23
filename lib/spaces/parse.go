package libspaces

import (
	"strings"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"

	"protocosm-space-mirror/lib/xrpc"
)

// Parse parses the contents of a spaces file: one space reference (at://{authority}/space/{type}/{skey}) per line.
// Blank lines, and lines starting with '#', are ignored. A space listed more than once counts once.
//
// Any line that is not a valid space reference is an error — the whole file is rejected.
//
// Example usage:
//
//	spaces, err := libspaces.Parse(text)
func Parse(text string) ([]string, error) {
	var spaces []string
	var seen = map[string]struct{}{}

	for index, line := range strings.Split(text, "\n") {
		var lineNumber int = index + 1

		line = strings.TrimSpace(line)
		if "" == line || strings.HasPrefix(line, "#") {
			continue
		}

		if !libxrpc.ValidSpaceRef(line) {
			return nil, erorr.Wrap(ErrBadLine, "not a valid space reference", field.Int("line", lineNumber), field.String("text", line))
		}

		if _, found := seen[line]; found {
			continue
		}
		seen[line] = struct{}{}
		spaces = append(spaces, line)
	}

	return spaces, nil
}
