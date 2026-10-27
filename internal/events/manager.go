package events

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
)

type Arguments struct {
	Term     string `json:"term"`
	CourseID string `json:"course_id,omitempty"`
}

func (a *Arguments) UnmarshalJSON(body []byte) error {
	type fields Arguments
	var v fields
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return err
	}
	*a = Arguments(v)
	return nil
}

type Delivery struct {
	Mode   string `json:"mode"`
	URL    string `json:"url"`
	Secret string `json:"secret,omitempty"`
}
type SubscribeRequest struct {
	Name      string    `json:"name"`
	Arguments Arguments `json:"arguments"`
	Delivery  Delivery  `json:"delivery"`
	Cursor    *string   `json:"cursor,omitempty"`
	TTLMS     *int64    `json:"ttlMs,omitempty"`
}
type SubscribeResult struct {
	ID            string    `json:"id"`
	RefreshBefore time.Time `json:"refreshBefore"`
	Cursor        *string   `json:"cursor"`
	Truncated     bool      `json:"truncated"`
}
type Subscription struct {
	ID, Principal, Account, Name string
	Arguments                    Arguments
	Delivery                     Delivery
	ExpiresAt                    time.Time
	OldSecret                    string
	RotateUntil                  time.Time
	Disabled                     bool
}
type Payload struct {
	core.ContentItem
	DetectedAt      time.Time `json:"detected_at"`
	TimestampSource string    `json:"timestamp_source"`
}
type Event struct {
	EventID   string    `json:"eventId"`
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Data      Payload   `json:"data"`
	Cursor    *string   `json:"cursor"`
}
type pending struct {
	SubscriptionID string
	Event          Event
	Attempts       int
	NextAt         time.Time
}
type scopeState struct {
	Seen      core.SeenContent
	LastScan  time.Time
	LastError string
}
type Receipt struct {
	EventID  string    `json:"event_id"`
	Name     string    `json:"name"`
	Title    string    `json:"title"`
	Status   string    `json:"status"`
	At       time.Time `json:"at"`
	Attempts int       `json:"attempts"`
}
type ledger struct {
	Subscriptions map[string]Subscription
	Scopes        map[string]scopeState
	Outbox        map[string]pending
	Verified      map[string]time.Time
	Recent        []Receipt
}

func readLedger(r state.Records) (ledger, error) {
	v, err := state.Get[ledger](r, "events")
	if errors.Is(err, state.ErrNotFound) {
		v = ledger{}
		err = nil
	}
	if v.Subscriptions == nil {
		v.Subscriptions = map[string]Subscription{}
	}
	if v.Scopes == nil {
		v.Scopes = map[string]scopeState{}
	}
	if v.Outbox == nil {
		v.Outbox = map[string]pending{}
	}
	if v.Verified == nil {
		v.Verified = map[string]time.Time{}
	}
	return v, err
}

type Source interface {
	Content(context.Context, string, Arguments) ([]core.ContentItem, error)
	Account(context.Context) (string, error)
}
type Manager struct {
	db       *state.Store
	source   Source
	sender   Sender
	mu       sync.Mutex // serializes lifecycle changes and the start of each delivery
	now      func() time.Time
	interval time.Duration
	Allowed  func(context.Context, string) bool
}

func New(db *state.Store, source Source, sender Sender) *Manager {
	return &Manager{db: db, source: source, sender: sender, now: time.Now, interval: 5 * time.Minute}
}

var termPattern = regexp.MustCompile(`^[0-9]{4}[,-][1-4]$`)

func normalize(r SubscribeRequest) (SubscribeRequest, error) {
	switch r.Name {
	case "notice.created", "assignment.created", "lecture.created":
	default:
		return r, errors.New("unsupported event name")
	}
	r.Arguments.Term = strings.TrimSpace(r.Arguments.Term)
	if !termPattern.MatchString(r.Arguments.Term) {
		return r, errors.New("term must be YYYY-S")
	}
	r.Arguments.Term = strings.ReplaceAll(r.Arguments.Term, "-", ",")
	r.Arguments.CourseID = strings.TrimSpace(r.Arguments.CourseID)
	if len(r.Arguments.CourseID) > 1024 {
		return r, errors.New("course_id is too long")
	}
	if r.Delivery.Mode != "webhook" {
		return r, errors.New("only webhook delivery is supported")
	}
	if err := validateURL(r.Delivery.URL); err != nil {
		return r, err
	}
	if r.Cursor != nil && *r.Cursor != "" {
		return r, errors.New("these event types do not support cursor replay")
	}
	return r, nil
}
func identity(principal string, r SubscribeRequest) string {
	b, _ := json.Marshal([]any{principal, r.Delivery.URL, r.Name, r.Arguments})
	return fmt.Sprintf("sub_%x", sha256.Sum256(b))
}
func scopeKey(s Subscription) string {
	b, _ := json.Marshal([]any{s.Account, s.Name, s.Arguments})
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func active(s Subscription, now time.Time) bool { return !s.Disabled && now.Before(s.ExpiresAt) }
func (m *Manager) allowed(ctx context.Context, p string) bool {
	return p != "" && (m.Allowed == nil || m.Allowed(ctx, p))
}

func (m *Manager) Subscribe(ctx context.Context, principal string, request SubscribeRequest) (SubscribeResult, error) {
	r, err := normalize(request)
	if err != nil {
		return SubscribeResult{}, err
	}
	if _, err = secretBytes(r.Delivery.Secret); err != nil {
		return SubscribeResult{}, err
	}
	if !m.allowed(ctx, principal) {
		return SubscribeResult{}, errors.New("access revoked")
	}
	lifetime := 24 * time.Hour
	if r.TTLMS != nil {
		if *r.TTLMS <= 0 {
			return SubscribeResult{}, errors.New("ttlMs must be positive")
		}
		lifetime = time.Duration(min(*r.TTLMS, int64((7*24*time.Hour)/time.Millisecond))) * time.Millisecond
		if lifetime < time.Minute {
			lifetime = time.Minute
		}
	}
	account, err := m.source.Account(ctx)
	if err != nil || account == "" {
		return SubscribeResult{}, errors.New("connect a KLAS account before monitoring")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	id := identity(principal, r)
	cacheKey := eventID(principal, r.Delivery.URL, r.Delivery.Secret)
	verified := false
	if err = m.db.View(func(records state.Records) error {
		v, e := readLedger(records)
		verified = now.Before(v.Verified[cacheKey])
		return e
	}); err != nil {
		return SubscribeResult{}, err
	}
	if !verified {
		if err = m.sender.Verify(ctx, r.Delivery.URL, id, r.Delivery.Secret); err != nil {
			return SubscribeResult{}, err
		}
	}
	expiry := now.Add(lifetime)
	err = m.db.Update(func(records state.Records) error {
		v, err := readLedger(records)
		if err != nil {
			return err
		}
		old, exists := v.Subscriptions[id]
		if !exists && len(v.Subscriptions) >= 64 {
			return errors.New("subscription limit reached")
		}
		sub := Subscription{ID: id, Principal: principal, Account: account, Name: r.Name, Arguments: r.Arguments, Delivery: r.Delivery, ExpiresAt: expiry}
		if exists && old.Delivery.Secret != r.Delivery.Secret {
			sub.OldSecret = old.Delivery.Secret
			sub.RotateUntil = now.Add(5 * time.Minute)
		} else if exists && now.Before(old.RotateUntil) {
			sub.OldSecret = old.OldSecret
			sub.RotateUntil = old.RotateUntil
		}
		v.Subscriptions[id] = sub
		for key, until := range v.Verified {
			if !now.Before(until) {
				delete(v.Verified, key)
			}
		}
		v.Verified[cacheKey] = now.Add(5 * time.Minute)
		return state.Put(records, "events", v)
	})
	return SubscribeResult{ID: id, RefreshBefore: expiry}, err
}
func (m *Manager) Unsubscribe(ctx context.Context, principal string, request SubscribeRequest) error {
	r, err := normalize(request)
	if err != nil {
		return err
	}
	if principal == "" {
		return errors.New("unauthenticated")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id := identity(principal, r)
	return m.db.Update(func(records state.Records) error {
		v, err := readLedger(records)
		if err != nil {
			return err
		}
		delete(v.Subscriptions, id)
		for key, item := range v.Outbox {
			if item.SubscriptionID == id {
				delete(v.Outbox, key)
			}
		}
		return state.Put(records, "events", v)
	})
}

// Poll fetches complete fresh snapshots. A failed domain never advances its
// baseline. Baseline changes and all matching outbox entries commit together.
func (m *Manager) Poll(ctx context.Context) error {
	account, err := m.source.Account(ctx)
	if err != nil {
		return err
	}
	now := m.now().UTC()
	scopes := map[string]Subscription{}
	var snapshot ledger
	err = m.db.View(func(records state.Records) error {
		var e error
		snapshot, e = readLedger(records)
		return e
	})
	if err != nil {
		return err
	}
	for _, sub := range snapshot.Subscriptions {
		key := scopeKey(sub)
		if sub.Account == account && active(sub, now) && m.allowed(ctx, sub.Principal) && now.Sub(snapshot.Scopes[key].LastScan) >= m.interval {
			scopes[key] = sub
		}
	}
	keys := make([]string, 0, len(scopes))
	for key := range scopes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		scope := scopes[key]
		items, fetchErr := m.source.Content(ctx, scope.Name, scope.Arguments)
		if err = m.db.Update(func(records state.Records) error {
			v, e := readLedger(records)
			if e != nil {
				return e
			}
			old := v.Scopes[key]
			old.LastScan = now
			if fetchErr != nil {
				old.LastError = "source_unavailable"
				v.Scopes[key] = old
				return state.Put(records, "events", v)
			}
			seen, added, e := core.DetectContent(old.Seen, items)
			if e != nil {
				return e
			}
			if len(v.Outbox)+len(added)*len(v.Subscriptions) > 10000 {
				return errors.New("event outbox is full")
			}
			for _, item := range added {
				at := now
				origin := "detected"
				if item.OccurredAt != nil {
					at = item.OccurredAt.UTC()
					origin = "source"
				}
				// Large records are retrieved through the read tools.
				item.Title = truncate(item.Title, 1000)
				ev := Event{EventID: eventID(account, scope.Name, item.ID), Name: scope.Name, Timestamp: at, Data: Payload{ContentItem: item, DetectedAt: now, TimestampSource: origin}}
				body, e := json.Marshal(ev)
				if e != nil || len(body) > maxBody {
					return errors.New("invalid event payload")
				}
				for id, sub := range v.Subscriptions {
					if scopeKey(sub) == key && active(sub, now) {
						outKey := id + "/" + ev.EventID
						if _, ok := v.Outbox[outKey]; !ok {
							v.Outbox[outKey] = pending{SubscriptionID: id, Event: ev, NextAt: now}
						}
					}
				}
			}
			old.Seen = seen
			old.LastError = ""
			v.Scopes[key] = old
			return state.Put(records, "events", v)
		}); err != nil {
			return err
		}
	}
	return nil
}
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (m *Manager) Dispatch(ctx context.Context) error {
	var keys []string
	if err := m.db.View(func(r state.Records) error {
		v, e := readLedger(r)
		for key := range v.Outbox {
			keys = append(keys, key)
		}
		return e
	}); err != nil {
		return err
	}
	sort.Strings(keys)
	for _, key := range keys {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := m.dispatchOne(ctx, key); err != nil {
			return err
		}
	}
	return nil
}
func (m *Manager) dispatchOne(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	var p pending
	var sub Subscription
	found := false
	if err := m.db.View(func(r state.Records) error {
		v, e := readLedger(r)
		p, found = v.Outbox[key]
		sub = v.Subscriptions[p.SubscriptionID]
		return e
	}); err != nil {
		return err
	}
	if !found || now.Before(p.NextAt) {
		return nil
	}
	account, accountErr := m.source.Account(ctx)
	if accountErr != nil && active(sub, now) {
		return nil
	}
	if !active(sub, now) || sub.Account != account || !m.allowed(ctx, sub.Principal) {
		return m.db.Update(func(r state.Records) error {
			v, e := readLedger(r)
			if e != nil {
				return e
			}
			delete(v.Outbox, key)
			return state.Put(r, "events", v)
		})
	}
	body, err := json.Marshal(p.Event)
	if err != nil {
		return err
	}
	secrets := []string{sub.Delivery.Secret}
	if sub.OldSecret != "" && now.Before(sub.RotateUntil) {
		secrets = append(secrets, sub.OldSecret)
	}
	status, sendErr := m.sender.Send(ctx, sub.Delivery.URL, sub.ID, secrets, p.Event.EventID, body)
	return m.db.Update(func(r state.Records) error {
		v, e := readLedger(r)
		if e != nil {
			return e
		}
		p.Attempts++
		result := "retrying"
		accepted := sendErr == nil && status >= 200 && status < 300
		permanent := status == 410 || status == 413 || (status >= 400 && status < 500 && status != 408 && status != 425 && status != 429) || p.Attempts >= 8
		if accepted {
			result = "received"
			delete(v.Outbox, key)
		} else if permanent {
			result = "failed"
			delete(v.Outbox, key)
			if status == 410 {
				sub.Disabled = true
				v.Subscriptions[sub.ID] = sub
			}
		} else {
			p.NextAt = now.Add(time.Duration(1<<min(p.Attempts, 8)) * time.Second)
			v.Outbox[key] = p
		}
		v.Recent = append(v.Recent, Receipt{EventID: p.Event.EventID, Name: p.Event.Name, Title: p.Event.Data.Title, Status: result, At: now, Attempts: p.Attempts})
		if len(v.Recent) > 100 {
			v.Recent = v.Recent[len(v.Recent)-100:]
		}
		return state.Put(r, "events", v)
	})
}

type Status struct {
	Active    int       `json:"active_subscriptions"`
	Pending   int       `json:"pending_deliveries"`
	LastScan  time.Time `json:"last_scan"`
	LastError string    `json:"last_error,omitempty"`
	Recent    []Receipt `json:"recent"`
}

func (m *Manager) Status() (Status, error) {
	result := Status{Recent: []Receipt{}}
	err := m.db.View(func(r state.Records) error {
		v, e := readLedger(r)
		if e != nil {
			return e
		}
		for _, s := range v.Subscriptions {
			if active(s, m.now()) {
				result.Active++
			}
		}
		result.Pending = len(v.Outbox)
		result.Recent = v.Recent
		for _, s := range v.Scopes {
			if s.LastScan.After(result.LastScan) {
				result.LastScan = s.LastScan
			}
			if s.LastError != "" {
				result.LastError = s.LastError
			}
		}
		return nil
	})
	return result, err
}
