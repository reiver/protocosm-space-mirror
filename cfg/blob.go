package cfg

import (
	"codeberg.org/reiver/go-env"
)

// BlobDir returns the directory blobs are stored in.
func BlobDir() string {
	return env.GetElse[string]("BLOB_DIR", "./.space-mirror.blobs")
}
