package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"
	"unicode/utf8"
)

const queryPageBudget = 28 * 1024 // Leaves room for the MCP envelope and text.
const snapshotMemoryBudget = 64 * 1024 * 1024

type queryPage struct {
	principal, account, tool string
	expires                  time.Time
	body                     json.RawMessage
}
type queryPages struct {
	mu    sync.Mutex
	pages map[string]queryPage
	bytes int
}

func randomCursor() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func (p *queryPages) prune(now time.Time) {
	for key, page := range p.pages {
		if !now.Before(page.expires) {
			delete(p.pages, key)
			p.bytes -= len(page.body)
		}
	}
}
func (p *queryPages) read(cursor, principal, account, tool string) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prune(time.Now())
	page, ok := p.pages[cursor]
	if !ok || page.principal != principal || page.account != account || page.tool != tool {
		return nil, errors.New("invalid or expired cursor; start a new query")
	}
	var value any
	err := json.Unmarshal(page.body, &value)
	return value, err
}

// Oversized individual items are lossless UTF-8 JSON fragments. They never count
// as complete items until every part has been received and reassembled.
func fragmentItems(items []json.RawMessage) []json.RawMessage {
	out := []json.RawMessage{}
	for _, item := range items {
		if len(item) < queryPageBudget/2 {
			out = append(out, item)
			continue
		}
		var id struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(item, &id)
		parts := []string{}
		for len(item) > 0 {
			n := min(len(item), 4096)
			for n > 0 && !utf8.Valid(item[:n]) {
				n--
			}
			parts = append(parts, string(item[:n]))
			item = item[n:]
		}
		for i, part := range parts {
			raw, _ := json.Marshal(map[string]any{"item_fragment": map[string]any{"id": id.ID, "part": i, "parts": len(parts), "encoding": "json", "text": part}})
			out = append(out, raw)
		}
	}
	return out
}
func (p *queryPages) create(value any, principal, account, tool string) (any, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var base map[string]json.RawMessage
	if err = json.Unmarshal(body, &base); err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err = json.Unmarshal(base["items"], &items); err != nil {
		return nil, errors.New("query result missing items")
	}
	items = fragmentItems(items)
	delete(base, "items")
	baseSize, _ := json.Marshal(base)
	if len(baseSize) > queryPageBudget/2 {
		return nil, errors.New("query metadata too large; narrow course scope")
	}
	chunks := [][]json.RawMessage{{}}
	size := len(baseSize) + 512
	for _, item := range items {
		if size+len(item)+1 > queryPageBudget {
			chunks = append(chunks, []json.RawMessage{})
			size = len(baseSize) + 512
		}
		chunks[len(chunks)-1] = append(chunks[len(chunks)-1], item)
		size += len(item) + 1
	}
	cursors := make([]string, len(chunks))
	for i := 1; i < len(cursors); i++ {
		cursors[i] = randomCursor()
	}
	pages := make([]json.RawMessage, len(chunks))
	total := 0
	for i, chunk := range chunks {
		m := map[string]json.RawMessage{}
		for k, v := range base {
			m[k] = v
		}
		if chunk == nil {
			chunk = []json.RawMessage{}
		}
		m["items"], _ = json.Marshal(chunk)
		var next any
		if i+1 < len(chunks) {
			next = cursors[i+1]
		}
		m["page"], _ = json.Marshal(map[string]any{"complete": i == len(chunks)-1, "next_cursor": next})
		pages[i], err = json.Marshal(m)
		if err != nil {
			return nil, err
		}
		if len(pages[i]) > queryPageBudget {
			return nil, errors.New("query page exceeds byte budget")
		}
		total += len(pages[i])
	}
	if total > snapshotMemoryBudget {
		return nil, errors.New("query snapshot too large; narrow course scope")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.prune(now)
	if p.pages == nil {
		p.pages = map[string]queryPage{}
	}
	if p.bytes+total > snapshotMemoryBudget {
		return nil, errors.New("query snapshot capacity exhausted; retry later")
	}
	for i := 1; i < len(pages); i++ {
		p.pages[cursors[i]] = queryPage{principal: principal, account: account, tool: tool, expires: now.Add(5 * time.Minute), body: pages[i]}
		p.bytes += len(pages[i])
	}
	var result any
	err = json.Unmarshal(pages[0], &result)
	return result, err
}
