package verboten

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
	"github.com/reiver/go-opt"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/nsid"
	"protocosm-space-mirror/lib/records"
	"protocosm-space-mirror/lib/tkn"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/srv/db"
	"protocosm-space-mirror/srv/internalhttp"
	"protocosm-space-mirror/srv/log"
	"protocosm-space-mirror/srv/tkn"
)

// Path is the HTTP path of this XRPC query.
const Path string = "/xrpc/" + libnsid.NSIDPrefixRepo + ".listRecords"

const (
	defaultLimit = 50
	maxLimit     = 100
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

// Handler serves listRecords: the records of one repo in a space, from the space-mirror's copy,
// ordered by collection then record key.
//
// Query parameters: space, repo, collection (optional), limit (optional; 1..100, default 50), cursor (optional), excludeValues (optional).
// Response: {"cursor": … (when there may be more), "records": [{"collection": …, "rkey": …, "cid": …, "value": … (unless excludeValues)}, …]}.
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

	if !libxrpc.ValidSpaceRef(spaceURI) {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'space' parameter")
		return
	}
	if !libxrpc.ValidDID(did) {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'repo' parameter")
		return
	}

	var collection opt.Optional[string]
	if c := query.Get("collection"); "" != c {
		if !libxrpc.ValidNSID(c) {
			libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid 'collection' parameter")
			return
		}
		collection = opt.Something(c)
	}

	limit, valid := libxrpc.Limit(query, defaultLimit, maxLimit)
	if !valid {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid 'limit' parameter")
		return
	}

	excludeValues, valid := libxrpc.Bool(query, "excludeValues")
	if !valid {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid 'excludeValues' parameter")
		return
	}

	// The cursor is "{collection}/{rkey}" of the last record of the previous page.
	var afterCollection string
	var afterRKey string
	if cursor := query.Get("cursor"); "" != cursor {
		c, r, found := strings.Cut(cursor, "/")
		if !found || !libxrpc.ValidNSID(c) || !libxrpc.ValidRecordKey(r) {
			libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidCursor, "invalid 'cursor' parameter")
			return
		}
		afterCollection = c
		afterRKey = r
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

	stored, err := libdb.RecordsList(log, receiver.DB, spaceURI, did, collection, afterCollection, afterRKey, limit)
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	type internalRecord struct {
		Collection string          `json:"collection"`
		RKey       string          `json:"rkey"`
		CID        string          `json:"cid"`
		Value      json.RawMessage `json:"value,omitempty"`
	}

	var items []internalRecord = make([]internalRecord, 0, len(stored))
	for _, record := range stored {
		var item = internalRecord{
			Collection: record.Collection,
			RKey:       record.RKey,
			CID:        record.CID,
		}

		if !excludeValues {
			value, err := librecords.ToJSON(record.DRISL)
			if nil != err {
				log.Error(
					field.S("failed to render stored record as JSON"),
					field.E(err),
					field.String("space-uri", spaceURI),
					field.String("did", did),
					field.String("collection", record.Collection),
					field.String("rkey", record.RKey),
				)
				libxrpc.WriteInternalServerError(responseWriter)
				return
			}
			item.Value = json.RawMessage(value)
		}

		items = append(items, item)
	}

	var cursor string
	if limit == len(stored) {
		var last libdb.Record = stored[len(stored)-1]
		cursor = last.Collection + "/" + last.RKey
	}

	body, err := libxrpc.MarshalJSON(struct{
		Cursor  string           `json:"cursor,omitempty"`
		Records []internalRecord `json:"records"`
	}{
		Cursor:  cursor,
		Records: items,
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
