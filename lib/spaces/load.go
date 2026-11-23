package libspaces

import (
	"database/sql"
	"strings"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"

	"protocosm-space-mirror/lib/db"
)

// Load applies the contents of a spaces file to the database:
// it parses it, compares it with the known spaces, checks the unlist limits, and applies the difference (in one transaction).
//
// Nothing is changed if the file is invalid or the unlist limits are exceeded (and 'force' is false).
//
// It returns the difference applied, and whether anything was appended to the change log.
func Load(logger log.Logger, db *sql.DB, text string, unlistMax int, force bool, now int64, purgeGraceSeconds int64) (Diff, bool, error) {
	log := logger.Begin()
	defer log.End()

	var nada Diff

	fileSpaces, err := Parse(text)
	if nil != err {
		log.Error(field.S("invalid spaces file"), field.E(err))
		return nada, false, err
	}

	known, err := libdb.KnownSpaces(log, db)
	if nil != err {
		return nada, false, err
	}

	var listedCount int
	for _, listed := range known {
		if listed {
			listedCount++
		}
	}

	var diff Diff = Compare(fileSpaces, known)

	log.Inform(
		field.S("spaces file"),
		field.Int("in-file", len(fileSpaces)),
		field.Int("listed", listedCount),
		field.String("adding", strings.Join(diff.Added, " ")),
		field.String("unlisting", strings.Join(diff.Removed, " ")),
		field.String("relisting", strings.Join(diff.Relisted, " ")),
		field.Bool("force", force),
	)

	if err := CheckUnlist(diff, listedCount, unlistMax, force); nil != err {
		log.Alert(field.S("refusing the spaces file"), field.E(err))
		return nada, false, err
	}

	if diff.IsEmpty() {
		return diff, false, nil
	}

	changelogged, err := libdb.ApplySpacesList(log, db, diff.Added, diff.Removed, diff.Relisted, now, now+purgeGraceSeconds)
	if nil != err {
		return nada, false, erorr.Wrap(err, "failed to apply the spaces file")
	}

	return diff, changelogged, nil
}
