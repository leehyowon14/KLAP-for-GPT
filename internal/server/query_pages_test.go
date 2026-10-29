package server

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQueryPagesPreserveUTF8AndPrincipal(t *testing.T) {
	p := &queryPages{}
	original := map[string]any{"id": "lecture:A", "content": strings.Repeat("한글😀", 8000)}
	result, e := p.create(map[string]any{"items": []any{original}}, "owner:client", "account", "klap_get_items")
	if e != nil {
		t.Fatal(e)
	}
	rebuilt := ""
	pages := 0
	for {
		pages++
		raw, _ := json.Marshal(result)
		if len(raw) > queryPageBudget {
			t.Fatal("oversized")
		}
		v := result.(map[string]any)
		for _, item := range v["items"].([]any) {
			f := item.(map[string]any)["item_fragment"].(map[string]any)
			rebuilt += f["text"].(string)
		}
		page := v["page"].(map[string]any)
		if page["complete"].(bool) {
			break
		}
		cursor := page["next_cursor"].(string)
		for _, wrong := range [][3]string{{"other", "account", "klap_get_items"}, {"owner:client", "other", "klap_get_items"}, {"owner:client", "account", "klap_lectures"}} {
			if _, e := p.read(cursor, wrong[0], wrong[1], wrong[2]); e == nil {
				t.Fatal("cursor binding lost")
			}
		}
		result, e = p.read(cursor, "owner:client", "account", "klap_get_items")
		if e != nil {
			t.Fatal(e)
		}
		retry, e := p.read(cursor, "owner:client", "account", "klap_get_items")
		if e != nil || !reflect.DeepEqual(retry, result) {
			t.Fatal("non-idempotent retry")
		}
	}
	var got map[string]any
	if json.Unmarshal([]byte(rebuilt), &got) != nil || !reflect.DeepEqual(got, original) || pages < 2 {
		t.Fatal("fragment loss")
	}
	p.mu.Lock()
	for k, v := range p.pages {
		v.expires = time.Now().Add(-time.Second)
		p.pages[k] = v
	}
	p.mu.Unlock()
	if _, e := p.read("invalid", "owner:client", "account", "klap_get_items"); e == nil {
		t.Fatal("expired accepted")
	}
}
func TestEmptyQueryPage(t *testing.T) {
	p := &queryPages{}
	v, e := p.create(map[string]any{"items": []any{}}, "a", "b", "c")
	if e != nil || !v.(map[string]any)["page"].(map[string]any)["complete"].(bool) {
		t.Fatalf("%v %v", v, e)
	}
}
