package libdb

import (
	"database/sql"
	"strings"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
	"github.com/reiver/go-opt"
)

// Record is a record, as stored: its path, its CID, and its DRISL bytes.
type Record struct {
	Collection string
	RKey       string
	CID        string
	DRISL      []byte
}

// Freshness says how up-to-date a space (or a repo in a space) is.
type Freshness struct {
	// VerifiedAt is the last time (unix seconds) the data was confirmed to match upstream. Nothing if never.
	VerifiedAt opt.Optional[int64]

	// StaleReasons lists why the data may be out-of-date. Empty when it is not known to be stale.
	StaleReasons []string
}

// SpaceVisible returns whether the space is visible.
func SpaceVisible(logger log.Logger, db *sql.DB, spaceURI string) (bool, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return false, erorr.Wrap(ErrNilDB, "failed to check space visibility")
	}

	const query string =
		`SELECT COUNT(*) FROM spaces AS s` +" "+
		`WHERE s.space_uri = ? AND ` + sqlSpaceVisible

	var count int
	err := db.QueryRow(query, spaceURI).Scan(&count)
	if nil != err {
		const msg string = "failed to check space visibility"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI))
		return false, erorr.Wrap(err, msg, field.String("space-uri", spaceURI))
	}

	return 0 < count, nil
}

// RepoVisible returns whether the repo (of 'did', in the space) is visible — including whether its space is.
func RepoVisible(logger log.Logger, db *sql.DB, spaceURI string, did string) (bool, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return false, erorr.Wrap(ErrNilDB, "failed to check repo visibility")
	}

	const query string =
		`SELECT COUNT(*) FROM repos AS rp` +" "+
		`JOIN spaces AS s ON s.space_uri = rp.space_uri` +" "+
		`WHERE rp.space_uri = ? AND rp.did = ?` +" "+
		`AND ` + sqlSpaceVisible +" "+
		`AND ` + sqlRepoVisible

	var count int
	err := db.QueryRow(query, spaceURI, did).Scan(&count)
	if nil != err {
		const msg string = "failed to check repo visibility"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI), field.String("did", did))
		return false, erorr.Wrap(err, msg, field.String("space-uri", spaceURI), field.String("did", did))
	}

	return 0 < count, nil
}

// RecordSelect returns a visible record. 'found' is false if there is no such record, or it is hidden
// (to callers, hidden is the same as absent).
//
// It does not check the visibility of the repo or space — call [RepoVisible] for that.
func RecordSelect(logger log.Logger, db *sql.DB, spaceURI string, did string, collection string, rkey string) (record Record, found bool, err error) {
	log := logger.Begin()
	defer log.End()

	var nada Record

	if nil == db {
		return nada, false, erorr.Wrap(ErrNilDB, "failed to select record")
	}

	const query string =
		`SELECT r.collection, r.rkey, r.cid, r.drisl FROM records AS r` +" "+
		`JOIN repos AS rp ON rp.space_uri = r.space_uri AND rp.did = r.did` +" "+
		`WHERE r.space_uri = ? AND r.did = ? AND r.collection = ? AND r.rkey = ?` +" "+
		`AND ` + sqlRecordVisible

	err = db.QueryRow(query, spaceURI, did, collection, rkey).Scan(&record.Collection, &record.RKey, &record.CID, &record.DRISL)
	if erorr.Is(err, sql.ErrNoRows) {
		return nada, false, nil
	}
	if nil != err {
		const msg string = "failed to select record"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI), field.String("did", did), field.String("collection", collection), field.String("rkey", rkey))
		return nada, false, erorr.Wrap(err, msg, field.String("space-uri", spaceURI), field.String("did", did), field.String("collection", collection), field.String("rkey", rkey))
	}

	return record, true, nil
}

// RecordsList returns up to 'limit' visible records of a repo, ordered by (collection, rkey),
// starting after the record (afterCollection, afterRKey) — pass "", "" to start at the beginning.
// If 'collection' is something, only records in that collection are returned.
//
// It does not check the visibility of the repo or space — call [RepoVisible] for that.
func RecordsList(logger log.Logger, db *sql.DB, spaceURI string, did string, collection opt.Optional[string], afterCollection string, afterRKey string, limit int) ([]Record, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return nil, erorr.Wrap(ErrNilDB, "failed to list records")
	}

	var query string =
		`SELECT r.collection, r.rkey, r.cid, r.drisl FROM records AS r` +" "+
		`JOIN repos AS rp ON rp.space_uri = r.space_uri AND rp.did = r.did` +" "+
		`WHERE r.space_uri = ? AND r.did = ?` +" "+
		`AND ` + sqlRecordVisible +" "+
		`AND (r.collection > ? OR (r.collection = ? AND r.rkey > ?))`
	var args []any = []any{spaceURI, did, afterCollection, afterCollection, afterRKey}

	if c, found := collection.Get(); found {
		query += ` AND r.collection = ?`
		args = append(args, c)
	}

	query += ` ORDER BY r.collection ASC, r.rkey ASC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if nil != err {
		const msg string = "failed to list records"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI), field.String("did", did))
		return nil, erorr.Wrap(err, msg, field.String("space-uri", spaceURI), field.String("did", did))
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var record Record

		err := rows.Scan(&record.Collection, &record.RKey, &record.CID, &record.DRISL)
		if nil != err {
			const msg string = "failed to scan record row"
			log.Error(field.S(msg), field.E(err))
			return nil, erorr.Wrap(err, msg)
		}

		records = append(records, record)
	}
	if err := rows.Err(); nil != err {
		const msg string = "failed after the record rows loop"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}

	return records, nil
}

// Blob is a stored blob.
type Blob struct {
	CID   string
	Size  int64
	Mime  string
	State string
	Path  string
}

// BlobSelectForRepo returns the blob 'cid', if a visible record of the repo (of 'did', in the space) refers to it.
// 'found' is false otherwise — even if the blob is stored for some other record, repo, or space.
//
// It does not check the visibility of the repo or space — call [RepoVisible] for that.
func BlobSelectForRepo(logger log.Logger, db *sql.DB, spaceURI string, did string, cid string) (blob Blob, found bool, err error) {
	log := logger.Begin()
	defer log.End()

	var nada Blob

	if nil == db {
		return nada, false, erorr.Wrap(ErrNilDB, "failed to select blob")
	}

	const query string =
		`SELECT b.cid, b.size, b.mime, b.state, b.path FROM blobs AS b` +" "+
		`WHERE b.cid = ? AND EXISTS (`+
			`SELECT 1 FROM blob_refs AS br` +" "+
			`JOIN records AS r ON r.space_uri = br.space_uri AND r.did = br.did AND r.generation = br.generation AND r.collection = br.collection AND r.rkey = br.rkey` +" "+
			`JOIN repos AS rp ON rp.space_uri = r.space_uri AND rp.did = r.did` +" "+
			`WHERE br.cid = b.cid AND br.space_uri = ? AND br.did = ?` +" "+
			`AND ` + sqlRecordVisible +
		`)`

	err = db.QueryRow(query, cid, spaceURI, did).Scan(&blob.CID, &blob.Size, &blob.Mime, &blob.State, &blob.Path)
	if erorr.Is(err, sql.ErrNoRows) {
		return nada, false, nil
	}
	if nil != err {
		const msg string = "failed to select blob"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI), field.String("did", did), field.String("cid", cid))
		return nada, false, erorr.Wrap(err, msg, field.String("space-uri", spaceURI), field.String("did", did), field.String("cid", cid))
	}

	return blob, true, nil
}

// BlobsList returns up to 'limit' CIDs of the blobs that visible records of the repo (of 'did', in the space) refer to,
// in order, starting after 'afterCID' — pass "" to start at the beginning.
//
// It does not check the visibility of the repo or space — call [RepoVisible] for that.
func BlobsList(logger log.Logger, db *sql.DB, spaceURI string, did string, afterCID string, limit int) ([]string, error) {
	log := logger.Begin()
	defer log.End()

	if nil == db {
		return nil, erorr.Wrap(ErrNilDB, "failed to list blobs")
	}

	const query string =
		`SELECT DISTINCT br.cid FROM blob_refs AS br` +" "+
		`JOIN records AS r ON r.space_uri = br.space_uri AND r.did = br.did AND r.generation = br.generation AND r.collection = br.collection AND r.rkey = br.rkey` +" "+
		`JOIN repos AS rp ON rp.space_uri = r.space_uri AND rp.did = r.did` +" "+
		`WHERE br.space_uri = ? AND br.did = ? AND br.cid > ?` +" "+
		`AND ` + sqlRecordVisible +" "+
		`ORDER BY br.cid ASC LIMIT ?`

	rows, err := db.Query(query, spaceURI, did, afterCID, limit)
	if nil != err {
		const msg string = "failed to list blobs"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI), field.String("did", did))
		return nil, erorr.Wrap(err, msg, field.String("space-uri", spaceURI), field.String("did", did))
	}
	defer rows.Close()

	var cids []string
	for rows.Next() {
		var cid string
		if err := rows.Scan(&cid); nil != err {
			const msg string = "failed to scan blob row"
			log.Error(field.S(msg), field.E(err))
			return nil, erorr.Wrap(err, msg)
		}
		cids = append(cids, cid)
	}
	if err := rows.Err(); nil != err {
		const msg string = "failed after the blob rows loop"
		log.Error(field.S(msg), field.E(err))
		return nil, erorr.Wrap(err, msg)
	}

	return cids, nil
}

// RepoFreshness returns how up-to-date a repo in a space is — combining the space's and the repo's freshness:
// the earlier verified-at time, and all the stale reasons of both.
func RepoFreshness(logger log.Logger, db *sql.DB, spaceURI string, did string) (Freshness, error) {
	log := logger.Begin()
	defer log.End()

	var nada Freshness

	if nil == db {
		return nada, erorr.Wrap(ErrNilDB, "failed to get freshness")
	}

	const query string =
		`SELECT s.verified_at, s.stale_reasons, rp.verified_at, rp.stale_reasons FROM repos AS rp` +" "+
		`JOIN spaces AS s ON s.space_uri = rp.space_uri` +" "+
		`WHERE rp.space_uri = ? AND rp.did = ?`

	var spaceVerifiedAt sql.NullInt64
	var spaceStaleReasons string
	var repoVerifiedAt sql.NullInt64
	var repoStaleReasons string

	err := db.QueryRow(query, spaceURI, did).Scan(&spaceVerifiedAt, &spaceStaleReasons, &repoVerifiedAt, &repoStaleReasons)
	if erorr.Is(err, sql.ErrNoRows) {
		return nada, nil
	}
	if nil != err {
		const msg string = "failed to get freshness"
		log.Error(field.S(msg), field.E(err), field.String("space-uri", spaceURI), field.String("did", did))
		return nada, erorr.Wrap(err, msg, field.String("space-uri", spaceURI), field.String("did", did))
	}

	var freshness Freshness

	// The data is only as fresh as the older of the two — and "never verified" wins.
	if spaceVerifiedAt.Valid && repoVerifiedAt.Valid {
		var verifiedAt int64 = spaceVerifiedAt.Int64
		if repoVerifiedAt.Int64 < verifiedAt {
			verifiedAt = repoVerifiedAt.Int64
		}
		freshness.VerifiedAt = opt.Something(verifiedAt)
	}

	freshness.StaleReasons = unionReasons(spaceStaleReasons, repoStaleReasons)

	return freshness, nil
}

// unionReasons combines space-separated lists of reasons, keeping the first occurrence of each.
func unionReasons(lists ...string) []string {
	var result []string
	var seen map[string]struct{} = map[string]struct{}{}

	for _, list := range lists {
		for _, reason := range strings.Fields(list) {
			if _, found := seen[reason]; found {
				continue
			}
			seen[reason] = struct{}{}
			result = append(result, reason)
		}
	}

	return result
}
