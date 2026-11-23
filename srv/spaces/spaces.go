package spacessrv

import (
	"flag"
	"os"
	"strings"
	"sync"
	"time"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"

	"protocosm-space-mirror/cfg"
	"protocosm-space-mirror/lib/spaces"
	"protocosm-space-mirror/srv/changelog"
	"protocosm-space-mirror/srv/db"
	"protocosm-space-mirror/srv/log"
)

var mutex sync.Mutex

func init() {
	// Side-effecting init()s return early under "go test".
	if nil != flag.Lookup("test.v") || strings.HasSuffix(os.Args[0], ".test") {
		return
	}

	// At start, a spaces file that is missing, invalid, or that would unlist too many spaces is fatal.
	_, err := load(cfg.ForceUnlist())
	if nil != err {
		err = erorr.Wrap(err, "failed to load the spaces file at start",
			field.String("file-name", cfg.SpacesFileName()),
		)
		panic(err)
	}
}

// Reload re-reads the spaces file and applies it.
//
// If the file is missing or invalid, or would unlist too many spaces (and 'force' is false), nothing changes
// — the previous list is kept — and the error is returned.
func Reload(force bool) (libspaces.Diff, error) {
	return load(force)
}

func load(force bool) (libspaces.Diff, error) {
	mutex.Lock()
	defer mutex.Unlock()

	log := logsrv.Begin()
	defer log.End()

	var nada libspaces.Diff

	var filename string = cfg.SpacesFileName()

	p, err := os.ReadFile(filename)
	if nil != err {
		log.Alert(field.S("failed to read the spaces file"), field.E(err), field.String("file-name", filename))
		return nada, erorr.Wrap(err, "failed to read the spaces file", field.String("file-name", filename))
	}

	diff, changelogged, err := libspaces.Load(log, dbsrv.WriteDB(), string(p), cfg.UnlistMax(), force, time.Now().Unix(), int64(cfg.PurgeGrace()/time.Second))
	if nil != err {
		return nada, err
	}

	if changelogged {
		changelogsrv.Notifier().Notify()
	}

	return diff, nil
}
