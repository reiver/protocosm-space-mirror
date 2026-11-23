package main

import (
	"os"
	"os/signal"
	"syscall"

	"codeberg.org/reiver/go-field"

	"protocosm-space-mirror/srv/log"
	"protocosm-space-mirror/srv/spaces"
)

// reloadSpacesOnSIGHUP re-reads the spaces file each time the process gets a SIGHUP.
//
// A reload that fails (missing or invalid file, or too many spaces unlisted) keeps the previous list.
// (To force a reload that unlists many spaces, use the reloadSpaces end-point with force.)
func reloadSpacesOnSIGHUP() {
	var hangup = make(chan os.Signal, 1)
	signal.Notify(hangup, syscall.SIGHUP)

	go func() {
		for range hangup {
			log := logsrv.Begin()

			log.Inform(field.S("SIGHUP: reloading the spaces file"))

			diff, err := spacessrv.Reload(false)
			if nil != err {
				log.Alert(field.S("spaces file reload rejected — keeping the previous list"), field.E(err))
			} else {
				log.Inform(
					field.S("spaces file reloaded"),
					field.Int("added", len(diff.Added)),
					field.Int("unlisted", len(diff.Removed)),
					field.Int("relisted", len(diff.Relisted)),
				)
			}

			log.End()
		}
	}()
}
