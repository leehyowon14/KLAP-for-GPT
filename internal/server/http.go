package server

import (
	"context"
	"encoding/json"
	"github.com/leehyowon14/KLAP-for-GPT/internal/auth"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type AccountController interface {
	Connect(context.Context, string, string) error
	Disconnect(context.Context) error
}

func (a *App) Handler(authentication *auth.Service, accounts AccountController) http.Handler {
	mux := http.NewServeMux()
	authentication.Register(mux)
	if a.Helper != nil {
		a.Helper.Register(mux, authentication.Owner)
	}
	mux.Handle("/mcp", authentication.Protect(a.MCP()))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.Handle("POST /api/call", authentication.Owner(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&input); e != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		value, e := a.Call(ctx, input.Name, input.Arguments, "owner")
		if e != nil {
			respond(w, 400, map[string]string{"error": e.Error()})
			return
		}
		respond(w, 200, map[string]any{"kind": input.Name, "data": value, "retrieved_at": time.Now().UTC()})
	})))
	mux.Handle("POST /api/klas/connect", authentication.Owner(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			StudentID string `json:"student_id"`
			Password  string `json:"password"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if e := d.Decode(&input); e != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		if e := accounts.Connect(ctx, strings.TrimSpace(input.StudentID), input.Password); e != nil {
			respond(w, 400, map[string]string{"error": e.Error()})
			return
		}
		respond(w, 200, map[string]bool{"connected": true})
	})))
	mux.Handle("POST /api/klas/disconnect", authentication.Owner(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queue, e := a.Jobs.List("owner")
		if e != nil {
			http.Error(w, "job state unavailable", 503)
			return
		}
		for _, job := range queue {
			if job.Status == "queued" || job.Status == "running" {
				if _, e = a.Jobs.Cancel(job.ID, "owner"); e != nil {
					http.Error(w, "could not stop attendance", 503)
					return
				}
			}
		}
		if e = accounts.Disconnect(r.Context()); e != nil {
			respond(w, 400, map[string]string{"error": e.Error()})
			return
		}
		respond(w, 200, map[string]bool{"connected": false})
	})))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		// The compiled application is self contained. It never needs an external
		// origin for scripts, fonts, or school account credentials.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		_, _ = w.Write([]byte(a.HTML))
	})
	// Explicit host checking replaces the SDK's loopback heuristic so a local
	// reverse proxy can safely forward the configured public hostname.
	public, _ := url.Parse(a.PublicURL)
	protected := http.NewCrossOriginProtection().Handler(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// no-referrer turns HTML form POST origins into "null" in browsers,
		// conflicting with the owner setup/login/consent origin checks. Keep
		// same-origin form metadata while omitting referrers to other origins.
		w.Header().Set("Referrer-Policy", "same-origin")
		if r.URL.Path != "/health" && !strings.EqualFold(r.Host, public.Host) {
			http.Error(w, "invalid host", 403)
			return
		}
		protected.ServeHTTP(w, r)
	})
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
