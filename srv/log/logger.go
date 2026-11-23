package logsrv

import (
	"io"
	"os"

	"codeberg.org/reiver/go-log"

	"protocosm-space-mirror/cfg"
)

var writer io.Writer = os.Stdout

var logger log.Logger = log.CreateLogger(writer, cfg.LogLevel())

func Logger() log.Logger {
	return logger
}
