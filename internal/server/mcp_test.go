package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/auth"
	"github.com/leehyowon14/KLAP-for-GPT/internal/events"
	"github.com/leehyowon14/KLAP-for-GPT/internal/jobs"
	"github.com/leehyowon14/KLAP-for-GPT/internal/klap"
	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
)

type fakeBackend struct{}

func (fakeBackend) Account(context.Context) (string, error) { return "account", nil }
func (fakeBackend) Read(context.Context, string, klap.Query) (any, error) {
	return []map[string]string{{"ID": "stable-id", "Title": "fixture"}}, nil
}
func (fakeBackend) Sync(context.Context, map[string]core.SyncDecision) (any, error) {
	return nil, errors.New("disabled")
}
func (fakeBackend) Content(context.Context, string, events.Arguments) ([]core.ContentItem, error) {
	return nil, nil
}
func (fakeBackend) Attend(context.Context, string, string, func(jobs.Progress)) (bool, error) {
	return false, errors.New("not requested")
}
func (fakeBackend) Connect(context.Context, string, string) error { return nil }
func (fakeBackend) Disconnect(context.Context) error              { return nil }

type rejectSender struct{}

func (rejectSender) Verify(context.Context, string, string, string) error {
	return &events.CallbackError{Reason: "challenge_failed"}
}
func (rejectSender) Send(context.Context, string, string, []string, string, []byte) (int, error) {
	return 0, errors.New("disabled")
}
func testApp(t *testing.T) (*App, *state.Store, []byte) {
	t.Helper()
	dir := t.TempDir()
	key, _ := state.LoadKey(dir)
	db, e := state.Open(dir, key)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	backend := fakeBackend{}
	queue, e := jobs.New(db, backend)
	if e != nil {
		t.Fatal(e)
	}
	return &App{Backend: backend, Jobs: queue, Events: events.New(db, backend, rejectSender{}), PublicURL: "https://klap.example", Mode: "server", HTML: "<!doctype html><html lang=ko><title>KLAP</title></html>"}, db, key
}
func rpc(t *testing.T, h http.Handler, method string, params any) map[string]any {
	t.Helper()
	return rpcStatus(t, h, method, params, http.StatusOK)
}
func rpcStatus(t *testing.T, h http.Handler, method string, params any, expectedStatus int) map[string]any {
	t.Helper()
	params.(map[string]any)["_meta"] = map[string]any{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": map[string]any{}, "io.modelcontextprotocol/clientInfo": map[string]string{"name": "klap-tests", "version": "1"}}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req := httptest.NewRequest("POST", "https://klap.example/mcp", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", method)
	if method == "tools/call" {
		req.Header.Set("Mcp-Name", params.(map[string]any)["name"].(string))
	}
	if method == "resources/read" {
		req.Header.Set("Mcp-Name", params.(map[string]any)["uri"].(string))
	}
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{Subject: "owner", ClientID: "client", Principal: "owner:client"}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var result map[string]any
	if rec.Code != expectedStatus || json.Unmarshal(rec.Body.Bytes(), &result) != nil {
		t.Fatalf("%s: HTTP %d %s", method, rec.Code, rec.Body.String())
	}
	return result
}
func TestMCP2DiscoveryToolsAndEvents(t *testing.T) {
	app, _, _ := testApp(t)
	handler := app.MCP()
	discovery := rpc(t, handler, "server/discover", map[string]any{})
	result, ok := discovery["result"].(map[string]any)
	if !ok {
		t.Fatalf("discovery: %v", discovery)
	}
	if result["resultType"] != "complete" {
		t.Fatalf("incomplete discovery %v", result)
	}
	caps := result["capabilities"].(map[string]any)
	if _, ok := caps["events"]; !ok {
		t.Fatal("events capability missing")
	}
	listed := rpc(t, handler, "tools/list", map[string]any{})
	tools := listed["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 15 {
		t.Fatal("missing tools")
	}
	for _, item := range tools {
		tool := item.(map[string]any)
		if tool["name"] == "klap_sync_apple" {
			t.Fatal("server-only advertised Apple")
		}
		if tool["name"] == "klap_open" {
			meta := tool["_meta"].(map[string]any)
			if meta["ui"].(map[string]any)["resourceUri"] != uiResourceURI(app.HTML) {
				t.Fatal("UI resource missing")
			}
		}
	}
	listed = rpc(t, handler, "events/list", map[string]any{})
	if len(listed["result"].(map[string]any)["events"].([]any)) != 3 {
		t.Fatal("missing event definitions")
	}
	output := rpc(t, handler, "tools/call", map[string]any{"name": "klap_open", "arguments": map[string]any{}})
	if output["error"] != nil {
		t.Fatal(output)
	}
	output = rpcStatus(t, handler, "tools/call", map[string]any{"name": "klap_due", "arguments": map[string]any{"days": 1000}}, http.StatusBadRequest)
	if output["error"] == nil {
		t.Fatal("schema range not enforced")
	}
	output = rpc(t, handler, "resources/read", map[string]any{"uri": uiResourceURI(app.HTML)})
	contents := output["result"].(map[string]any)["contents"].([]any)
	if !strings.Contains(contents[0].(map[string]any)["text"].(string), "KLAP") {
		t.Fatal("UI resource empty")
	}
}
func TestMCPCallbackErrorAndTransportAuthentication(t *testing.T) {
	app, db, key := testApp(t)
	response := rpc(t, app.MCP(), "events/subscribe", map[string]any{"name": "notice.created", "arguments": map[string]any{"term": "2026-2"}, "delivery": map[string]any{"mode": "webhook", "url": "https://example.com/hook", "secret": "whsec_MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE="}})
	e, ok := response["error"].(map[string]any)
	if !ok || e["code"] != float64(-32015) {
		t.Fatalf("callback error: %v", response)
	}
	authentication, err := auth.New(db, app.PublicURL, key, strings.Repeat("b", 40))
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Handler(authentication, fakeBackend{})
	req := httptest.NewRequest("POST", app.PublicURL+"/mcp", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 401 || !strings.Contains(rec.Header().Get("WWW-Authenticate"), "oauth-protected-resource") {
		t.Fatal("MCP missing OAuth challenge")
	}
	req = httptest.NewRequest("GET", "https://evil.example/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("unconfigured host accepted")
	}
}

func TestUIResourceURITracksBundleChanges(t *testing.T) {
	original := uiResourceURI("<html>KLAP</html>")
	if original != uiResourceURI("<html>KLAP</html>") {
		t.Fatal("identical bundles must share the URI")
	}
	for _, changed := range []string{"", "<html>KLAP</html> ", "<html>KLAP<style>input{outline:none}</style></html>"} {
		if original == uiResourceURI(changed) {
			t.Fatal("changed bundles must invalidate the host cache")
		}
	}
}
