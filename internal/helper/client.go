package helper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/leehyowon14/KLAP-cli/pkg/core"
)

type Executor struct {
	Reminders  core.ReminderSyncer
	Calendar   core.CalendarSyncer
	Categories core.CategoryLister
}

func (e Executor) Execute(ctx context.Context, r Request) Result {
	result := Result{ID: r.ID}
	var value any
	var err error
	decode := func(v any) error {
		d := json.NewDecoder(strings.NewReader(string(r.Input)))
		d.DisallowUnknownFields()
		return d.Decode(v)
	}
	switch r.Operation {
	case "reminders.sync":
		var p core.ReminderRequest
		if err = decode(&p); err == nil {
			value, err = e.Reminders.Sync(ctx, p)
		}
	case "calendar.sync":
		var p core.CalendarRequest
		if err = decode(&p); err == nil {
			value, err = e.Calendar.Sync(ctx, p)
		}
	case "categories.list":
		var p struct{}
		if err = decode(&p); err == nil {
			value, err = e.Categories.List(ctx)
		}
	default:
		err = errors.New("unsupported Mac operation")
	}
	if err != nil {
		result.Error = err.Error()
	} else {
		result.Output, err = json.Marshal(value)
		if err != nil {
			result.Error = err.Error()
		}
	}
	return result
}

// RunClient makes a persistent outbound connection. Only the connection retries;
// a disconnected in-flight Apple operation is never replayed automatically.
func RunClient(ctx context.Context, base, token string, executor Executor, onState func(bool)) error {
	u, e := url.Parse(strings.TrimRight(base, "/"))
	if e != nil || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("invalid helper server URL")
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else if u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1") {
		u.Scheme = "ws"
	} else {
		return errors.New("Mac helper requires HTTPS")
	}
	u.Path = "/api/helper/connect"
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, response, err := websocket.DefaultDialer.DialContext(ctx, u.String(), http.Header{"Authorization": []string{"Bearer " + token}})
		if response != nil && response.StatusCode == 401 {
			return errors.New("Mac pairing was revoked; pair this Mac again")
		}
		if err == nil {
			conn.SetReadLimit(1024 * 1024)
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			var ready Request
			if e := conn.ReadJSON(&ready); e != nil || ready.Operation != "ready" {
				conn.Close()
				return errors.New("Mac helper handshake failed")
			}
			_ = conn.SetReadDeadline(time.Time{})
			if onState != nil {
				onState(true)
			}
			serveClient(ctx, conn, executor)
			if onState != nil {
				onState(false)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
func serveClient(ctx context.Context, conn *websocket.Conn, executor Executor) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer conn.Close()
	go func() { <-ctx.Done(); conn.Close() }()
	conn.SetReadLimit(1024 * 1024)
	requests := make(chan Request, 1)
	go func() {
		defer cancel()
		defer close(requests)
		for {
			var r Request
			if conn.ReadJSON(&r) != nil {
				return
			}
			select {
			case requests <- r:
			case <-ctx.Done():
				return
			default:
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case r, ok := <-requests:
			if !ok {
				return
			}
			jobCtx, stop := context.WithTimeout(ctx, 2*time.Minute)
			result := executor.Execute(jobCtx, r)
			stop()
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if conn.WriteJSON(result) != nil {
				return
			}
		}
	}
}
