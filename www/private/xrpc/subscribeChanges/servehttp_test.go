package verboten_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"protocosm-space-mirror/lib/changelog"
	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/testfixtures"
	"protocosm-space-mirror/lib/xrpc"
	"protocosm-space-mirror/www/private/xrpc/subscribeChanges"
)

type internalEvent struct {
	ID   string
	Name string
	Data string
}

// readEvents reads SSE events (skipping comments) from 'reader' into a channel.
func readEvents(reader *bufio.Reader) <-chan internalEvent {
	var events = make(chan internalEvent, 100)

	go func() {
		defer close(events)

		var event internalEvent
		for {
			line, err := reader.ReadString('\n')
			if nil != err {
				return
			}
			line = strings.TrimSuffix(line, "\n")

			switch {
			case "" == line:
				if "" != event.ID || "" != event.Data {
					events <- event
				}
				event = internalEvent{}
			case strings.HasPrefix(line, ":"):
				// A comment (heartbeat).
			case strings.HasPrefix(line, "id: "):
				event.ID = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				event.Name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				event.Data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	return events
}

func nextEvent(t *testing.T, events <-chan internalEvent) internalEvent {
	t.Helper()

	select {
	case event, ok := <-events:
		if !ok {
			t.Fatalf("The stream ended unexpectedly.")
		}
		return event
	case <-time.After(5 * time.Second):
		t.Fatalf("Timed out waiting for an event.")
	}
	return internalEvent{}
}

func TestServeHTTP_replayThenLive(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	var notifier libchangelog.Notifier

	// One entry before connecting.
	if _, err := libdb.AddHide(forum.Logger, forum.DB, libdb.HideScope{Kind: libdb.ScopeAccount, DID: libtestfixtures.Bob}, libdb.ReasonOperator, 1795435200); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}

	server := httptest.NewServer(verboten.Handler{
		Logger:    forum.Logger,
		DB:        forum.DB,
		Tokens:    forum.Tokens,
		Notifier:  &notifier,
		Heartbeat: 50 * time.Millisecond,
		MaxLag:    verboten.DefaultMaxLag,
	})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL + "?since=0", nil)
	req.Header.Set("Authorization", "Bearer " + libtestfixtures.ReadToken)

	resp, err := http.DefaultClient.Do(req)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	defer resp.Body.Close()

	if http.StatusOK != resp.StatusCode || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("Expected a 200 event stream but actually got %d %q.", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	events := readEvents(bufio.NewReader(resp.Body))

	// The replay.
	{
		event := nextEvent(t, events)

		var change libchangelog.ChangeJSON
		if err := json.Unmarshal([]byte(event.Data), &change); nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s\nDATA: %s", err, err, event.Data)
		}
		if "1" != event.ID || "change" != event.Name || "hidden" != change.Kind || libtestfixtures.Bob != change.Repo {
			t.Errorf("The actual replayed event is not what was expected: %#v %#v", event, change)
		}
	}

	// Let a few heartbeats pass — they must not show up as events.
	time.Sleep(150 * time.Millisecond)

	// A live entry.
	if _, err := libdb.AddHide(forum.Logger, forum.DB, libdb.HideScope{Kind: libdb.ScopeSpace, SpaceURI: libtestfixtures.Space}, libdb.ReasonOperator, 1795435201); nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	notifier.Notify()

	{
		event := nextEvent(t, events)
		if "2" != event.ID || !strings.Contains(event.Data, `"scope":"space"`) {
			t.Errorf("The actual live event is not what was expected: %#v", event)
		}
	}
}

func TestServeHTTP_lastEventID(t *testing.T) {
	forum := libtestfixtures.NewForum(t)

	for _, scope := range []libdb.HideScope{
		{Kind: libdb.ScopeAccount, DID: libtestfixtures.Bob},
		{Kind: libdb.ScopeAccount, DID: libtestfixtures.Alice},
	} {
		if _, err := libdb.AddHide(forum.Logger, forum.DB, scope, libdb.ReasonOperator, 1); nil != err {
			t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
		}
	}

	server := httptest.NewServer(verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens, Heartbeat: time.Second, MaxLag: verboten.DefaultMaxLag})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	req.Header.Set("Authorization", "Bearer " + libtestfixtures.ReadToken)
	req.Header.Set("Last-Event-ID", "1")

	resp, err := http.DefaultClient.Do(req)
	if nil != err {
		t.Fatalf("Did not expect an error but actually got one: (%T) %s", err, err)
	}
	defer resp.Body.Close()

	event := nextEvent(t, readEvents(bufio.NewReader(resp.Body)))
	if "2" != event.ID {
		t.Errorf("Expected to resume after Last-Event-ID 1 (at 2), but actually got %q.", event.ID)
	}
}

func TestServeHTTP_errors(t *testing.T) {
	forum := libtestfixtures.NewForum(t)
	for i:=0; i<5; i++ {
		forum.Exec(t, `INSERT INTO changelog (at, kind) VALUES (1, 'record')`)
	}
	forum.Exec(t, `UPDATE meta SET changelog_floor_seq = 2`)

	handler := verboten.Handler{Logger: forum.Logger, DB: forum.DB, Tokens: forum.Tokens, Heartbeat: time.Second, MaxLag: 2}

	tests := []struct{
		Since string
		ExpectedError string
	}{
		{
			Since:         "1",
			ExpectedError: libxrpc.ErrorCursorTooOld,
		},
		{
			// 5 - 2 = 3 behind, more than MaxLag 2
			Since:         "2",
			ExpectedError: libxrpc.ErrorCursorTooFarBehind,
		},
		{
			Since:         "x",
			ExpectedError: libxrpc.ErrorInvalidRequest,
		},
	}

	for testNumber, test := range tests {
		req := httptest.NewRequest(http.MethodGet, verboten.Path + "?since=" + test.Since, nil)
		req.Header.Set("Authorization", "Bearer " + libtestfixtures.ReadToken)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		var body struct{
			Error string `json:"error"`
		}
		json.Unmarshal(recorder.Body.Bytes(), &body)

		if http.StatusBadRequest != recorder.Code || test.ExpectedError != body.Error {
			t.Errorf("For test #%d, the actual response is not what was expected.", testNumber)
			t.Logf("EXPECTED: 400 %s", test.ExpectedError)
			t.Logf("ACTUAL:   %d %s", recorder.Code, body.Error)
			continue
		}
	}
}
