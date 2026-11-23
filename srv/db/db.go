package dbsrv

import (
	"database/sql"
	"flag"
	"os"
	"strings"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"

	_ "github.com/mattn/go-sqlite3"

	"protocosm-space-mirror/cfg"
	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/srv/log"
)

var (
	readDB  *sql.DB
	writeDB *sql.DB
)

func ReadDB() *sql.DB {
	return readDB
}

func WriteDB() *sql.DB {
	return writeDB
}

func init() {
	// Side-effecting init()s return early under "go test".
	if nil != flag.Lookup("test.v") || strings.HasSuffix(os.Args[0], ".test") {
		return
	}

	var filename string = cfg.DBFileName()
	var dsn string = libdb.DSN(filename)

	{
		var err error

		writeDB, err = sql.Open("sqlite3", dsn)
		if nil != err {
			err = erorr.Wrap(err, "failed to open SQLite write database connection",
				field.String("file-name", filename),
			)
			panic(err)
		}
		if nil == writeDB {
			var err error = libdb.ErrNilDB
			err = erorr.Wrap(err, "nil SQLite write database connection",
				field.String("file-name", filename),
			)
			panic(err)
		}

		// There is only one writer (one writer goroutine).
		writeDB.SetMaxOpenConns(1)

		err = writeDB.Ping()
		if nil != err {
			err = erorr.Wrap(err, "failed to ping SQLite write database connection",
				field.String("file-name", filename),
			)
			panic(err)
		}
	}

	{
		// Create database table(s) if they don't exist yet.
		//
		// This happens on the write connection, which already has auto_vacuum=INCREMENTAL set (from the DSN),
		// so that a new database file gets it before its first table is created.
		err := libdb.CreateDatabase(logsrv.Logger(), writeDB)
		if nil != err {
			err = erorr.Wrap(err, "failed to create database table(s) in SQLite database",
				field.String("file-name", filename),
			)
			panic(err)
		}
	}

	{
		var err error

		readDB, err = sql.Open("sqlite3", dsn)
		if nil != err {
			err = erorr.Wrap(err, "failed to open SQLite read database connection",
				field.String("file-name", filename),
			)
			panic(err)
		}
		if nil == readDB {
			var err error = libdb.ErrNilDB
			err = erorr.Wrap(err, "nil SQLite read database connection",
				field.String("file-name", filename),
			)
			panic(err)
		}

		err = readDB.Ping()
		if nil != err {
			err = erorr.Wrap(err, "failed to ping SQLite read database connection",
				field.String("file-name", filename),
			)
			panic(err)
		}
	}

	{
		// Fail fast if the settings the purge procedure depends on are not in effect.
		for _, db := range []*sql.DB{writeDB, readDB} {
			err := libdb.VerifySettings(logsrv.Logger(), db)
			if nil != err {
				err = erorr.Wrap(err, "SQLite database settings not in effect",
					field.String("file-name", filename),
				)
				panic(err)
			}
		}
	}
}

func Close() {
	log := logsrv.Begin()
	defer log.End()

	var filename string = cfg.DBFileName()

	if nil != readDB {
		err := readDB.Close()
		if nil != err {
			log.Error(
				field.S("failed to close SQLite read database connection"),
				field.E(err),
				field.String("file-name", filename),
			)
		}
	}

	if nil != writeDB {
		err := writeDB.Close()
		if nil != err {
			log.Error(
				field.S("failed to close SQLite write database connection"),
				field.E(err),
				field.String("file-name", filename),
			)
		}
	}
}
