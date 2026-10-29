package server

import (
	"context"
	"encoding/json"
	"github.com/leehyowon14/KLAP-for-GPT/internal/klap"
	"strings"
	"testing"
)

type agentBackend struct {
	fakeBackend
	calls int
}

func (b *agentBackend) Read(ctx context.Context, kind string, q klap.Query) (any, error) {
	b.calls++
	return map[string]any{"query": map[string]any{"intent": q.Intent}, "items": []any{map[string]any{"id": "a", "title": strings.Repeat("한글", 12000)}}}, nil
}
func TestAgentToolContractAndContinuation(t *testing.T) {
	a, _, _ := testApp(t)
	backend := &agentBackend{}
	a.Backend = backend
	h := a.MCP()
	for _, args := range []any{map[string]any{"intent": "overview", "limit": 3}, map[string]any{"intent": "overview", "course_id": "a"}, map[string]any{"intent": "overview", "include_expired": nil}} {
		r := rpcStatus(t, h, "tools/call", map[string]any{"name": "klap_lectures", "arguments": args}, 400)
		if _, ok := r["error"]; !ok {
			result := r["result"].(map[string]any)
			if result["isError"] != true {
				t.Fatalf("accepted %v", args)
			}
		}
	}
	r := rpc(t, h, "tools/call", map[string]any{"name": "klap_lectures", "arguments": map[string]any{"intent": "overview"}})
	result := r["result"].(map[string]any)
	structured := result["structuredContent"].(map[string]any)
	if structured["schema_version"] != float64(2) {
		t.Fatalf("%v", r)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if len(text) > 500 {
		t.Fatal("duplicated JSON")
	}
	data := structured["data"].(map[string]any)
	cursor := data["page"].(map[string]any)["next_cursor"].(string)
	args, _ := json.Marshal(map[string]any{"cursor": cursor, "intent": "overview"})
	if _, e := a.Call(context.Background(), "klap_lectures", args, "owner:client"); e == nil {
		t.Fatal("mixed cursor accepted")
	}
	r = rpc(t, h, "tools/call", map[string]any{"name": "klap_lectures", "arguments": map[string]any{"cursor": cursor}})
	if _, ok := r["error"]; ok || backend.calls != 1 {
		t.Fatalf("refetched: %v calls %d", r, backend.calls)
	}
}
