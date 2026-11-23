package libtakedown

import (
	"database/sql"
	"net/http"
	"time"

	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"

	"protocosm-space-mirror/lib/changelog"
	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/tkn"
	"protocosm-space-mirror/lib/xrpc"
)

// Config is what an operator-takedown end-point needs.
type Config struct {
	Logger   log.Logger
	DB       *sql.DB // the write database
	Tokens   []libtkn.Token
	Notifier *libchangelog.Notifier

	// ScopeKind is libdb.ScopeSpace, libdb.ScopeAccount, or libdb.ScopeRecord.
	ScopeKind string

	// Hide is true for hide*, and false for unhide*.
	Hide bool

	// Path is the end-point's HTTP path (for logging).
	Path string
}

// Serve handles an operator-takedown XRPC procedure (hideSpace, unhideSpace, hideAccount, unhideAccount, hideRecord, unhideRecord).
//
// It requires a token with the "admin" permission.
//
// The JSON input is:
//
//	• space scope:   {"space": …}
//	• account scope: {"did": …}
//	• record scope:  {"space": …, "repo": …, "collection": …, "rkey": …, "cid": … (optional — only that version)}
//
// The output is {"changed": true|false} — false if the operator hide was already there (hide*), or was not there (unhide*).
// A change to what consumers can see is announced in the change log ("hidden" / "visible").
func Serve(config Config, responseWriter http.ResponseWriter, request *http.Request) {
	log := config.Logger.Begin(field.String("www.path", config.Path))
	defer log.End()

	if !libxrpc.RequirePOST(responseWriter, request) {
		return
	}

	token, authorized := libxrpc.Authorize(responseWriter, request, config.Tokens, libtkn.PermissionAdmin)
	if !authorized {
		return
	}

	var input struct {
		Space      string `json:"space"`
		DID        string `json:"did"`
		Repo       string `json:"repo"`
		Collection string `json:"collection"`
		RKey       string `json:"rkey"`
		CID        string `json:"cid"`
	}
	if !libxrpc.ReadJSONBody(responseWriter, request, &input) {
		return
	}

	var scope = libdb.HideScope{Kind: config.ScopeKind}
	var valid bool
	switch config.ScopeKind {
	case libdb.ScopeSpace:
		scope.SpaceURI = input.Space
		valid = libxrpc.ValidSpaceRef(input.Space) && "" == input.DID && "" == input.Repo && "" == input.Collection && "" == input.RKey && "" == input.CID
	case libdb.ScopeAccount:
		scope.DID = input.DID
		valid = libxrpc.ValidDID(input.DID) && "" == input.Space && "" == input.Repo && "" == input.Collection && "" == input.RKey && "" == input.CID
	case libdb.ScopeRecord:
		scope.SpaceURI = input.Space
		scope.DID = input.Repo
		scope.Collection = input.Collection
		scope.RKey = input.RKey
		scope.CID = input.CID
		valid = libxrpc.ValidSpaceRef(input.Space) && libxrpc.ValidDID(input.Repo) && libxrpc.ValidNSID(input.Collection) && libxrpc.ValidRecordKey(input.RKey) && "" == input.DID && len(input.CID) <= 256
	}
	if !valid {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid input for a "+config.ScopeKind+" takedown")
		return
	}

	var now int64 = time.Now().Unix()

	var changed bool
	var err error
	if config.Hide {
		changed, err = libdb.AddHide(log, config.DB, scope, libdb.ReasonOperator, now)
	} else {
		changed, err = libdb.RemoveHide(log, config.DB, scope, libdb.ReasonOperator, now)
	}
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	log.Inform(
		field.S("operator takedown"),
		field.String("service", token.Name),
		field.Bool("hide", config.Hide),
		field.String("scope-kind", scope.Kind),
		field.String("space-uri", scope.SpaceURI),
		field.String("did", scope.DID),
		field.String("collection", scope.Collection),
		field.String("rkey", scope.RKey),
		field.String("cid", scope.CID),
		field.Bool("changed", changed),
	)

	if changed && nil != config.Notifier {
		config.Notifier.Notify()
	}

	body, err := libxrpc.MarshalJSON(struct{
		Changed bool `json:"changed"`
	}{
		Changed: changed,
	})
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	libxrpc.WriteJSON(responseWriter, http.StatusOK, body)
}
