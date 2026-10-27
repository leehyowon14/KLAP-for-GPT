package jobs

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
)

type Progress struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Percent  float64 `json:"percent"`
	Achieved string  `json:"achieved"`
	Required string  `json:"required"`
}
type Item struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
type Job struct {
	ID              string    `json:"id"`
	Account         string    `json:"-"`
	Principal       string    `json:"-"`
	Key             string    `json:"-"`
	Status          string    `json:"status"`
	Items           []Item    `json:"items"`
	Progress        Progress  `json:"progress"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	CancelRequested bool      `json:"cancel_requested"`
	Error           string    `json:"error,omitempty"`
}

// Private fields use a separate persistence type so response serialization never
// exposes account identity or idempotency keys.
type savedJob struct {
	Job                     Job
	Account, Principal, Key string
}
type ledger struct {
	Jobs map[string]savedJob
	Keys map[string]string
}

func read(r state.Records) (ledger, error) {
	v, e := state.Get[ledger](r, "attendance")
	if errors.Is(e, state.ErrNotFound) {
		e = nil
	}
	if v.Jobs == nil {
		v.Jobs = map[string]savedJob{}
	}
	if v.Keys == nil {
		v.Keys = map[string]string{}
	}
	return v, e
}

type Runner interface {
	Account(context.Context) (string, error)
	Attend(context.Context, string, string, func(Progress)) (bool, error)
}
type Manager struct {
	db        *state.Store
	runner    Runner
	mu, runMu sync.Mutex
	active    string
	cancel    context.CancelFunc
	wake      chan struct{}
	now       func() time.Time
}

func New(db *state.Store, runner Runner) (*Manager, error) {
	m := &Manager{db: db, runner: runner, wake: make(chan struct{}, 1), now: time.Now}
	err := db.Update(func(r state.Records) error {
		v, e := read(r)
		if e != nil {
			return e
		}
		for id, j := range v.Jobs {
			if unfinished(j.Job.Status) {
				j.Job.Status = "interrupted"
				j.Job.Error = "서버가 재시작되어 중단되었습니다. 상태를 확인한 뒤 다시 요청하세요."
				j.Job.UpdatedAt = m.now()
				v.Jobs[id] = j
			}
		}
		return state.Put(r, "attendance", v)
	})
	return m, err
}
func unfinished(s string) bool { return s == "queued" || s == "running" }
func (m *Manager) Start(ctx context.Context, principal, key string, ids []string) (Job, error) {
	if principal == "" || len(key) < 8 || len(key) > 128 {
		return Job{}, errors.New("principal and an 8–128 character idempotency key are required")
	}
	unique := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			unique = append(unique, id)
			seen[id] = true
		}
	}
	if len(unique) > 100 {
		return Job{}, errors.New("select at most 100 lectures")
	}
	if err := core.ValidateLectureIDs(unique); err != nil {
		return Job{}, err
	}
	account, err := m.runner.Account(ctx)
	if err != nil || account == "" {
		return Job{}, errors.New("connect a KLAS account before attendance")
	}
	identityBytes, _ := json.Marshal([]string{principal, account, key})
	identity := string(identityBytes)
	var result Job
	err = m.db.Update(func(r state.Records) error {
		v, e := read(r)
		if e != nil {
			return e
		}
		if id, ok := v.Keys[identity]; ok {
			j := v.Jobs[id]
			if len(j.Job.Items) != len(unique) {
				return errors.New("idempotency key was used for different lectures")
			}
			for i, item := range j.Job.Items {
				if item.ID != unique[i] {
					return errors.New("idempotency key was used for different lectures")
				}
			}
			result = j.Job
			return nil
		}
		if len(v.Jobs) >= 10000 {
			return errors.New("attendance history is full")
		}
		pending := 0
		for _, j := range v.Jobs {
			if j.Account == account && unfinished(j.Job.Status) {
				pending++
				for _, item := range j.Job.Items {
					if seen[item.ID] {
						return errors.New("a selected lecture is already queued or running")
					}
				}
			}
		}
		if pending >= 5 {
			return errors.New("attendance queue is full")
		}
		now := m.now().UTC()
		result = Job{ID: "job_" + rand.Text(), Status: "queued", CreatedAt: now, UpdatedAt: now, Items: []Item{}}
		for _, id := range unique {
			result.Items = append(result.Items, Item{ID: id, Status: "queued"})
		}
		v.Jobs[result.ID] = savedJob{Job: result, Account: account, Principal: principal, Key: key}
		v.Keys[identity] = result.ID
		return state.Put(r, "attendance", v)
	})
	if err == nil {
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
	return result, err
}
func (m *Manager) Get(id, principal string) (Job, error) {
	var result Job
	err := m.db.View(func(r state.Records) error {
		v, e := read(r)
		if e != nil {
			return e
		}
		j, ok := v.Jobs[id]
		if !ok || (principal != "owner" && j.Principal != principal) {
			return state.ErrNotFound
		}
		result = j.Job
		return nil
	})
	return result, err
}
func (m *Manager) List(principal string) ([]Job, error) {
	result := []Job{}
	err := m.db.View(func(r state.Records) error {
		v, e := read(r)
		if e != nil {
			return e
		}
		for _, j := range v.Jobs {
			if principal == "owner" || j.Principal == principal {
				result = append(result, j.Job)
			}
		}
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	if len(result) > 50 {
		result = result[:50]
	}
	return result, err
}
func (m *Manager) Cancel(id, principal string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result Job
	err := m.db.Update(func(r state.Records) error {
		v, e := read(r)
		if e != nil {
			return e
		}
		j, ok := v.Jobs[id]
		if !ok || (principal != "owner" && j.Principal != principal) {
			return state.ErrNotFound
		}
		if unfinished(j.Job.Status) {
			j.Job.CancelRequested = true
			if j.Job.Status == "queued" {
				j.Job.Status = "cancelled"
			}
			j.Job.UpdatedAt = m.now()
			v.Jobs[id] = j
		}
		result = j.Job
		return state.Put(r, "attendance", v)
	})
	if err == nil && m.active == id && m.cancel != nil {
		m.cancel()
	}
	return result, err
}
func (m *Manager) change(id string, fn func(*Job)) error {
	return m.db.Update(func(r state.Records) error {
		v, e := read(r)
		if e != nil {
			return e
		}
		j, ok := v.Jobs[id]
		if !ok {
			return state.ErrNotFound
		}
		fn(&j.Job)
		j.Job.UpdatedAt = m.now().UTC()
		v.Jobs[id] = j
		return state.Put(r, "attendance", v)
	})
}
func (m *Manager) RunNext(ctx context.Context) (bool, error) {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	var next *savedJob
	err := m.db.View(func(r state.Records) error {
		v, e := read(r)
		if e != nil {
			return e
		}
		for _, j := range v.Jobs {
			if j.Job.Status == "queued" && (next == nil || j.Job.CreatedAt.Before(next.Job.CreatedAt)) {
				copy := j
				next = &copy
			}
		}
		return nil
	})
	if err != nil || next == nil {
		return false, err
	}
	jobCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.active = next.Job.ID
	m.cancel = cancel
	m.mu.Unlock()
	defer func() { cancel(); m.mu.Lock(); m.active = ""; m.cancel = nil; m.mu.Unlock() }()
	err = m.change(next.Job.ID, func(j *Job) {
		if j.CancelRequested {
			cancel()
			return
		}
		j.Status = "running"
	})
	if err != nil {
		return true, err
	}
	failed := false
	for index, item := range next.Job.Items {
		if jobCtx.Err() != nil {
			break
		}
		if err = m.change(next.Job.ID, func(j *Job) { j.Items[index].Status = "running"; j.Progress = Progress{ID: item.ID} }); err != nil {
			return true, err
		}
		var progressErr error
		complete, runErr := m.runner.Attend(jobCtx, item.ID, next.Account, func(p Progress) {
			if progressErr != nil {
				return
			}
			progressErr = m.change(next.Job.ID, func(j *Job) { j.Progress = p })
			if progressErr != nil {
				cancel()
			}
		})
		if progressErr != nil {
			return true, progressErr
		}
		if jobCtx.Err() != nil {
			break
		}
		itemStatus := "completed"
		message := ""
		if runErr != nil || !complete {
			failed = true
			itemStatus = "failed"
			message = "수강 완료가 확인되지 않았습니다."
			if runErr != nil {
				message = runErr.Error()
			}
		}
		if err = m.change(next.Job.ID, func(j *Job) { j.Items[index].Status = itemStatus; j.Items[index].Error = message }); err != nil {
			return true, err
		}
	}
	err = m.change(next.Job.ID, func(j *Job) {
		if j.CancelRequested {
			j.Status = "cancelled"
		} else if jobCtx.Err() != nil {
			j.Status = "interrupted"
		} else if failed {
			j.Status = "failed"
		} else {
			j.Status = "completed"
		}
		for i := range j.Items {
			if unfinished(j.Items[i].Status) {
				j.Items[i].Status = j.Status
			}
		}
	})
	return true, err
}
func (m *Manager) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		worked, err := m.RunNext(ctx)
		if err != nil {
			return err
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-m.wake:
		case <-time.After(5 * time.Second):
		}
	}
}
