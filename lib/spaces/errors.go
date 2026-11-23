package libspaces

import (
	"codeberg.org/reiver/go-erorr"
)

const (
	ErrBadLine          = erorr.Error("libspaces: bad line in the spaces file")
	ErrTooManyUnlisted  = erorr.Error("libspaces: the spaces file would unlist too many spaces")
)
