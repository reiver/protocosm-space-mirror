package verboten

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"

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
const Path string = "/xrpc/" + libnsid.NSIDPrefixRepo + ".getRecord"

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

// Handler serves getRecord: a single record from the space-mirror's copy.
//
// Query parameters: space, repo, collection, rkey.
// Response: {"uri": …, "cid": …, "value": …}.
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

	var spaceURI   string = query.Get("space")
	var did        string = query.Get("repo")
	var collection string = query.Get("collection")
	var rkey       string = query.Get("rkey")

	switch {
	case !libxrpc.ValidSpaceRef(spaceURI):
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'space' parameter")
		return
	case !libxrpc.ValidDID(did):
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'repo' parameter")
		return
	case !libxrpc.ValidNSID(collection):
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'collection' parameter")
		return
	case !libxrpc.ValidRecordKey(rkey):
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid or missing 'rkey' parameter")
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

	record, found, err := libdb.RecordSelect(log, receiver.DB, spaceURI, did, collection, rkey)
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}
	if !found {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorRecordNotFound, "record not found")
		return
	}

	value, err := librecords.ToJSON(record.DRISL)
	if nil != err {
		log.Error(
			field.S("failed to render stored record as JSON"),
			field.E(err),
			field.String("space-uri", spaceURI),
			field.String("did", did),
			field.String("collection", collection),
			field.String("rkey", rkey),
		)
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	body, err := libxrpc.MarshalJSON(struct{
		URI   string          `json:"uri"`
		CID   string          `json:"cid"`
		Value json.RawMessage `json:"value"`
	}{
		URI:   spaceURI + "/" + did + "/" + collection + "/" + rkey,
		CID:   record.CID,
		Value: json.RawMessage(value),
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
