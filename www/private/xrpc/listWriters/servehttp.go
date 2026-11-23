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
const Path string = "/xrpc/" + libnsid.NSIDPrefix + ".listWriters"

const (
	defaultLimit = 100
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

// Handler serves listWriters: the repos of a space, as the space-mirror sees them, ordered by DID.
//
// Query parameters: space, limit (optional; 1..1000, default 100), cursor (optional).
//
// A token with the "read" permission gets the visible repos: {"cursor": …, "repos": [{"did": …, "rev": …}, …]}.
// A token with the "admin" permission also gets the hidden ones, and why: […, {"did": …, "rev": …, "visible": false, "hideReasons": […]}].
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

	var isAdmin bool = token.Has(libtkn.PermissionAdmin)

	var query = request.URL.Query()

	var spaceURI string = query.Get("space")
	var cursor   string = query.Get("cursor")

	if !libxrpc.ValidSpaceRef(spaceURI) {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'space' parameter")
		return
	}
	if "" != cursor && !libxrpc.ValidDID(cursor) {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidCursor, "invalid 'cursor' parameter")
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

	writers, err := libdb.WritersList(log, receiver.DB, spaceURI, cursor, limit, isAdmin)
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	type internalRepo struct {
		DID         string   `json:"did"`
		Rev         string   `json:"rev"`
		Visible     *bool    `json:"visible,omitempty"`
		HideReasons []string `json:"hideReasons,omitempty"`
	}

	var repos []internalRepo = make([]internalRepo, 0, len(writers))
	for _, writer := range writers {
		var repo = internalRepo{
			DID: writer.DID,
			Rev: writer.Rev,
		}
		if isAdmin {
			var visible bool = writer.Visible
			repo.Visible = &visible
			repo.HideReasons = writer.HideReasons
		}
		repos = append(repos, repo)
	}

	var nextCursor string
	if limit == len(writers) {
		nextCursor = writers[len(writers)-1].DID
	}

	body, err := libxrpc.MarshalJSON(struct{
		Cursor string         `json:"cursor,omitempty"`
		Repos  []internalRepo `json:"repos"`
	}{
		Cursor: nextCursor,
		Repos:  repos,
	})
	if nil != err {
		log.Error(field.S("failed to render response"), field.E(err))
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	{
		freshness, err := libdb.SpaceFreshness(log, receiver.DB, spaceURI)
		if nil != err {
			libxrpc.WriteInternalServerError(responseWriter)
			return
		}
		libxrpc.SetFreshnessHeaders(responseWriter, freshness)
	}

	libxrpc.WriteJSON(responseWriter, http.StatusOK, body)
}
