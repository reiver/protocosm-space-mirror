package verboten

import (
	"database/sql"
	"net/http"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"

	"protocosm-space-mirror/lib/changelog"
	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/nsid"
	"protocosm-space-mirror/lib/takedown"
	"protocosm-space-mirror/lib/tkn"
	"protocosm-space-mirror/srv/changelog"
	"protocosm-space-mirror/srv/db"
	"protocosm-space-mirror/srv/internalhttp"
	"protocosm-space-mirror/srv/log"
	"protocosm-space-mirror/srv/tkn"
)

// Path is the HTTP path of this XRPC procedure.
const Path string = "/xrpc/" + libnsid.NSIDPrefix + ".hideSpace"

func init() {
	var handler http.Handler = Handler{
		Logger:   logsrv.Logger(),
		DB:       dbsrv.WriteDB(),
		Tokens:   tknsrv.Tokens(),
		Notifier: changelogsrv.Notifier(),
	}

	err := internalhttpsrv.Mux.HandlePath(handler, Path)
	if nil != err {
		err = erorr.Wrap(err, "problem registering HTTP-handler for path",
			field.String("path", Path),
		)
		panic(err)
	}
}

// Handler serves hideSpace — an operator takedown. See libtakedown.Serve.
type Handler struct {
	Logger   log.Logger
	DB       *sql.DB
	Tokens   []libtkn.Token
	Notifier *libchangelog.Notifier
}

func (receiver Handler) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	libtakedown.Serve(libtakedown.Config{
		Logger:    receiver.Logger,
		DB:        receiver.DB,
		Tokens:    receiver.Tokens,
		Notifier:  receiver.Notifier,
		ScopeKind: libdb.ScopeSpace,
		Hide:      true,
		Path:      Path,
	}, responseWriter, request)
}
