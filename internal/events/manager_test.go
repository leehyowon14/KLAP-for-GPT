package events

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
)

type fakeSource struct {
	items             []core.ContentItem
	err, errorAccount error
	account           string
	reads             int
}

func (s *fakeSource) Content(context.Context, string, Arguments) ([]core.ContentItem, error) {
	s.reads++
	return s.items, s.err
}
func (s *fakeSource) Account(context.Context) (string, error) { return s.account, s.errorAccount }

type sent struct {
	ID      string
	Body    []byte
	Secrets []string
}
type fakeSender struct {
	verifyErr error
	verified  int
	status    int
	sent      []sent
}

func (s *fakeSender) Verify(context.Context, string, string, string) error {
	s.verified++
	return s.verifyErr
}
func (s *fakeSender) Send(_ context.Context, _, _ string, secrets []string, id string, body []byte) (int, error) {
	s.sent = append(s.sent, sent{id, append([]byte{}, body...), secrets})
	return s.status, nil
}
func harness(t *testing.T) (*Manager, *fakeSource, *fakeSender, *time.Time, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	key, err := state.LoadKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := state.Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	source := &fakeSource{account: "account-a"}
	sender := &fakeSender{status: 204}
	now := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	m := New(db, source, sender)
	m.now = func() time.Time { return now }
	return m, source, sender, &now, dir, key
}
func subscription() SubscribeRequest {
	return SubscribeRequest{Name: "notice.created", Arguments: Arguments{Term: "2026-2"}, Delivery: Delivery{Mode: "webhook", URL: "https://example.com/events", Secret: testSecret}}
}
func mustPoll(t *testing.T, m *Manager) {
	t.Helper()
	if err := m.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func mustDispatch(t *testing.T, m *Manager) {
	t.Helper()
	if err := m.Dispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestBaselineAtomicOutboxRestartAndStableRetry(t *testing.T) {
	ctx := context.Background()
	m, source, sender, now, dir, key := harness(t)
	if _, err := m.Subscribe(ctx, "owner", subscription()); err != nil {
		t.Fatal(err)
	}
	source.items = []core.ContentItem{{ID: "old", Title: "기존 공지"}}
	mustPoll(t, m)
	mustDispatch(t, m)
	if len(sender.sent) != 0 {
		t.Fatal("baseline flooded historical items")
	}
	*now = now.Add(5 * time.Minute)
	source.items = []core.ContentItem{{ID: "new", Title: "새 공지"}, {ID: "old"}, {ID: "new"}}
	source.err = errors.New("partial course failure")
	mustPoll(t, m)
	mustDispatch(t, m)
	if len(sender.sent) != 0 {
		t.Fatal("partial fetch advanced baseline")
	}
	*now = now.Add(5 * time.Minute)
	source.err = nil
	mustPoll(t, m)
	before, _ := m.Status()
	if before.Pending != 1 {
		t.Fatalf("outbox = %d", before.Pending)
	}
	m.db.Close()
	db, err := state.Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m = New(db, source, sender)
	m.now = func() time.Time { return *now }
	sender.status = 500
	mustDispatch(t, m)
	if len(sender.sent) != 1 {
		t.Fatal("restart lost committed outbox")
	}
	first := sender.sent[0]
	mustDispatch(t, m)
	if len(sender.sent) != 1 {
		t.Fatal("ignored backoff")
	}
	*now = now.Add(3 * time.Second)
	sender.status = 204
	mustDispatch(t, m)
	if len(sender.sent) != 2 || sender.sent[1].ID != first.ID || string(sender.sent[1].Body) != string(first.Body) {
		t.Fatal("retry changed event identity or payload")
	}
	*now = now.Add(5 * time.Minute)
	source.items = []core.ContentItem{{ID: "old"}}
	mustPoll(t, m)
	*now = now.Add(5 * time.Minute)
	source.items = []core.ContentItem{{ID: "new", Title: "edited"}, {ID: "old"}}
	mustPoll(t, m)
	mustDispatch(t, m)
	if len(sender.sent) != 2 {
		t.Fatal("disappear/reappear or title update generated duplicate")
	}
	var event Event
	if err = json.Unmarshal(first.Body, &event); err != nil || event.Data.TimestampSource != "detected" || event.Cursor != nil {
		t.Fatalf("%+v %v", event, err)
	}
}
func TestSubscribeIdentityVerificationRotationAndUnsubscribe(t *testing.T) {
	ctx := context.Background()
	m, source, sender, now, _, _ := harness(t)
	r := subscription()
	a, err := m.Subscribe(ctx, "owner", r)
	if err != nil {
		t.Fatal(err)
	}
	r.Arguments.Term = " 2026,2 "
	b, err := m.Subscribe(ctx, "owner", r)
	if err != nil || a.ID != b.ID || sender.verified != 1 {
		t.Fatal("non-idempotent refresh or redundant verification")
	}
	r.Delivery.Secret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("abcdefghijklmnopqrstuvwxyzABCDEF"))
	if _, err = m.Subscribe(ctx, "owner", r); err != nil {
		t.Fatal(err)
	}
	source.items = nil
	mustPoll(t, m)
	*now = now.Add(time.Minute)
	m.interval = time.Second
	source.items = []core.ContentItem{{ID: "x"}}
	mustPoll(t, m)
	mustDispatch(t, m)
	if len(sender.sent) != 1 || len(sender.sent[0].Secrets) != 2 {
		t.Fatal("secret rotation did not dual sign")
	}
	source.items = append(source.items, core.ContentItem{ID: "y"})
	*now = now.Add(time.Minute)
	mustPoll(t, m)
	if err = m.Unsubscribe(ctx, "another-owner", r); err != nil {
		t.Fatal(err)
	}
	status, _ := m.Status()
	if status.Pending != 1 {
		t.Fatal("different principal unsubscribed someone else's event")
	}
	if err = m.Unsubscribe(ctx, "owner", r); err != nil {
		t.Fatal(err)
	}
	if err = m.Unsubscribe(ctx, "owner", r); err != nil {
		t.Fatal(err)
	}
	mustDispatch(t, m)
	if len(sender.sent) != 1 {
		t.Fatal("delivered after unsubscribe")
	}
}
func TestExpirationRevocationAndPermanentResponses(t *testing.T) {
	for _, scenario := range []string{"expired", "revoked", "account-changed", "410", "413", "temporarily-unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			m, source, sender, now, _, _ := harness(t)
			r := subscription()
			ttl := int64(60000)
			if scenario == "expired" {
				r.TTLMS = &ttl
			}
			if _, err := m.Subscribe(ctx, "owner", r); err != nil {
				t.Fatal(err)
			}
			mustPoll(t, m)
			*now = now.Add(time.Second)
			m.interval = time.Second
			source.items = []core.ContentItem{{ID: "x"}}
			mustPoll(t, m)
			switch scenario {
			case "expired":
				*now = now.Add(time.Minute)
			case "revoked":
				m.Allowed = func(context.Context, string) bool { return false }
			case "account-changed":
				source.account = "account-b"
			case "410":
				sender.status = 410
			case "413":
				sender.status = 413
			case "temporarily-unavailable":
				source.errorAccount = errors.New("temporary")
			}
			mustDispatch(t, m)
			*now = now.Add(time.Minute)
			mustDispatch(t, m)
			status, _ := m.Status()
			if scenario == "410" || scenario == "413" {
				if len(sender.sent) != 1 || status.Pending != 0 {
					t.Fatal("permanent failure retried")
				}
			} else if len(sender.sent) != 0 {
				t.Fatal("delivered without current authorization")
			}
			if scenario == "temporarily-unavailable" && status.Pending != 1 {
				t.Fatal("transient account lookup lost outbox")
			}
		})
	}
}
func TestBadSubscriptionsNeverActivate(t *testing.T) {
	m, _, sender, _, _, _ := harness(t)
	ctx := context.Background()
	r := subscription()
	sender.verifyErr = &CallbackError{"challenge_failed"}
	if _, err := m.Subscribe(ctx, "owner", r); err == nil {
		t.Fatal("verification failure accepted")
	}
	status, _ := m.Status()
	if status.Active != 0 {
		t.Fatal("activated failed callback")
	}
	sender.verifyErr = nil
	for _, bad := range []SubscribeRequest{{Name: "unknown"}, {Name: r.Name, Arguments: Arguments{Term: "2026-11"}, Delivery: r.Delivery}, {Name: r.Name, Arguments: r.Arguments, Delivery: Delivery{Mode: "webhook", URL: "https://127.0.0.1/", Secret: testSecret}}} {
		if _, err := m.Subscribe(ctx, "owner", bad); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	var args Arguments
	if json.Unmarshal([]byte(`{"term":"2026-2","typo":true}`), &args) == nil {
		t.Fatal("unknown argument accepted")
	}
}
