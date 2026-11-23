package verboten

import (
	"database/sql"
	"io"
	"net/http"
	"strconv"
	"time"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
	"codeberg.org/reiver/go-log"

	"protocosm-space-mirror/lib/changelog"
	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/nsid"
	"protocosm-space-mirror/lib/tkn"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/srv/changelog"
	"protocosm-space-mirror/srv/db"
	"protocosm-space-mirror/srv/internalhttp"
	"protocosm-space-mirror/srv/log"
	"protocosm-space-mirror/srv/tkn"
)

// Path is the HTTP path of this XRPC query.
const Path string = "/xrpc/" + libnsid.NSIDPrefix + ".subscribeChanges"

const (
	// DefaultHeartbeat is how often a comment is sent when there is nothing new
	// (so proxies don't close an idle stream, and a gone client is noticed).
	DefaultHeartbeat = 15 * time.Second

	// DefaultMaxLag is how far behind (in entries) a stream may be.
	// A consumer further behind should catch up with getChanges first.
	DefaultMaxLag = 10000

	// batchSize is how many entries are read from the database at a time.
	batchSize = 500
)

func init() {
	var handler http.Handler = Handler{
		Logger:    logsrv.Logger(),
		DB:        dbsrv.ReadDB(),
		Tokens:    tknsrv.Tokens(),
		Notifier:  changelogsrv.Notifier(),
		Heartbeat: DefaultHeartbeat,
		MaxLag:    DefaultMaxLag,
	}

	err := internalhttpsrv.Mux.HandlePath(handler, Path)
	if nil != err {
		err = erorr.Wrap(err, "problem registering HTTP-handler for path",
			field.String("path", Path),
		)
		panic(err)
	}
}

// Handler serves subscribeChanges: the change log as a stream of server-sent events (SSE).
//
// Query parameters: since (optional; a seq; default 0). The "Last-Event-ID" request header, if present, takes its place
// (so an SSE client that reconnects resumes where it left off).
//
// Each entry is sent as:
//
//	id: <seq>
//	event: change
//	data: {"seq": …, "at": …, "kind": …, …}
//
// The stream first replays the entries after "since", then sends new ones as they are added.
// When there is nothing new, a comment is sent every so often (a heartbeat).
//
// Before streaming: CursorTooOld if "since" is older than the change log keeps (reload);
// CursorTooFarBehind if "since" is more than MaxLag entries behind (catch up with getChanges first).
// While streaming: if the stream falls more than MaxLag entries behind, it is closed.
type Handler struct {
	Logger    log.Logger
	DB        *sql.DB
	Tokens    []libtkn.Token
	Notifier  *libchangelog.Notifier
	Heartbeat time.Duration
	MaxLag    int64
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

	var position int64
	{
		var s string = request.Header.Get("Last-Event-ID")
		if "" == s {
			s = request.URL.Query().Get("since")
		}

		var valid bool
		position, valid = libxrpc.ParseSeq(s)
		if !valid {
			libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorInvalidRequest, "invalid 'since' parameter (or Last-Event-ID header)")
			return
		}
	}

	{
		floorSeq, maxSeq, err := libdb.ChangelogBounds(log, receiver.DB)
		if nil != err {
			libxrpc.WriteInternalServerError(responseWriter)
			return
		}
		if position < floorSeq {
			libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorCursorTooOld, "'since' is older than the change log keeps — reload")
			return
		}
		if receiver.MaxLag < maxSeq - position {
			libxrpc.WriteError(responseWriter, http.StatusBadRequest, libxrpc.ErrorCursorTooFarBehind, "'since' is too far behind — catch up with getChanges first")
			return
		}
	}

	var controller *http.ResponseController = http.NewResponseController(responseWriter)

	{
		var header http.Header = responseWriter.Header()
		header.Set("Content-Type", "text/event-stream; charset=utf-8")
		header.Set("Cache-Control", "no-cache")
		header.Set("X-Accel-Buffering", "no")
		responseWriter.WriteHeader(http.StatusOK)

		if err := controller.Flush(); nil != err {
			log.Error(field.S("failed to flush"), field.E(err))
			return
		}
	}

	log.Inform(field.S("subscriber connected"), field.String("service", token.Name), field.Int64("since", position))
	defer log.Inform(field.S("subscriber disconnected"), field.String("service", token.Name))

	var heartbeat *time.Ticker = time.NewTicker(receiver.Heartbeat)
	defer heartbeat.Stop()

	for {
		// Get the wait channel before reading, so an entry committed in between is not missed.
		var wait <-chan struct{}
		if nil != receiver.Notifier {
			wait = receiver.Notifier.Wait()
		}

		changes, err := libdb.ChangesList(log, receiver.DB, position, batchSize)
		if nil != err {
			return
		}

		for _, change := range changes {
			data, err := libchangelog.Marshal(change)
			if nil != err {
				log.Error(field.S("failed to render change"), field.E(err), field.Int64("seq", change.Seq))
				return
			}

			var event string =
				"id: " + strconv.FormatInt(change.Seq, 10) + "\n" +
				"event: change\n" +
				"data: " + string(data) + "\n" +
				"\n"
			if _, err := io.WriteString(responseWriter, event); nil != err {
				return
			}

			position = change.Seq
		}
		if 0 < len(changes) {
			if err := controller.Flush(); nil != err {
				return
			}
		}

		if batchSize == len(changes) {
			// There may be more — but don't let the stream fall too far behind.
			_, maxSeq, err := libdb.ChangelogBounds(log, receiver.DB)
			if nil != err {
				return
			}
			if receiver.MaxLag < maxSeq - position {
				log.Warn(field.S("subscriber too far behind; closing"), field.String("service", token.Name), field.Int64("position", position), field.Int64("max-seq", maxSeq))
				return
			}
			continue
		}

		select {
		case <-request.Context().Done():
			return
		case <-wait:
			// There is something new.
		case <-heartbeat.C:
			if _, err := io.WriteString(responseWriter, ": heartbeat\n\n"); nil != err {
				return
			}
			if err := controller.Flush(); nil != err {
				return
			}
		}
	}
}
