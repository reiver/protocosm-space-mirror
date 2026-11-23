package verboten

import (
	"database/sql"
	"net/http"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"

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
const Path string = "/xrpc/" + libnsid.NSIDPrefixRepo + ".listBlobs"

const (
	defaultLimit = 500
	maxLimit     = 1000
)

func init() {
	var handler http.Handler = Handler{
		Logger: logsrv.Logger(),
		DB:     dbsrv.ReadDB(),
		Tokens: tknsrv.Tokens(),
	}

	err := internalhttpsrv.Mux.HandlePath(handler, Path)
	if nil != err {
		err = erorr.Wrap(err, "problem registering HTTP-handler for path",
			field.String("path", Path),
		)
		panic(err)
	}
}

// Handler serves listBlobs: the CIDs of the blobs that one repo's (visible) records in a space refer to.
//
// Query parameters: space, repo, limit (optional; 1..1000, default 500), cursor (optional).
// Response: {"cursor": … (when there may be more), "cids": […]}.
type Handler struct {
	Logger log.Logger
	DB     *sql.DB
	Tokens []libtkn.Token
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
	var cursor   string = query.Get("cursor")

	if !libxrpc.ValidSpaceRef(spaceURI) {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'space' parameter")
		return
	}
	if !libxrpc.ValidDID(did) {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'repo' parameter")
		return
	}

	limit, valid := libxrpc.Limit(query, defaultLimit, maxLimit)
	if !valid {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid 'limit' parameter")
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

	// The cursor is the last CID of the previous page.
	cids, err := libdb.BlobsList(log, receiver.DB, spaceURI, did, cursor, limit)
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}
	if nil == cids {
		cids = []string{}
	}

	var nextCursor string
	if limit == len(cids) {
		nextCursor = cids[len(cids)-1]
	}

	body, err := libxrpc.MarshalJSON(struct{
		Cursor string   `json:"cursor,omitempty"`
		CIDs   []string `json:"cids"`
	}{
		Cursor: nextCursor,
		CIDs:   cids,
	})
	if nil != err {
		log.Error(field.S("failed to render response"), field.E(err))
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	{
		freshness, err := libdb.RepoFreshness(log, receiver.DB, spaceURI, did)
		if nil != err {
			libxrpc.WriteInternalServerError(responseWriter)
			return
		}
		libxrpc.SetFreshnessHeaders(responseWriter, freshness)
	}

	libxrpc.WriteJSON(responseWriter, http.StatusOK, body)
}
