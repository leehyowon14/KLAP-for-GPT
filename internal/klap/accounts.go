package klap

import (
	"context"
	"errors"
	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
	"time"
)

// Credentials adapts the encrypted instance ledger to KLAP-core. Authentication
// and session renewal still run exclusively inside the shared core.
type Credentials struct{ DB *state.Store }
type savedAccount struct {
	StudentID, Password string
	Session             core.Session
	SavedAt             time.Time
}

func (c Credentials) load() (savedAccount, error) {
	var v savedAccount
	err := c.DB.View(func(r state.Records) error { var e error; v, e = state.Get[savedAccount](r, "klas/account"); return e })
	return v, err
}
func (c Credentials) Save(_ context.Context, id, password string, session core.Session) error {
	return c.DB.Update(func(r state.Records) error {
		old, e := state.Get[savedAccount](r, "klas/account")
		if e != nil && !errors.Is(e, state.ErrNotFound) {
			return e
		}
		if old.StudentID != "" && old.StudentID != id {
			return errors.New("disconnect the current KLAS account before replacing it")
		}
		return state.Put(r, "klas/account", savedAccount{StudentID: id, Password: password, Session: session, SavedAt: time.Now().UTC()})
	})
}
func (c Credentials) List(context.Context) ([]core.SavedUser, error) {
	v, e := c.load()
	if errors.Is(e, state.ErrNotFound) {
		return []core.SavedUser{}, nil
	}
	if e != nil {
		return nil, e
	}
	return []core.SavedUser{{StudentID: v.StudentID, SavedAt: v.SavedAt, UserID: v.Session.UserID}}, nil
}
func (c Credentials) Current(context.Context) (string, error) {
	v, e := c.load()
	if errors.Is(e, state.ErrNotFound) {
		return "", nil
	}
	return v.StudentID, e
}
func (c Credentials) Select(_ context.Context, id string) error {
	v, e := c.load()
	if e != nil {
		return e
	}
	if id != v.StudentID {
		return errors.New("account not found")
	}
	return nil
}
func (c Credentials) Remove(_ context.Context, id string) error {
	return c.DB.Update(func(r state.Records) error {
		v, e := state.Get[savedAccount](r, "klas/account")
		if e != nil {
			return e
		}
		if id != v.StudentID {
			return errors.New("account not found")
		}
		delete(r, "klas/account")
		return nil
	})
}
func (c Credentials) LoadPassword(_ context.Context, id string) (string, error) {
	v, e := c.load()
	if e != nil {
		return "", e
	}
	if id != v.StudentID {
		return "", errors.New("account not found")
	}
	return v.Password, nil
}
func (c Credentials) LoadSession(_ context.Context, id string) (core.Session, error) {
	v, e := c.load()
	if e != nil {
		return core.Session{}, e
	}
	if id != v.StudentID {
		return core.Session{}, errors.New("account not found")
	}
	return v.Session, nil
}
func (c Credentials) SaveSession(_ context.Context, id string, session core.Session) error {
	return c.DB.Update(func(r state.Records) error {
		v, e := state.Get[savedAccount](r, "klas/account")
		if e != nil {
			return e
		}
		if id != v.StudentID {
			return errors.New("account changed during session renewal")
		}
		v.Session = session
		return state.Put(r, "klas/account", v)
	})
}
