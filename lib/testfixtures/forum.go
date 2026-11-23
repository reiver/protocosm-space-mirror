// Package libtestfixtures builds a small, realistic space-mirror database for tests.
//
// It is only meant to be used from tests.
package libtestfixtures

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/reiver/go-log"
	"github.com/reiver/go-drisl"

	_ "github.com/mattn/go-sqlite3"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/tkn"
)

// The fixture's identifiers.
const (
	Space     = "at://did:example:forum/space/com.atmoboards.forum/general"
	Authority = "did:example:forum"
	Alice     = "did:example:alice"
	Bob       = "did:example:bob"

	// ReadToken has the "read" permission; AdminToken has "read" and "admin"; NoReadToken has only "admin".
	ReadToken   = "test-read-token"
	AdminToken  = "test-read-admin-token"
	NoReadToken = "test-admin-only-token"
)

// BlobBytes are the bytes of the one blob (an "image") in the fixture.
var BlobBytes = []byte("\x89PNG not really a png")

// Forum is the fixture.
type Forum struct {
	Logger  log.Logger
	DB      *sql.DB
	BlobDir string
	Tokens  []libtkn.Token

	// BlobCID is the CID (string form) of the blob that alice's first thread refers to.
	BlobCID string
}

// NewForum creates the fixture:
//
//	• an active space, verified at 1795435200 (2026-11-23T12:00:00Z),
//	• alice (generation 1): thread 3aaa (with an image blob), thread 3bbb, reply 3ccc,
//	• bob (generation 0): thread 3ddd,
//	• the blob's bytes in a file in BlobDir,
//	• tokens: ReadToken ("read"), AdminToken ("read" and "admin"), and NoReadToken ("admin" only).
func NewForum(t *testing.T) Forum {
	t.Helper()

	var forum Forum
	forum.Logger = log.CreateLogger(io.Discard, "v")
	forum.BlobDir = t.TempDir()

	{
		db, err := sql.Open("sqlite3", libdb.DSN(filepath.Join(t.TempDir(), "test.sqlite3")))
		if nil != err {
			t.Fatalf("Did not expect an error opening the database but actually got one: (%T) %s", err, err)
		}
		t.Cleanup(func() {
			db.Close()
		})
		forum.DB = db
	}

	if err := libdb.CreateDatabase(forum.Logger, forum.DB); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	var blobCID drisl.CID
	{
		digest := sha256.Sum256(BlobBytes)
		blobCID = drisl.CID(append([]byte{0x01, 0x55, 0x12, 0x20}, digest[:]...))
		forum.BlobCID = blobCID.String()

		if err := os.WriteFile(filepath.Join(forum.BlobDir, forum.BlobCID), BlobBytes, 0o600); nil != err {
			t.Fatalf("Did not expect an error writing the blob file but actually got one: (%T) %s", err, err)
		}
	}

	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := forum.DB.Exec(statement, args...); nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s\nSTATEMENT: %s", err, err, statement)
		}
	}

	record := func(did string, generation int, collection string, rkey string, value map[string]any) {
		t.Helper()

		p, err := drisl.Marshal(value)
		if nil != err {
			t.Fatalf("Did not expect an error marshaling a record but actually got one: (%T) %s", err, err)
		}

		digest := sha256.Sum256(p)
		var cid string = drisl.CID(append([]byte{0x01, 0x71, 0x12, 0x20}, digest[:]...)).String()

		exec(`INSERT INTO records (space_uri, did, generation, collection, rkey, cid, drisl) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			Space, did, generation, collection, rkey, cid, p)
	}

	exec(`INSERT INTO spaces (space_uri, authority, type, skey, lifecycle, had_success, epoch, notify_token, verified_at) VALUES (?, ?, 'com.atmoboards.forum', 'general', 'active', 1, 1, 'notify-token', 1795435200)`,
		Space, Authority)
	exec(`INSERT INTO repos (space_uri, did, epoch, generation, verified_at) VALUES (?, ?, 2, 1, 1795435200)`, Space, Alice)
	exec(`INSERT INTO repos (space_uri, did, epoch, generation, verified_at) VALUES (?, ?, 3, 0, 1795435200)`, Space, Bob)

	record(Alice, 1, "com.atmoboards.thread", "3aaa", map[string]any{
		"$type": "com.atmoboards.thread",
		"title": "Hello <everyone> & welcome",
		"image": map[string]any{
			"$type":    "blob",
			"ref":      blobCID,
			"mimeType": "image/png",
			"size":     int64(len(BlobBytes)),
		},
		"createdAt": "2026-11-23T12:00:00.000Z",
	})
	record(Alice, 1, "com.atmoboards.thread", "3bbb", map[string]any{
		"$type": "com.atmoboards.thread",
		"title": "Second thread",
		"createdAt": "2026-11-23T12:01:00.000Z",
	})
	record(Alice, 1, "com.atmoboards.reply", "3ccc", map[string]any{
		"$type": "com.atmoboards.reply",
		"text":  "A reply",
		"createdAt": "2026-11-23T12:02:00.000Z",
	})
	record(Bob, 0, "com.atmoboards.thread", "3ddd", map[string]any{
		"$type": "com.atmoboards.thread",
		"title": "Bob's thread",
		"createdAt": "2026-11-23T12:03:00.000Z",
	})

	exec(`INSERT INTO blobs (cid, size, mime, state, path) VALUES (?, ?, 'image/png', 'fetched', ?)`, forum.BlobCID, len(BlobBytes), forum.BlobCID)
	exec(`INSERT INTO blob_refs (space_uri, did, generation, collection, rkey, cid) VALUES (?, ?, 1, 'com.atmoboards.thread', '3aaa', ?)`, Space, Alice, forum.BlobCID)

	{
		readDigest := sha256.Sum256([]byte(ReadToken))
		readAdminDigest := sha256.Sum256([]byte(AdminToken))
		adminDigest := sha256.Sum256([]byte(NoReadToken))

		tokens, err := libtkn.ParseTokens(
			"test-reader read " + hex.EncodeToString(readDigest[:]) + "\n" +
			"test-operator read,admin " + hex.EncodeToString(readAdminDigest[:]) + "\n" +
			"test-admin admin " + hex.EncodeToString(adminDigest[:]) + "\n",
		)
		if nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
		forum.Tokens = tokens
	}

	return forum
}

// Exec runs a SQL statement on the fixture's database (ex: to add a hide).
func (receiver Forum) Exec(t *testing.T, statement string, args ...any) {
	t.Helper()

	if _, err := receiver.DB.Exec(statement, args...); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s\nSTATEMENT: %s", err, err, statement)
	}
}
