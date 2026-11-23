package libdb

import (
	"codeberg.org/reiver/go-erorr"
)

const (
	ErrNilDB             = erorr.Error("libdb: nil database")
	ErrSettingNotApplied = erorr.Error("libdb: database setting not applied")
)
