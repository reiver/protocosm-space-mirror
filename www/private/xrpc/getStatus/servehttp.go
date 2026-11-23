package verboten

import (
	"database/sql"
	"net/http"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"
	"github.com/reiver/go-opt"

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
const Path string = "/xrpc/" + libnsid.NSIDPrefix + ".getStatus"

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

// Handler serves getStatus: the status of every space the space-mirror mirrors (paginated), ordered by space URI.
// It is also how a service finds out which spaces exist — a space missing from here should be dropped.
//
// Query parameters:
//
//	• cursor (optional),
//	• limit (optional; 1..1000, default 100),
//	• space (optional) — only this space,
//	• includeRepos (optional) — also the status of each repo; only allowed together with 'space'.
//
// Response: {"cursor": …, "seq": …, "spaces": [{"space": …, "lifecycle": …, "serving": …, "verifiedAt": …, "staleReasons": […], "repos": […]}, …]}.
//
// "seq" is the change-log high-water mark at the time. With the "admin" permission, hidden things are included, with "hideReasons".
type Handler struct {
	Logger log.Logger
	DB     *sql.DB
	Tokens []libtkn.Token
}

type internalRepo struct {
	DID          string   `json:"did"`
	Rev          string   `json:"rev"`
	Visible      bool     `json:"visible"`
	VerifiedAt   string   `json:"verifiedAt,omitempty"`
	StaleReasons []string `json:"staleReasons,omitempty"`
	HideReasons  []string `json:"hideReasons,omitempty"`
}

type internalSpace struct {
	Space        string         `json:"space"`
	Lifecycle    string         `json:"lifecycle"`
	Serving      bool           `json:"serving"`
	VerifiedAt   string         `json:"verifiedAt,omitempty"`
	StaleReasons []string       `json:"staleReasons,omitempty"`
	HideReasons  []string       `json:"hideReasons,omitempty"`
	Repos        []internalRepo `json:"repos,omitempty"`
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

	var cursor string = query.Get("cursor")
	if "" != cursor && !libxrpc.ValidSpaceRef(cursor) {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidCursor, "invalid 'cursor' parameter")
		return
	}

	var onlySpace opt.Optional[string]
	if s := query.Get("space"); "" != s {
		if !libxrpc.ValidSpaceRef(s) {
			libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid 'space' parameter")
			return
		}
		onlySpace = opt.Something(s)
	}

	limit, valid := libxrpc.Limit(query, defaultLimit, maxLimit)
	if !valid {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid 'limit' parameter")
		return
	}

	includeRepos, valid := libxrpc.Bool(query, "includeRepos")
	if !valid {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid 'includeRepos' parameter")
		return
	}
	if includeRepos && onlySpace.IsNothing() {
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "'includeRepos' requires 'space'")
		return
	}

	statuses, highWaterSeq, err := libdb.SpacesStatusList(log, receiver.DB, onlySpace, cursor, limit)
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	var spaces []internalSpace = make([]internalSpace, 0, len(statuses))
	for _, status := range statuses {
		var space = internalSpace{
			Space:        status.SpaceURI,
			Lifecycle:    status.Lifecycle,
			Serving:      status.Serving,
			VerifiedAt:   libxrpc.FormatTime(status.Freshness.VerifiedAt),
			StaleReasons: status.Freshness.StaleReasons,
		}
		if isAdmin {
			space.HideReasons = status.HideReasons
		}

		if includeRepos && (status.Serving || isAdmin) {
			repoStatuses, err := libdb.RepoStatusesList(log, receiver.DB, status.SpaceURI)
			if nil != err {
				libxrpc.WriteInternalServerError(responseWriter)
				return
			}

			for _, repoStatus := range repoStatuses {
				if !repoStatus.Visible && !isAdmin {
					continue
				}

				var repo = internalRepo{
					DID:          repoStatus.DID,
					Rev:          repoStatus.Rev,
					Visible:      repoStatus.Visible,
					VerifiedAt:   libxrpc.FormatTime(repoStatus.Freshness.VerifiedAt),
					StaleReasons: repoStatus.Freshness.StaleReasons,
				}
				if isAdmin {
					repo.HideReasons = repoStatus.HideReasons
				}
				space.Repos = append(space.Repos, repo)
			}
		}

		spaces = append(spaces, space)
	}

	var nextCursor string
	if limit == len(statuses) {
		nextCursor = statuses[len(statuses)-1].SpaceURI
	}

	body, err := libxrpc.MarshalJSON(struct{
		Cursor string          `json:"cursor,omitempty"`
		Seq    int64           `json:"seq"`
		Spaces []internalSpace `json:"spaces"`
	}{
		Cursor: nextCursor,
		Seq:    highWaterSeq,
		Spaces: spaces,
	})
	if nil != err {
		log.Error(field.S("failed to render response"), field.E(err))
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	libxrpc.WriteJSON(responseWriter, http.StatusOK, body)
}
