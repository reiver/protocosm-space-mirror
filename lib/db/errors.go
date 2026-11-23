package libdb

import (
	"codeberg.org/reiver/go-erorr"
)

const (
	ErrBadHideScope      = erorr.Error("libdb: bad hide scope")
	ErrNilDB             = erorr.Error("libdb: nil database")
	ErrSettingNotApplied = erorr.Error("libdb: database setting not applied")
)
