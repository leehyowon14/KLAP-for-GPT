package jobs

import (
	"context"
	"errors"
	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
	"sync"
	"testing"
	"time"
)

type fakeRunner struct {
	mu       sync.Mutex
	calls    []string
	block    chan struct{}
	started  chan struct{}
	complete bool
	err      error
}

func (f *fakeRunner) Account(context.Context) (string, error) { return "account", nil }
func (f *fakeRunner) Attend(ctx context.Context, id, account string, progress func(Progress)) (bool, error) {
	f.mu.Lock()
	f.calls = append(f.calls, id)
	f.mu.Unlock()
	progress(Progress{ID: id, Percent: 25})
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.block != nil {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-f.block:
		}
	}
	return f.complete, f.err
}
func fixture(t *testing.T) (*Manager, *fakeRunner, *state.Store) {
	t.Helper()
	dir := t.TempDir()
	key, _ := state.LoadKey(dir)
	db, e := state.Open(dir, key)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	runner := &fakeRunner{complete: true}
	m, e := New(db, runner)
	if e != nil {
		t.Fatal(e)
	}
	return m, runner, db
}
func lecture(key string) string {
	id, _ := core.StableLectureID(core.CourseRef{TermValue: "2026,2", CourseID: "course"}, key)
	return id
}
func TestQueueDeduplicatesAndRequiresServerCompletion(t *testing.T) {
	m, r, _ := fixture(t)
	ctx := context.Background()
	j, e := m.Start(ctx, "owner:client", "same-key", []string{lecture("a"), lecture("a"), lecture("b")})
	if e != nil {
		t.Fatal(e)
	}
	same, e := m.Start(ctx, "owner:client", "same-key", []string{lecture("a"), lecture("b")})
	if e != nil || same.ID != j.ID {
		t.Fatal("duplicate request created a second job")
	}
	if _, e = m.Start(ctx, "owner:client", "same-key", []string{lecture("c")}); e == nil {
		t.Fatal("idempotency conflict accepted")
	}
	if _, e = m.Start(ctx, "owner:client", "other-key", []string{lecture("a")}); e == nil {
		t.Fatal("duplicate active lecture accepted")
	}
	if _, e = m.Get(j.ID, "another-client"); !errors.Is(e, state.ErrNotFound) {
		t.Fatal("cross-client read allowed")
	}
	r.complete = false
	if worked, e := m.RunNext(ctx); !worked || e != nil {
		t.Fatal(e)
	}
	result, _ := m.Get(j.ID, "owner:client")
	if result.Status != "failed" || len(r.calls) != 2 {
		t.Fatalf("%+v %v", result, r.calls)
	}
	for _, item := range result.Items {
		if item.Status != "failed" {
			t.Fatal("completion claimed without KLAS confirmation")
		}
	}
}
func TestCancellationStopsBeforeTheNextLecture(t *testing.T) {
	m, r, _ := fixture(t)
	r.block = make(chan struct{})
	r.started = make(chan struct{}, 1)
	j, e := m.Start(context.Background(), "owner:client", "cancel-key", []string{lecture("a"), lecture("b")})
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := m.RunNext(context.Background()); done <- e }()
	select {
	case <-r.started:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not start")
	}
	if _, e = m.Cancel(j.ID, "owner:client"); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel blocked")
	}
	result, _ := m.Get(j.ID, "owner:client")
	if result.Status != "cancelled" || len(r.calls) != 1 {
		t.Fatalf("%+v %v", result, r.calls)
	}
	if _, e = m.Cancel(j.ID, "owner:client"); e != nil {
		t.Fatal("repeat cancellation failed")
	}
}
func TestRestartNeverReplaysUnfinishedActions(t *testing.T) {
	m, r, db := fixture(t)
	j, e := m.Start(context.Background(), "owner:client", "restart-key", []string{lecture("a")})
	if e != nil {
		t.Fatal(e)
	}
	restored, e := New(db, r)
	if e != nil {
		t.Fatal(e)
	}
	result, e := restored.Get(j.ID, "owner:client")
	if e != nil || result.Status != "interrupted" {
		t.Fatalf("%+v %v", result, e)
	}
	if worked, e := restored.RunNext(context.Background()); worked || e != nil || len(r.calls) != 0 {
		t.Fatal("restart replayed a write")
	}
	same, e := restored.Start(context.Background(), "owner:client", "restart-key", []string{lecture("a")})
	if e != nil || same.ID != j.ID || same.Status != "interrupted" {
		t.Fatal("duplicate request replayed an interrupted action")
	}
}
func TestQueuedCancelAndInvalidIDs(t *testing.T) {
	m, r, _ := fixture(t)
	for _, ids := range [][]string{nil, {"1:2"}, {"lecture:v1:broken"}} {
		if _, e := m.Start(context.Background(), "owner:client", "invalid-key", ids); e == nil {
			t.Fatal("invalid ID queued")
		}
	}
	j, e := m.Start(context.Background(), "owner:client", "queued-cancel", []string{lecture("a")})
	if e != nil {
		t.Fatal(e)
	}
	m.Cancel(j.ID, "owner:client")
	m.RunNext(context.Background())
	if len(r.calls) != 0 {
		t.Fatal("cancelled queued job ran")
	}
}
