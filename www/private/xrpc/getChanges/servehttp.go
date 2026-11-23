package verboten

import (
	"database/sql"
	"net/http"
	"strconv"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"

	"protocosm-space-mirror/lib/changelog"
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
const Path string = "/xrpc/" + libnsid.NSIDPrefix + ".getChanges"

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

// Handler serves getChanges: change-log entries after a position, in order.
//
// Query parameters: since (optional; a seq; default 0 — the beginning), limit (optional; 1..1000, default 100).
// Response: {"cursor": "<seq>", "changes": [{"seq": …, "at": …, "kind": …, …}, …]}.
//
// "cursor" is the position to pass as "since" next time (the last seq returned — or "since" itself, if there was nothing new).
// Entries hold references only; fetch values with getRecord / listRecords / listSpaceRecords.
//
// If "since" is older than what the change log still keeps, the error is CursorTooOld —
// and the consumer must reload (see listSpaceRecords and getStatus).
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

	since, valid := libxrpc.ParseSeq(query.Get("since"))
	if !valid {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid 'since' parameter")
		return
	}

	limit, valid := libxrpc.Limit(query, defaultLimit, maxLimit)
	if !valid {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid 'limit' parameter")
		return
	}

	{
		floorSeq, _, err := libdb.ChangelogBounds(log, receiver.DB)
		if nil != err {
			libxrpc.WriteInternalServerError(responseWriter)
			return
		}
		if since < floorSeq {
			libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorCursorTooOld, "'since' is older than the change log keeps — reload")
			return
		}
	}

	changes, err := libdb.ChangesList(log, receiver.DB, since, limit)
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	var items []libchangelog.ChangeJSON = make([]libchangelog.ChangeJSON, 0, len(changes))
	var cursor int64 = since
	for _, change := range changes {
		items = append(items, libchangelog.ToJSON(change))
		cursor = change.Seq
	}

	body, err := libxrpc.MarshalJSON(struct{
		Cursor  string                    `json:"cursor"`
		Changes []libchangelog.ChangeJSON `json:"changes"`
	}{
		Cursor:  strconv.FormatInt(cursor, 10),
		Changes: items,
	})
	if nil != err {
		log.Error(field.S("failed to render response"), field.E(err))
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	libxrpc.WriteJSON(responseWriter, http.StatusOK, body)
}
