package main

import (
	"protocosm-space-mirror/srv/db"
	"protocosm-space-mirror/srv/log"
)

func main() {
	shout()

	log := logsrv.Begin()
	defer log.End()

	log.Highlightf("protocosm-space-mirror 🪞")
	defer log.Highlightf("protocosm-space-mirror 👻")

	defer dbsrv.Close()

	showandtell()

	reloadSpacesOnSIGHUP()
	purgeOnSchedule()

	log.Informf("Here we go…")
	webservers()
}
