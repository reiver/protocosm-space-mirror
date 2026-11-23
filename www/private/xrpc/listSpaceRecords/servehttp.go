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
const Path string = "/xrpc/" + libnsid.NSIDPrefix + ".listSpaceRecords"

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

// Handler serves listSpaceRecords: the visible records across all the repos of a space, ordered by (repo, collection, rkey).
// It is how a service loads (or re-loads) a whole space.
//
// Query parameters: space, collection (optional), limit (optional; 1..100, default 50), cursor (optional), excludeValues (optional).
// Response: {"cursor": … (when there may be more), "seq": … (first page only), "records": [{"repo": …, "collection": …, "rkey": …, "cid": …, "value": … (unless excludeValues)}, …]}.
//
// Loading a space:
//
//	• Remember the "seq" from the first page.
//	• Page through to the end. (Pages are not one snapshot — the space may change while paging.)
//	• Replace — do not merge with — whatever was held for the space.
//	• Then follow the change log from that "seq", which repairs anything that changed while paging.
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
	if !libxrpc.ValidSpaceRef(spaceURI) {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'space' parameter")
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

	// The cursor is "{did}/{collection}/{rkey}" of the last record of the previous page.
	// (None of the three can contain a "/".)
	var afterDID, afterCollection, afterRKey string
	var firstPage bool = true
	if cursor := query.Get("cursor"); "" != cursor {
		var parts []string = strings.Split(cursor, "/")
		if 3 != len(parts) || !libxrpc.ValidDID(parts[0]) || !libxrpc.ValidNSID(parts[1]) || !libxrpc.ValidRecordKey(parts[2]) {
			libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidCursor, "invalid 'cursor' parameter")
			return
		}
		afterDID, afterCollection, afterRKey = parts[0], parts[1], parts[2]
		firstPage = false
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

	stored, highWaterSeq, err := libdb.SpaceRecordsList(log, receiver.DB, spaceURI, collection, afterDID, afterCollection, afterRKey, limit)
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	type internalRecord struct {
		Repo       string          `json:"repo"`
		Collection string          `json:"collection"`
		RKey       string          `json:"rkey"`
		CID        string          `json:"cid"`
		Value      json.RawMessage `json:"value,omitempty"`
	}

	var items []internalRecord = make([]internalRecord, 0, len(stored))
	for _, record := range stored {
		var item = internalRecord{
			Repo:       record.DID,
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
					field.String("did", record.DID),
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

	var nextCursor string
	if limit == len(stored) {
		var last libdb.SpaceRecord = stored[len(stored)-1]
		nextCursor = last.DID + "/" + last.Collection + "/" + last.RKey
	}

	var seq *int64
	if firstPage {
		seq = &highWaterSeq
	}

	body, err := libxrpc.MarshalJSON(struct{
		Cursor  string           `json:"cursor,omitempty"`
		Seq     *int64           `json:"seq,omitempty"`
		Records []internalRecord `json:"records"`
	}{
		Cursor:  nextCursor,
		Seq:     seq,
		Records: items,
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
