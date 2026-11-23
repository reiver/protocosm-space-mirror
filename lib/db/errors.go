package libdb

import (
	"codeberg.org/reiver/go-erorr"
)

const (
	ErrBadHideScope      = erorr.Error("libdb: bad hide scope")
	ErrBadPurgeScope     = erorr.Error("libdb: bad purge scope")
	ErrBadSpaceRef       = erorr.Error("libdb: bad space reference")
	ErrFenced            = erorr.Error("libdb: fenced — a purge started (or is going on) since the work began")
	ErrNilDB             = erorr.Error("libdb: nil database")
	ErrSettingNotApplied = erorr.Error("libdb: database setting not applied")
)
