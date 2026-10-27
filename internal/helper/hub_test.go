package helper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
)

func setup(t *testing.T) *Hub {
	t.Helper()
	dir := t.TempDir()
	key, _ := state.LoadKey(dir)
	db, e := state.Open(dir, key)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return New(db)
}
func TestPairingExpiresAndIsOneUse(t *testing.T) {
	h := setup(t)
	now := time.Now()
	h.now = func() time.Time { return now }
	p, e := h.Pair()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.Redeem("invalid"); e == nil {
		t.Fatal("invalid code accepted")
	}
	now = now.Add(6 * time.Minute)
	if _, e = h.Redeem(p.Code); e == nil {
		t.Fatal("expired code accepted")
	}
	p, _ = h.Pair()
	token, e := h.Redeem(p.Code)
	if e != nil || !h.valid(token) {
		t.Fatal("valid pairing failed", e)
	}
	if _, e = h.Redeem(p.Code); e == nil {
		t.Fatal("code reused")
	}
	p, _ = h.Pair()
	replacement, _ := h.Redeem(p.Code)
	if h.valid(token) || !h.valid(replacement) {
		t.Fatal("old credential survived replacement")
	}
	if h.Revoke() != nil || h.valid(replacement) {
		t.Fatal("revoke failed")
	}
}
func TestOfflineAndUnsupportedCallsNeverQueue(t *testing.T) {
	h := setup(t)
	for _, op := range []string{"reminders.sync", "exec"} {
		var out any
		if e := h.Call(context.Background(), op, map[string]any{}, &out); e == nil {
			t.Fatal("unavailable operation accepted")
		}
	}
	r := (Executor{}).Execute(context.Background(), Request{ID: "x", Operation: "exec", Input: json.RawMessage(`{"cmd":"anything"}`)})
	if r.Error == "" {
		t.Fatal("untrusted operation reached native execution")
	}
}
func connect(t *testing.T, h *Hub) (*websocket.Conn, *httptest.Server) {
	t.Helper()
	p, _ := h.Pair()
	token, _ := h.Redeem(p.Code)
	server := httptest.NewServer(http.HandlerFunc(h.Accept))
	t.Cleanup(server.Close)
	conn, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), http.Header{"Authorization": []string{"Bearer " + token}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var ready Request
	if e := conn.ReadJSON(&ready); e != nil || ready.Operation != "ready" {
		t.Fatal("helper did not become ready", e)
	}
	return conn, server
}
func TestTypedRoundTripAndDisconnectWithoutReplay(t *testing.T) {
	h := setup(t)
	conn, _ := connect(t, h)
	done := make(chan error, 1)
	var out map[string]int
	go func() {
		done <- h.Call(context.Background(), "reminders.sync", map[string]any{"assignments": []any{}}, &out)
	}()
	var request Request
	if e := conn.ReadJSON(&request); e != nil {
		t.Fatal(e)
	}
	if request.Operation != "reminders.sync" {
		t.Fatal(request)
	}
	var second any
	if h.Call(context.Background(), "calendar.sync", nil, &second) == nil {
		t.Fatal("parallel native write accepted")
	}
	conn.WriteJSON(Result{ID: request.ID, Output: json.RawMessage(`{"created":2}`)})
	if e := <-done; e != nil || out["created"] != 2 {
		t.Fatal(out, e)
	}
	go func() { done <- h.Call(context.Background(), "calendar.sync", map[string]any{}, &out) }()
	if conn.ReadJSON(&request) != nil {
		t.Fatal("request missing")
	}
	conn.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("disconnect reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect did not fail pending work")
	}
	if h.Connected() {
		t.Fatal("disconnected peer still available")
	}
	if h.Call(context.Background(), "calendar.sync", nil, &out) == nil {
		t.Fatal("work queued offline")
	}
}
func TestCancellationClosesConnectionAndUnauthorizedHandshake(t *testing.T) {
	h := setup(t)
	conn, server := connect(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { var out any; done <- h.Call(ctx, "categories.list", struct{}{}, &out) }()
	var request Request
	if conn.ReadJSON(&request) != nil {
		t.Fatal("request missing")
	}
	cancel()
	if <-done == nil {
		t.Fatal("cancellation ignored")
	}
	_, response, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if e == nil || response.StatusCode != 401 {
		t.Fatal("anonymous helper connected")
	}
}
