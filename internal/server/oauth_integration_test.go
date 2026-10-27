package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/leehyowon14/KLAP-for-GPT/internal/auth"
)

func TestOwnerConsentToAuthenticatedMCPAndRevocation(t *testing.T) {
	app, db, key := testApp(t)
	bootstrap := strings.Repeat("b", 40)
	authentication, e := auth.New(db, app.PublicURL, key, bootstrap)
	if e != nil {
		t.Fatal(e)
	}
	handler := app.Handler(authentication, fakeBackend{})
	var cookie *http.Cookie
	form := func(path string, values url.Values, owner bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", app.PublicURL+path, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", app.PublicURL)
		if owner {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	setup := form("/setup", url.Values{"bootstrap": {bootstrap}, "password": {"a-password-for-isolated-integration-test"}}, false)
	if setup.Code != 303 || len(setup.Result().Cookies()) != 1 {
		t.Fatal("owner setup failed", setup.Code)
	}
	cookie = setup.Result().Cookies()[0]
	req := httptest.NewRequest("GET", app.PublicURL+"/api/session", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var session struct {
		CSRF string `json:"csrf"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &session)
	if session.CSRF == "" {
		t.Fatal("missing owner CSRF")
	}
	req = httptest.NewRequest("POST", app.PublicURL+"/oauth/register", strings.NewReader(`{"redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var client struct {
		ID string `json:"client_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &client)
	if rec.Code != 201 || client.ID == "" {
		t.Fatal("DCR failed")
	}
	verifier := strings.Repeat("v", 43)
	challenge := sha256.Sum256([]byte(verifier))
	params := url.Values{"client_id": {client.ID}, "redirect_uri": {"https://client.example/callback"}, "response_type": {"code"}, "scope": {"klap"}, "state": {"state-with-enough-entropy"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}, "resource": {app.PublicURL + "/mcp"}}
	consent := form("/oauth/authorize?"+params.Encode(), url.Values{"csrf": {session.CSRF}, "decision": {"allow"}}, true)
	location, _ := url.Parse(consent.Header().Get("Location"))
	if location == nil || location.Query().Get("code") == "" {
		t.Fatalf("consent failed: %d", consent.Code)
	}
	tokens := form("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {client.ID}, "code": {location.Query().Get("code")}, "code_verifier": {verifier}, "redirect_uri": {"https://client.example/callback"}, "resource": {app.PublicURL + "/mcp"}}, false)
	var access struct {
		Token string `json:"access_token"`
	}
	_ = json.Unmarshal(tokens.Body.Bytes(), &access)
	if tokens.Code != 200 || access.Token == "" {
		t.Fatal("token exchange failed", tokens.Code)
	}
	// The protocol harness supplies only a token here; erase its test identity so
	// the production OAuth middleware must establish the authenticated context.
	authenticated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(t.Context())
		r.Header.Set("Authorization", "Bearer "+access.Token)
		handler.ServeHTTP(w, r)
	})
	result := rpc(t, authenticated, "tools/call", map[string]any{"name": "klap_open", "arguments": map[string]any{}})
	value, ok := result["result"].(map[string]any)
	if !ok || value["isError"] == true || value["structuredContent"] == nil {
		t.Fatal("OAuth-to-MCP roundtrip failed", result)
	}
	revoked := form("/oauth/revoke", url.Values{"token": {access.Token}, "token_type_hint": {"access_token"}, "client_id": {client.ID}}, false)
	if revoked.Code != 200 {
		t.Fatal("revoke failed")
	}
	req = httptest.NewRequest("POST", app.PublicURL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+access.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("revoked token still calls MCP", rec.Code)
	}
}

func TestOwnerFormOriginBehindHTTPSProxy(t *testing.T) {
	app, db, key := testApp(t)
	authentication, err := auth.New(db, app.PublicURL, key, strings.Repeat("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Handler(authentication, fakeBackend{})
	public, _ := url.Parse(app.PublicURL)
	proxyRequest := func(method, path, body string) *http.Request {
		// nginx terminates HTTPS and forwards HTTP with the public Host.
		req := httptest.NewRequest(method, "http://127.0.0.1:3030"+path, strings.NewReader(body))
		req.Host = public.Host
		req.Header.Set("X-Forwarded-Proto", "https")
		return req
	}
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, proxyRequest("GET", "/login", ""))
	if page.Code != http.StatusOK || page.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("auth form must retain its same-origin POST Origin: status=%d policy=%q", page.Code, page.Header().Get("Referrer-Policy"))
	}
	for _, tc := range []struct {
		name, origin string
		want         int
	}{
		{"public HTTPS origin", app.PublicURL, http.StatusBadRequest},
		{"opaque origin", "null", http.StatusForbidden},
		{"missing origin", "", http.StatusForbidden},
		{"unrelated origin", "https://evil.example", http.StatusForbidden},
		{"sibling host", "https://other." + public.Host, http.StatusForbidden},
		{"HTTP downgrade", "http://" + public.Host, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A short password cannot create an owner. HTTP 400 here proves the
			// legitimate request reached form validation instead of Origin denial.
			req := proxyRequest("POST", "/setup", url.Values{"password": {"short"}}.Encode())
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
