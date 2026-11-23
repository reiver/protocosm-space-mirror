package libdb

// DSN returns the github.com/mattn/go-sqlite3 data-source-name for the SQLite database file 'filename'.
//
// SQLite pragmas are per connection, so they are put in the DSN, which the driver applies to every connection it opens
// (in both the read and the write pool).
//
// The settings are:
//
//	• auto_vacuum=INCREMENTAL — so that 'PRAGMA incremental_vacuum' can return the pages a purge freed. (It only takes effect if set before the first table is created.)
//	• secure_delete=ON        — so that deleted content is overwritten, rather than left readable in free pages.
//	• journal_mode=WAL        — so that readers don't block the (single) writer.
//	• synchronous=NORMAL      — the usual pairing with WAL.
//	• busy_timeout=5000       — wait up to 5 seconds for a lock rather than failing at once.
//
// See also:
//
//	• [VerifySettings]
//
// Example usage:
//
//	db, err := sql.Open("sqlite3", libdb.DSN("./.space-mirror.sqlite3"))
func DSN(filename string) string {
	const params string =
		"_auto_vacuum=incremental" +"&"+
		"_secure_delete=on"        +"&"+
		"_journal_mode=WAL"        +"&"+
		"_synchronous=NORMAL"      +"&"+
		"_busy_timeout=5000"

	return "file:" + filename + "?" + params
}
