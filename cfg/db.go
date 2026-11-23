package cfg

import (
	"codeberg.org/reiver/go-env"
)

func DBFileName() string {
	return env.GetElse[string]("DB_FILE_NAME", "./.space-mirror.sqlite3")
}
