package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	db             *state.Store
	store          *oauthStore
	provider       fosite.OAuth2Provider
	base, resource string
	secure         bool
	rateMu         sync.Mutex
	rateStart      time.Time
	attempts       int
}
type owner struct {
	Password      []byte
	BootstrapHash string
}
type ownerSession struct {
	CSRF    string
	Expires time.Time
}
type identityKey struct{}
type Identity struct{ Subject, ClientID, Principal string }

func New(db *state.Store, base string, key []byte, bootstrap string) (*Service, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return nil, errors.New("public URL must contain only scheme and host")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")) {
		return nil, errors.New("public URL requires HTTPS (except loopback development)")
	}
	if len(key) != 32 {
		return nil, errors.New("OAuth secret must be 32 bytes")
	}
	s := &Service{db: db, store: &oauthStore{db: db}, base: u.String(), resource: u.String() + "/mcp", secure: u.Scheme == "https"}
	config := &fosite.Config{GlobalSecret: key, EnforcePKCE: true, EnforcePKCEForPublicClients: true, EnablePKCEPlainChallengeMethod: false, AccessTokenLifespan: time.Hour, AuthorizeCodeLifespan: 5 * time.Minute, RefreshTokenLifespan: 30 * 24 * time.Hour, RefreshTokenScopes: []string{}, ScopeStrategy: fosite.ExactScopeStrategy, SendDebugMessagesToClients: false}
	s.provider = compose.Compose(config, s.store, compose.NewOAuth2HMACStrategy(config), compose.OAuth2AuthorizeExplicitFactory, compose.OAuth2RefreshTokenGrantFactory, compose.OAuth2TokenIntrospectionFactory, compose.OAuth2TokenRevocationFactory, compose.OAuth2PKCEFactory)
	err = db.Update(func(r state.Records) error {
		if _, e := state.Get[owner](r, "owner"); e == nil {
			return nil
		} else if !errors.Is(e, state.ErrNotFound) {
			return e
		}
		if len(bootstrap) < 32 {
			return errors.New("a bootstrap token of at least 32 characters is required for first setup")
		}
		return state.Put(r, "owner", owner{BootstrapHash: hash(bootstrap)})
	})
	return s, err
}
func hash(v string) string   { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func FromContext(ctx context.Context) (Identity, bool) {
	v, ok := ctx.Value(identityKey{}).(Identity)
	return v, ok
}

// WithIdentity is for authenticated transport adapters, never request parameters.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}
func (s *Service) limit() bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := time.Now()
	if now.Sub(s.rateStart) > time.Minute {
		s.rateStart = now
		s.attempts = 0
	}
	s.attempts++
	return s.attempts <= 30
}
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", s.metadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", s.protectedMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", s.protectedMetadata)
	mux.HandleFunc("POST /oauth/register", s.registerClient)
	mux.HandleFunc("GET /oauth/authorize", s.authorize)
	mux.HandleFunc("POST /oauth/authorize", s.authorize)
	mux.HandleFunc("POST /oauth/token", s.token)
	mux.HandleFunc("POST /oauth/revoke", s.revoke)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /setup", s.setup)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /api/session", s.sessionStatus)
}
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Service) metadata(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, map[string]any{"issuer": s.base, "authorization_endpoint": s.base + "/oauth/authorize", "token_endpoint": s.base + "/oauth/token", "registration_endpoint": s.base + "/oauth/register", "revocation_endpoint": s.base + "/oauth/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{"klap"}})
}
func (s *Service) protectedMetadata(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, map[string]any{"resource": s.resource, "authorization_servers": []string{s.base}, "scopes_supported": []string{"klap"}, "bearer_methods_supported": []string{"header"}})
}
func validRedirect(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}
func (s *Service) registerClient(w http.ResponseWriter, r *http.Request) {
	if !s.limit() {
		http.Error(w, "try again later", 429)
		return
	}
	var in struct {
		RedirectURIs  []string `json:"redirect_uris"`
		Name          string   `json:"client_name"`
		Auth          string   `json:"token_endpoint_auth_method"`
		GrantTypes    []string `json:"grant_types"`
		ResponseTypes []string `json:"response_types"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in) != nil || len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 8 || (in.Auth != "" && in.Auth != "none") {
		jsonResponse(w, 400, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	for _, v := range in.RedirectURIs {
		if len(v) > 2048 || !validRedirect(v) {
			jsonResponse(w, 400, map[string]string{"error": "invalid_redirect_uri"})
			return
		}
	}
	for _, v := range in.GrantTypes {
		if v != "authorization_code" && v != "refresh_token" {
			jsonResponse(w, 400, map[string]string{"error": "invalid_client_metadata"})
			return
		}
	}
	for _, v := range in.ResponseTypes {
		if v != "code" {
			jsonResponse(w, 400, map[string]string{"error": "invalid_client_metadata"})
			return
		}
	}
	c := fosite.DefaultClient{ID: rand.Text(), Public: true, RedirectURIs: in.RedirectURIs, GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"}, Scopes: []string{"klap"}, Audience: []string{s.resource}}
	err := s.db.Update(func(records state.Records) error {
		count := 0
		for k := range records {
			if strings.HasPrefix(k, "oauth/client/") {
				count++
			}
		}
		if count >= 64 {
			return errors.New("client registration limit reached")
		}
		return state.Put(records, "oauth/client/"+c.ID, c)
	})
	if err != nil {
		http.Error(w, "client registration unavailable", 503)
		return
	}
	jsonResponse(w, 201, map[string]any{"client_id": c.ID, "client_id_issued_at": time.Now().Unix(), "redirect_uris": c.RedirectURIs, "grant_types": c.GrantTypes, "response_types": c.ResponseTypes, "token_endpoint_auth_method": "none"})
}

func (s *Service) authorize(w http.ResponseWriter, r *http.Request) {
	session, ok := s.ownerSession(r)
	if !ok {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return
	}
	var req fosite.AuthorizeRequester
	var resp fosite.AuthorizeResponder
	var oauthErr error
	run := func(records state.Records) error {
		ctx := context.WithValue(r.Context(), transactionKey{}, records)
		req, oauthErr = s.provider.NewAuthorizeRequest(ctx, r)
		if oauthErr != nil {
			return nil
		}
		if resource := req.GetRequestForm().Get("resource"); resource != "" && resource != s.resource {
			oauthErr = fosite.ErrInvalidRequest
			return nil
		}
		if r.Method == http.MethodGet {
			return nil
		}
		if !s.sameOrigin(r) || !equal(r.PostFormValue("csrf"), session.CSRF) {
			oauthErr = fosite.ErrAccessDenied
			return nil
		}
		if r.PostFormValue("decision") != "allow" {
			oauthErr = fosite.ErrAccessDenied
			return nil
		}
		for _, scope := range req.GetRequestedScopes() {
			if scope != "klap" {
				oauthErr = fosite.ErrInvalidScope
				return nil
			}
			req.GrantScope(scope)
		}
		req.GrantScope("klap")
		req.GrantAudience(s.resource)
		resp, oauthErr = s.provider.NewAuthorizeResponse(ctx, req, &fosite.DefaultSession{Subject: "owner"})
		if oauthErr == nil {
			return state.Put(records, "oauth/grant/"+req.GetClient().GetID(), time.Now().Add(30*24*time.Hour))
		}
		return nil
	}
	var err error
	if r.Method == http.MethodGet {
		err = s.db.View(run)
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		err = s.db.Update(run)
	}
	if err != nil {
		http.Error(w, "authorization storage unavailable", 503)
		return
	}
	if oauthErr != nil {
		s.provider.WriteAuthorizeError(r.Context(), w, req, oauthErr)
		return
	}
	if r.Method == http.MethodGet {
		render(w, consentPage, map[string]string{"CSRF": session.CSRF, "Client": req.GetClient().GetID(), "Redirect": req.GetRedirectURI().String(), "Action": r.URL.RequestURI()})
		return
	}
	s.provider.WriteAuthorizeResponse(r.Context(), w, req, resp)
}
func (s *Service) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if resource := r.PostForm.Get("resource"); resource != "" && resource != s.resource {
		jsonResponse(w, 400, map[string]string{"error": "invalid_target"})
		return
	}
	var req fosite.AccessRequester
	var resp fosite.AccessResponder
	var oauthErr error
	err := s.db.Update(func(records state.Records) error {
		ctx := context.WithValue(r.Context(), transactionKey{}, records)
		req, oauthErr = s.provider.NewAccessRequest(ctx, r, &fosite.DefaultSession{})
		if oauthErr == nil {
			resp, oauthErr = s.provider.NewAccessResponse(ctx, req)
		}
		return nil
	})
	if err != nil {
		http.Error(w, "token storage unavailable", 503)
		return
	}
	if oauthErr != nil {
		s.provider.WriteAccessError(r.Context(), w, req, oauthErr)
		return
	}
	s.provider.WriteAccessResponse(r.Context(), w, req, resp)
}
func (s *Service) revoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var oauthErr error
	err := s.db.Update(func(records state.Records) error {
		oauthErr = s.provider.NewRevocationRequest(context.WithValue(r.Context(), transactionKey{}, records), r)
		return nil
	})
	if err != nil {
		http.Error(w, "revocation storage unavailable", 503)
		return
	}
	s.provider.WriteRevocationResponse(r.Context(), w, oauthErr)
}
func (s *Service) Allowed(ctx context.Context, principal string) bool {
	if !strings.HasPrefix(principal, "owner:") {
		return false
	}
	id := strings.TrimPrefix(principal, "owner:")
	allowed := false
	_ = s.db.View(func(r state.Records) error {
		expiry, e := state.Get[time.Time](r, "oauth/grant/"+id)
		allowed = e == nil && time.Now().Before(expiry)
		return nil
	})
	return allowed
}
func (s *Service) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || token == r.Header.Get("Authorization") {
			s.unauthorized(w)
			return
		}
		var requester fosite.AccessRequester
		var authErr error
		err := s.db.View(func(records state.Records) error {
			_, requester, authErr = s.provider.IntrospectToken(context.WithValue(r.Context(), transactionKey{}, records), token, fosite.AccessToken, &fosite.DefaultSession{}, "klap")
			return nil
		})
		if err != nil || authErr != nil || requester == nil || requester.GetSession().GetSubject() != "owner" || !requester.GetGrantedAudience().Has(s.resource) {
			s.unauthorized(w)
			return
		}
		id := Identity{Subject: "owner", ClientID: requester.GetClient().GetID(), Principal: "owner:" + requester.GetClient().GetID()}
		if !s.Allowed(r.Context(), id.Principal) {
			s.unauthorized(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}
func (s *Service) unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource/mcp", scope="klap"`, s.base))
	http.Error(w, "connect KLAP with OAuth", 401)
}

func (s *Service) sameOrigin(r *http.Request) bool { return r.Header.Get("Origin") == s.base }
func (s *Service) cookieName() string {
	if s.secure {
		return "__Host-klap_owner"
	}
	return "klap_owner"
}
func (s *Service) ownerSession(r *http.Request) (ownerSession, bool) {
	c, e := r.Cookie(s.cookieName())
	if e != nil || len(c.Value) < 24 {
		return ownerSession{}, false
	}
	var session ownerSession
	e = s.db.View(func(records state.Records) error {
		var err error
		session, err = state.Get[ownerSession](records, "owner/session/"+hash(c.Value))
		return err
	})
	return session, e == nil && time.Now().Before(session.Expires)
}
func (s *Service) Owner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.ownerSession(r)
		if !ok {
			http.Error(w, "sign in to this instance", 401)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && (!s.sameOrigin(r) || !equal(r.Header.Get("X-CSRF-Token"), session.CSRF)) {
			http.Error(w, "invalid origin or CSRF token", 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Service) sessionStatus(w http.ResponseWriter, r *http.Request) {
	session, ok := s.ownerSession(r)
	if !ok {
		jsonResponse(w, 200, map[string]any{"signed_in": false})
		return
	}
	jsonResponse(w, 200, map[string]any{"signed_in": true, "csrf": session.CSRF})
}
func (s *Service) loginPage(w http.ResponseWriter, r *http.Request) {
	var own owner
	_ = s.db.View(func(records state.Records) error { var e error; own, e = state.Get[owner](records, "owner"); return e })
	render(w, loginPage, map[string]any{"Setup": len(own.Password) == 0, "Next": safeNext(r.URL.Query().Get("next"))})
}
func safeNext(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || u.IsAbs() || u.Host != "" || u.Path != "/oauth/authorize" {
		return "/"
	}
	return u.RequestURI()
}
func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		http.Error(w, "invalid origin", 403)
		return
	}
	if !s.limit() {
		http.Error(w, "try again later", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var own owner
	err := s.db.View(func(records state.Records) error { var e error; own, e = state.Get[owner](records, "owner"); return e })
	if err != nil || bcrypt.CompareHashAndPassword(own.Password, []byte(r.PostFormValue("password"))) != nil {
		http.Error(w, "로그인 정보를 확인해 주세요.", 401)
		return
	}
	s.createOwnerSession(w, r, safeNext(r.PostFormValue("next")))
}
func (s *Service) setup(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		http.Error(w, "invalid origin", 403)
		return
	}
	if !s.limit() {
		http.Error(w, "try again later", 429)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	password := r.PostFormValue("password")
	if len(password) < 12 || len(password) > 72 {
		http.Error(w, "암호는 12–72바이트로 입력해 주세요.", 400)
		return
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "password setup failed", 400)
		return
	}
	err = s.db.Update(func(records state.Records) error {
		own, e := state.Get[owner](records, "owner")
		if e != nil {
			return e
		}
		if len(own.Password) > 0 || !equal(own.BootstrapHash, hash(r.PostFormValue("bootstrap"))) {
			return errors.New("invalid bootstrap")
		}
		return state.Put(records, "owner", owner{Password: digest})
	})
	if err != nil {
		http.Error(w, "설치 토큰을 확인해 주세요. 이미 설정한 인스턴스는 로그인하세요.", 403)
		return
	}
	s.createOwnerSession(w, r, "/")
}
func (s *Service) createOwnerSession(w http.ResponseWriter, r *http.Request, next string) {
	token := rand.Text()
	session := ownerSession{CSRF: rand.Text(), Expires: time.Now().Add(8 * time.Hour)}
	err := s.db.Update(func(records state.Records) error {
		for key := range records {
			if strings.HasPrefix(key, "owner/session/") {
				v, e := state.Get[ownerSession](records, key)
				if e == nil && time.Now().After(v.Expires) {
					delete(records, key)
				}
			}
		}
		return state.Put(records, "owner/session/"+hash(token), session)
	})
	if err != nil {
		http.Error(w, "session storage unavailable", 503)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: token, Path: "/", Secure: s.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 8 * 3600})
	http.Redirect(w, r, next, 303)
}
func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	session, ok := s.ownerSession(r)
	if !ok || !s.sameOrigin(r) || !equal(r.Header.Get("X-CSRF-Token"), session.CSRF) {
		http.Error(w, "invalid session", 403)
		return
	}
	c, _ := r.Cookie(s.cookieName())
	if err := s.db.Update(func(records state.Records) error { delete(records, "owner/session/"+hash(c.Value)); return nil }); err != nil {
		http.Error(w, "storage unavailable", 503)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Path: "/", Secure: s.secure, HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteLaxMode})
	jsonResponse(w, 200, map[string]bool{"signed_in": false})
}

func render(w http.ResponseWriter, source string, v any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	_ = template.Must(template.New("page").Parse(source)).Execute(w, v)
}
