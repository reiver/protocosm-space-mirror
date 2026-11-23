package verboten

import (
	"net/http"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"

	"protocosm-space-mirror/lib/nsid"
	"protocosm-space-mirror/lib/spaces"
	"protocosm-space-mirror/lib/tkn"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/srv/internalhttp"
	"protocosm-space-mirror/srv/log"
	"protocosm-space-mirror/srv/spaces"
	"protocosm-space-mirror/srv/tkn"
)

// Path is the HTTP path of this XRPC procedure.
const Path string = "/xrpc/" + libnsid.NSIDPrefix + ".reloadSpaces"

// The XRPC error names this end-point adds.
const (
	ErrorInvalidSpacesFile    = "InvalidSpacesFile"
	ErrorSpacesFileUnreadable = "SpacesFileUnreadable"
	ErrorTooManyUnlisted      = "TooManyUnlisted"
)

func init() {
	var handler http.Handler = Handler{
		Logger: logsrv.Logger(),
		Tokens: tknsrv.Tokens(),
		Reload: spacessrv.Reload,
	}

	err := internalhttpsrv.Mux.HandlePath(handler, Path)
	if nil != err {
		err = erorr.Wrap(err, "problem registering HTTP-handler for path",
			field.String("path", Path),
		)
		panic(err)
	}
}

// Handler serves reloadSpaces: re-read the spaces file and apply it (the same as a SIGHUP — but it can force).
//
// It requires a token with the "admin" permission.
//
// Input (optional): {"force": true} — allows this one reload to unlist more spaces than the limits allow.
// Output: {"added": […], "unlisted": […], "relisted": […]}.
//
// Errors: InvalidSpacesFile, TooManyUnlisted (both 400; nothing changed), SpacesFileUnreadable (500; nothing changed).
type Handler struct {
	Logger log.Logger
	Tokens []libtkn.Token
	Reload func(force bool) (libspaces.Diff, error)
}

func (receiver Handler) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	log := receiver.Logger.Begin(field.String("www.path", Path))
	defer log.End()

	if !libxrpc.RequirePOST(responseWriter, request) {
		return
	}

	token, authorized := libxrpc.Authorize(responseWriter, request, receiver.Tokens, libtkn.PermissionAdmin)
	if !authorized {
		return
	}

	var input struct {
		Force bool `json:"force"`
	}
	if 0 != request.ContentLength {
		if !libxrpc.ReadJSONBody(responseWriter, request, &input) {
			return
		}
	}

	log.Inform(field.S("reloading the spaces file"), field.String("service", token.Name), field.Bool("force", input.Force))

	if nil == receiver.Reload {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	diff, err := receiver.Reload(input.Force)
	switch {
	case nil == err:
		// Nothing here.
	case erorr.Is(err, libspaces.ErrBadLine):
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, ErrorInvalidSpacesFile, err.Error())
		return
	case erorr.Is(err, libspaces.ErrTooManyUnlisted):
		libxrpc.WriteError(responseWriter, http.StatusBadRequest, ErrorTooManyUnlisted, err.Error())
		return
	default:
		libxrpc.WriteError(responseWriter, http.StatusInternalServerError, ErrorSpacesFileUnreadable, "the spaces file could not be read or applied")
		return
	}

	var nonNil = func(list []string) []string {
		if nil == list {
			return []string{}
		}
		return list
	}

	body, err := libxrpc.MarshalJSON(struct{
		Added    []string `json:"added"`
		Unlisted []string `json:"unlisted"`
		Relisted []string `json:"relisted"`
	}{
		Added:    nonNil(diff.Added),
		Unlisted: nonNil(diff.Removed),
		Relisted: nonNil(diff.Relisted),
	})
	if nil != err {
		libxrpc.WriteInternalServerError(responseWriter)
		return
	}

	libxrpc.WriteJSON(responseWriter, http.StatusOK, body)
}
