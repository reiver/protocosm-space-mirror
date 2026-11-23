package verboten

import (
	"database/sql"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"

	"protocosm-space-mirror/cfg"
	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/nsid"
	"protocosm-space-mirror/lib/tkn"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/srv/db"
	"protocosm-space-mirror/srv/internalhttp"
	"protocosm-space-mirror/srv/log"
	"protocosm-space-mirror/srv/tkn"
)

// Path is the HTTP path of this XRPC query.
const Path string = "/xrpc/" + libnsid.NSIDPrefixRepo + ".getBlob"

func init() {
	var handler http.Handler = Handler{
		Logger:  logsrv.Logger(),
		DB:      dbsrv.ReadDB(),
		Tokens:  tknsrv.Tokens(),
		BlobDir: cfg.BlobDir(),
	}

	err := internalhttpsrv.Mux.HandlePath(handler, Path)
	if nil != err {
		err = erorr.Wrap(err, "problem registering HTTP-handler for path",
			field.String("path", Path),
		)
		panic(err)
	}
}

// Handler serves getBlob: a blob that one of a repo's (visible) records in a space refers to.
//
// Query parameters: space, repo, cid.
// Response: the blob's bytes, with its (declared) MIME type.
//
// The MIME type is whatever the record's author declared, so the response also carries
// "X-Content-Type-Options: nosniff" and "Content-Security-Policy: default-src 'none'".
type Handler struct {
	Logger  log.Logger
	DB      *sql.DB
	Tokens  []libtkn.Token
	BlobDir string
}

func (receiver Handler) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	log := receiver.Logger.Begin(field.String("www.path", Path))
	defer log.End()

	if !libxrpc.RequireGET(responseWriter, request) {
		return
	}

	token, authorized := libxrpc.Authorize(responseWriter, request, receiver.Tokens, libtkn.PermissionRead)
	if !authorized {
		return
	}
	log.Trace(field.String("service", token.Name))

	var query = request.URL.Query()

	var spaceURI string = query.Get("space")
	var did      string = query.Get("repo")
	var cid      string = query.Get("cid")

	switch {
	case !libxrpc.ValidSpaceRef(spaceURI):
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'space' parameter")
		return
	case !libxrpc.ValidDID(did):
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'repo' parameter")
		return
	case "" == cid:
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "missing 'cid' parameter")
		return
	}

	{
		visible, err := libdb.SpaceVisible(log, receiver.DB, spaceURI)
		if nil != err {
			libxrpc.WriteInternalServerError(responseWriter)
			return
		}
		if !visible {
			libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorSpaceNotFound, "space not found")
			return
		}
	}

	{
		visible, err := libdb.RepoVisible(log, receiver.DB, spaceURI, did)
		if nil != err {
			libxrpc.WriteInternalServerError(responseWriter)
			return
		}
		if !visible {
			libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorRepoNotFound, "repo not found")
			return
		}
	}

	blob, found, err := libdb.BlobSelectForRepo(log, receiver.DB, spaceURI, did, cid)
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}
	if !found {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorBlobNotFound, "blob not found")
		return
	}
	if "fetched" != blob.State {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorBlobNotAvailable, "blob not available")
		return
	}

	// The path comes from our own database, but check it stays inside the blob directory anyway.
	var filename string = filepath.Join(receiver.BlobDir, filepath.Clean("/" + blob.Path))
	if !strings.HasPrefix(filename, filepath.Clean(receiver.BlobDir) + string(filepath.Separator)) {
		log.Alert(field.S("blob path escapes the blob directory"), field.String("cid", cid), field.String("path", blob.Path))
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	file, err := os.Open(filename)
	if nil != err {
		log.Error(field.S("failed to open blob file"), field.E(err), field.String("cid", cid), field.String("file-name", filename))
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorBlobNotAvailable, "blob not available")
		return
	}
	defer file.Close()

	{
		freshness, err := libdb.RepoFreshness(log, receiver.DB, spaceURI, did)
		if nil != err {
			libxrpc.WriteInternalServerError(responseWriter)
			return
		}
		libxrpc.SetFreshnessHeaders(responseWriter, freshness)
	}

	var header http.Header = responseWriter.Header()
	{
		var mime string = blob.Mime
		if "" == mime {
			mime = "application/octet-stream"
		}
		header.Set("Content-Type", mime)
	}
	header.Set("Content-Length", strconv.FormatInt(blob.Size, 10))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'")

	responseWriter.WriteHeader(http.StatusOK)
	if http.MethodHead == request.Method {
		return
	}

	_, err = io.Copy(responseWriter, io.LimitReader(file, blob.Size))
	if nil != err {
		log.Error(field.S("failed to send blob"), field.E(err), field.String("cid", cid))
		return
	}
}
