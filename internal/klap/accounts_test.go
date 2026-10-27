package klap

import (
	"context"
	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
	"testing"
)

func TestCredentialsKeepAccountsIsolatedDuringRenewal(t *testing.T) {
	dir := t.TempDir()
	key, _ := state.LoadKey(dir)
	db, e := state.Open(dir, key)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	c := Credentials{db}
	ctx := context.Background()
	rows, e := c.List(ctx)
	if e != nil || len(rows) != 0 {
		t.Fatal(e)
	}
	if e = c.Save(ctx, "student-a", "password", core.Session{UserID: "u"}); e != nil {
		t.Fatal(e)
	}
	if e = c.Save(ctx, "student-b", "other", core.Session{}); e == nil {
		t.Fatal("implicit account replacement allowed")
	}
	if e = c.SaveSession(ctx, "student-b", core.Session{}); e == nil {
		t.Fatal("wrong account session write accepted")
	}
	if _, e = c.LoadPassword(ctx, "student-b"); e == nil {
		t.Fatal("other account password returned")
	}
	c.Remove(ctx, "student-a")
	c.Save(ctx, "student-b", "new", core.Session{})
	if e = c.SaveSession(ctx, "student-a", core.Session{}); e == nil {
		t.Fatal("stale renewal overwrote new account")
	}
}
