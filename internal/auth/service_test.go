package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
)

const testBase = "https://klap.example"
const testBootstrap = "test-bootstrap-token-with-at-least-32-characters"

type authHarness struct {
	s      *Service
	mux    *http.ServeMux
	db     *state.Store
	dir    string
	key    []byte
	cookie *http.Cookie
	csrf   string
}

func newHarness(t *testing.T) *authHarness {
	t.Helper()
	dir := t.TempDir()
	key, _ := state.LoadKey(dir)
	db, err := state.Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(db, testBase, key, testBootstrap)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	h := &authHarness{s: s, mux: mux, db: db, dir: dir, key: key}
	t.Cleanup(func() { h.db.Close() })
	response := h.form("/setup", url.Values{"bootstrap": {testBootstrap}, "password": {"a-long-owner-test-password"}}, false)
	if response.Code != 303 {
		t.Fatalf("setup status %d", response.Code)
	}
	h.cookie = response.Result().Cookies()[0]
	req := httptest.NewRequest("GET", testBase+"/api/session", nil)
	req.AddCookie(h.cookie)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var v struct {
		CSRF string `json:"csrf"`
	}
	json.Unmarshal(rec.Body.Bytes(), &v)
	h.csrf = v.CSRF
	return h
}
func (h *authHarness) form(path string, values url.Values, cookie bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", testBase+path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testBase)
	if cookie {
		req.AddCookie(h.cookie)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}
func (h *authHarness) register(t *testing.T) string {
	t.Helper()
	req := httptest.NewRequest("POST", testBase+"/oauth/register", strings.NewReader(`{"redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"}`))
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	var v struct {
		ID string `json:"client_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &v)
	if rec.Code != 201 || v.ID == "" {
		t.Fatalf("registration: %d", rec.Code)
	}
	return v.ID
}
func (h *authHarness) code(t *testing.T, client, verifier string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {client}, "redirect_uri": {"https://client.example/callback"}, "response_type": {"code"}, "scope": {"klap"}, "state": {"random-state-12345678"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "resource": {testBase + "/mcp"}}
	rec := h.form("/oauth/authorize?"+q.Encode(), url.Values{"csrf": {h.csrf}, "decision": {"allow"}}, true)
	u, _ := url.Parse(rec.Header().Get("Location"))
	if u == nil || u.Query().Get("code") == "" {
		t.Fatalf("authorization failed: %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	return u.Query().Get("code")
}
func (h *authHarness) exchange(client, code, verifier string) *httptest.ResponseRecorder {
	return h.form("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {code}, "redirect_uri": {"https://client.example/callback"}, "code_verifier": {verifier}, "resource": {testBase + "/mcp"}}, false)
}

type tokens struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
}

func decodeTokens(t *testing.T, rec *httptest.ResponseRecorder) tokens {
	t.Helper()
	var v tokens
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || rec.Code != 200 || v.Access == "" || v.Refresh == "" {
		t.Fatalf("token response %d: %s", rec.Code, rec.Body.String())
	}
	return v
}
func (h *authHarness) protected(token string) int {
	req := httptest.NewRequest("POST", testBase+"/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.s.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := FromContext(r.Context())
		if !ok || id.Subject != "owner" {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	})).ServeHTTP(rec, req)
	return rec.Code
}
func TestOAuthPKCERotationPersistenceAndRevocation(t *testing.T) {
	h := newHarness(t)
	if !h.cookie.Secure || !h.cookie.HttpOnly || h.cookie.SameSite != http.SameSiteLaxMode || h.csrf == "" {
		t.Fatal("owner cookie or CSRF protections missing")
	}
	client := h.register(t)
	verifier := strings.Repeat("v", 43)
	code := h.code(t, client, verifier)
	if rec := h.exchange(client, code, strings.Repeat("w", 43)); rec.Code == 200 {
		t.Fatal("wrong PKCE verifier accepted")
	}
	// A failed redemption consumes the PKCE session; restart authorization.
	code = h.code(t, client, verifier)
	token := decodeTokens(t, h.exchange(client, code, verifier))
	if h.protected(token.Access) != 204 {
		t.Fatal("issued token rejected")
	}
	h.db.Close()
	db, err := state.Open(h.dir, h.key)
	if err != nil {
		t.Fatal(err)
	}
	h.db = db
	s, err := New(db, testBase, h.key, "")
	if err != nil {
		t.Fatal(err)
	}
	h.s = s
	h.mux = http.NewServeMux()
	s.Register(h.mux)
	if h.protected(token.Access) != 204 {
		t.Fatal("restart lost authorization")
	}
	refresh := func(value string) *httptest.ResponseRecorder {
		return h.form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "refresh_token": {value}, "resource": {testBase + "/mcp"}}, false)
	}
	rotated := decodeTokens(t, refresh(token.Refresh))
	if rotated.Refresh == token.Refresh || h.protected(rotated.Access) != 204 {
		t.Fatal("refresh rotation failed")
	}
	if h.protected(token.Access) == 204 {
		t.Fatal("old access token survived rotation")
	}
	if rec := refresh(token.Refresh); rec.Code == 200 {
		t.Fatal("refresh token replay accepted")
	}
	if h.protected(rotated.Access) == 204 || s.Allowed(context.Background(), "owner:"+client) {
		t.Fatal("replay did not revoke grant and event access")
	}
}
func TestCodeRedemptionIsAtomic(t *testing.T) {
	h := newHarness(t)
	client := h.register(t)
	v := strings.Repeat("x", 43)
	code := h.code(t, client, v)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); statuses <- h.exchange(client, code, v).Code }()
	}
	wg.Wait()
	close(statuses)
	success := 0
	for code := range statuses {
		if code == 200 {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("code redeemed %d times", success)
	}
}
func TestOwnerCSRFRedirectAndRegistrationBoundaries(t *testing.T) {
	h := newHarness(t)
	if r := h.form("/setup", url.Values{"bootstrap": {testBootstrap}, "password": {"another-owner-password"}}, false); r.Code != 403 {
		t.Fatal("owner overwritten")
	}
	req := httptest.NewRequest("POST", testBase+"/settings", nil)
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("X-CSRF-Token", h.csrf)
	rec := httptest.NewRecorder()
	h.s.Owner(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("cross-origin owner action accepted")
	}
	for _, v := range []string{"https://evil.example", "//evil.example", "/\\evil.example", "/login?next=https://evil.example"} {
		if safeNext(v) != "/" {
			t.Errorf("unsafe redirect %s", v)
		}
	}
	for _, v := range []string{"javascript:alert(1)", "http://private.example/callback", "https://u:p@example.com", "https://example.com/#fragment"} {
		if validRedirect(v) {
			t.Errorf("invalid redirect accepted %s", v)
		}
	}
	client := h.register(t)
	v := strings.Repeat("z", 43)
	code := h.code(t, client, v)
	rec = h.form("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {code}, "code_verifier": {v}, "resource": {"https://another.example/mcp"}}, false)
	if rec.Code != 400 {
		t.Fatal("wrong resource accepted")
	}
	if h.protected("unknown") != 401 {
		t.Fatal("unknown bearer accepted")
	}
}

func TestConsentPolicyAllowsOnlyRegisteredCallbackOrigin(t *testing.T) {
	h := newHarness(t)
	client := h.register(t)
	digest := sha256.Sum256([]byte(strings.Repeat("v", 43)))
	q := url.Values{"client_id": {client}, "redirect_uri": {"https://client.example/callback"}, "response_type": {"code"}, "scope": {"klap"}, "state": {"random-state-12345678"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(digest[:])}, "code_challenge_method": {"S256"}, "resource": {testBase + "/mcp"}}
	consent := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, testBase+"/oauth/authorize?"+q.Encode(), nil)
		req.AddCookie(h.cookie)
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, req)
		return rec
	}
	rec := consent()
	want := strings.Replace(pagePolicy, "form-action 'self'", "form-action 'self' https://client.example", 1)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Security-Policy") != want {
		t.Fatalf("consent policy: status=%d policy=%q", rec.Code, rec.Header().Get("Content-Security-Policy"))
	}
	if rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Body.String(), `name="csrf"`) {
		t.Fatal("consent cache or CSRF protections missing")
	}
	q.Set("redirect_uri", "https://evil.example/callback")
	rec = consent()
	if rec.Code == http.StatusOK || strings.Contains(rec.Header().Get("Content-Security-Policy"), "evil.example") || strings.Contains(rec.Header().Get("Location"), "evil.example") {
		t.Fatal("unregistered callback influenced the consent policy or redirect")
	}
	for _, decision := range []string{"allow", "deny"} {
		q.Set("redirect_uri", "https://client.example/callback")
		rec = h.form("/oauth/authorize?"+q.Encode(), url.Values{"csrf": {h.csrf}, "decision": {decision}}, true)
		u, err := url.Parse(rec.Header().Get("Location"))
		if err != nil || rec.Code != http.StatusSeeOther || u.Host != "client.example" {
			t.Fatalf("%s callback failed: status=%d", decision, rec.Code)
		}
		if decision == "allow" && u.Query().Get("code") == "" || decision == "deny" && u.Query().Get("error") != "access_denied" {
			t.Fatalf("wrong %s callback response", decision)
		}
	}
	login := httptest.NewRecorder()
	h.mux.ServeHTTP(login, httptest.NewRequest(http.MethodGet, testBase+"/login", nil))
	if login.Header().Get("Content-Security-Policy") != pagePolicy {
		t.Fatal("login form policy was widened")
	}
}

func TestConsentCallbackOrigin(t *testing.T) {
	for _, tc := range []struct{ uri, want string }{
		{"https://chatgpt.com/connector/oauth/test?foo=bar", "https://chatgpt.com"},
		{"https://client.example:8443/callback;query?next=https://other.example", "https://client.example:8443"},
		{"http://localhost:8080/callback", "http://localhost:8080"},
		{"http://127.0.0.1:8080/callback", "http://127.0.0.1:8080"},
		{"http://[::1]:8080/callback", "http://[::1]:8080"},
		{"https://*.example/callback", ""},
		{"https://client.example;script-src/callback", ""},
		{"https://u:p@client.example/callback", ""},
		{"https://client.example/callback#fragment", ""},
		{"http://client.example/callback", ""},
		{"//client.example/callback", ""},
		{"javascript:alert(1)", ""},
	} {
		t.Run(tc.uri, func(t *testing.T) {
			u, err := url.Parse(tc.uri)
			if err != nil {
				t.Fatal(err)
			}
			if got := consentCallbackOrigin(u); got != tc.want {
				t.Fatalf("origin = %q, want %q", got, tc.want)
			}
		})
	}
	if got := consentCallbackOrigin(nil); got != "" {
		t.Fatalf("nil callback origin = %q", got)
	}
}
