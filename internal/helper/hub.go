// Package helper transports only typed EventKit requests to a paired Mac.
// KLAS authentication, fetching and sync policy remain in KLAP-core on the server.
package helper

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
)

type pairing struct {
	CodeHash, TokenHash string
	Expires             time.Time
}
type PairCode struct {
	Code    string    `json:"code"`
	Expires time.Time `json:"expires_at"`
}
type Request struct {
	ID        string          `json:"id"`
	Operation string          `json:"operation"`
	Input     json.RawMessage `json:"input"`
}
type Result struct {
	ID     string          `json:"id"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}
type peer struct {
	conn    *websocket.Conn
	write   sync.Mutex
	pending map[string]chan Result
	done    chan struct{}
}
type Hub struct {
	db       *state.Store
	mu       sync.Mutex
	client   *peer
	attempts int
	rateAt   time.Time
	now      func() time.Time
}

func New(db *state.Store) *Hub { return &Hub{db: db, now: time.Now} }
func digest(s string) string   { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func match(a, b string) bool {
	return a != "" && b != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func (h *Hub) Pair() (PairCode, error) {
	v := PairCode{Code: rand.Text() + rand.Text(), Expires: h.now().Add(5 * time.Minute)}
	e := h.db.Update(func(r state.Records) error {
		old, e := state.Get[pairing](r, "helper/pair")
		if e != nil && !errors.Is(e, state.ErrNotFound) {
			return e
		}
		old.CodeHash = digest(v.Code)
		old.Expires = v.Expires
		return state.Put(r, "helper/pair", old)
	})
	return v, e
}
func (h *Hub) Redeem(code string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.now().Sub(h.rateAt) > time.Minute {
		h.rateAt = h.now()
		h.attempts = 0
	}
	h.attempts++
	if h.attempts > 10 {
		return "", errors.New("pairing rate limit exceeded")
	}
	token := rand.Text() + rand.Text()
	e := h.db.Update(func(r state.Records) error {
		p, e := state.Get[pairing](r, "helper/pair")
		if e != nil || !h.now().Before(p.Expires) || !match(p.CodeHash, digest(code)) {
			return errors.New("invalid or expired pairing code")
		}
		return state.Put(r, "helper/pair", pairing{TokenHash: digest(token)})
	})
	if e != nil {
		return "", e
	}
	if h.client != nil {
		h.client.conn.Close()
	}
	return token, nil
}
func (h *Hub) valid(token string) bool {
	var p pairing
	e := h.db.View(func(r state.Records) error { var e error; p, e = state.Get[pairing](r, "helper/pair"); return e })
	return e == nil && match(p.TokenHash, digest(token))
}
func (h *Hub) Connected() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.client != nil }
func (h *Hub) Revoke() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e := h.db.Update(func(r state.Records) error { delete(r, "helper/pair"); return nil }); e != nil {
		return e
	}
	if h.client != nil {
		h.client.conn.Close()
	}
	return nil
}
func (h *Hub) Register(mux *http.ServeMux, owner func(http.Handler) http.Handler) {
	mux.Handle("POST /api/helper/pair", owner(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, e := h.Pair()
		if e != nil {
			http.Error(w, "pairing unavailable", 503)
			return
		}
		reply(w, v)
	})))
	mux.Handle("POST /api/helper/disconnect", owner(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := h.Revoke(); e != nil {
			http.Error(w, "disconnect failed", 503)
			return
		}
		reply(w, map[string]bool{"connected": false})
	})))
	mux.HandleFunc("POST /api/helper/redeem", func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Code string `json:"code"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		d.DisallowUnknownFields()
		if d.Decode(&p) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		token, e := h.Redeem(p.Code)
		if e != nil {
			http.Error(w, "invalid, expired, or rate-limited pairing code", 403)
			return
		}
		reply(w, map[string]string{"token": token})
	})
	mux.HandleFunc("GET /api/helper/connect", h.Accept)
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
func (h *Hub) Accept(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || !h.valid(token) {
		http.Error(w, "invalid helper token", 401)
		return
	}
	u := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }, HandshakeTimeout: 10 * time.Second}
	c, e := u.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	p := &peer{conn: c, pending: map[string]chan Result{}, done: make(chan struct{})}
	h.mu.Lock()
	if !h.valid(token) {
		h.mu.Unlock()
		c.Close()
		return
	}
	if h.client != nil {
		h.client.conn.Close()
	}
	h.client = p
	h.mu.Unlock()
	defer func() {
		c.Close()
		h.mu.Lock()
		if h.client == p {
			h.client = nil
		}
		close(p.done)
		h.mu.Unlock()
	}()
	c.SetReadLimit(1024 * 1024)
	_ = c.SetReadDeadline(time.Now().Add(50 * time.Second))
	c.SetPongHandler(func(string) error { return c.SetReadDeadline(time.Now().Add(50 * time.Second)) })
	// The ready frame makes successful upgrade and application registration one
	// observable handshake; callers cannot race a half-registered connection.
	p.write.Lock()
	e = c.WriteJSON(Request{Operation: "ready", Input: json.RawMessage(`{"protocol":1}`)})
	p.write.Unlock()
	if e != nil {
		return
	}
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-p.done:
				return
			case <-ticker.C:
				if c.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
					c.Close()
					return
				}
			}
		}
	}()
	for {
		var result Result
		if e := c.ReadJSON(&result); e != nil {
			return
		}
		_ = c.SetReadDeadline(time.Now().Add(50 * time.Second))
		h.mu.Lock()
		ch := p.pending[result.ID]
		if ch != nil {
			delete(p.pending, result.ID)
		}
		h.mu.Unlock()
		if ch != nil {
			ch <- result
		}
	}
}
func allowedOperation(s string) bool {
	return s == "reminders.sync" || s == "calendar.sync" || s == "categories.list"
}
func (h *Hub) Call(ctx context.Context, operation string, input, output any) error {
	if !allowedOperation(operation) {
		return errors.New("unsupported Mac operation")
	}
	body, e := json.Marshal(input)
	if e != nil {
		return e
	}
	if len(body) > 512*1024 {
		return errors.New("Mac request is too large")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	id := rand.Text()
	ch := make(chan Result, 1)
	h.mu.Lock()
	p := h.client
	if p == nil {
		h.mu.Unlock()
		return errors.New("Mac helper가 연결되어 있지 않습니다. 연결 후 다시 요청하세요.")
	}
	if len(p.pending) > 0 {
		h.mu.Unlock()
		return errors.New("Mac 동기화 작업이 이미 실행 중입니다.")
	}
	p.pending[id] = ch
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(p.pending, id); h.mu.Unlock() }()
	p.write.Lock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	e = p.conn.WriteJSON(Request{ID: id, Operation: operation, Input: body})
	p.write.Unlock()
	if e != nil {
		p.conn.Close()
		return errors.New("Mac connection lost; operation was not retried")
	}
	select {
	case <-ctx.Done():
		p.conn.Close()
		return ctx.Err()
	case <-p.done:
		return errors.New("Mac connection lost; verify Apple state before retrying")
	case result := <-ch:
		if result.Error != "" {
			return errors.New(result.Error)
		}
		if len(result.Output) == 0 {
			return errors.New("Mac returned an empty response")
		}
		return json.Unmarshal(result.Output, output)
	}
}

type Reminders struct{ Hub *Hub }

func (r Reminders) Sync(ctx context.Context, in core.ReminderRequest) (out core.ReminderResult, e error) {
	e = r.Hub.Call(ctx, "reminders.sync", in, &out)
	return
}

type Calendar struct{ Hub *Hub }

func (c Calendar) Sync(ctx context.Context, in core.CalendarRequest) (out core.CalendarResult, e error) {
	e = c.Hub.Call(ctx, "calendar.sync", in, &out)
	return
}

type Categories struct{ Hub *Hub }

func (c Categories) List(ctx context.Context) (out core.AppleCategories, e error) {
	e = c.Hub.Call(ctx, "categories.list", struct{}{}, &out)
	return
}
