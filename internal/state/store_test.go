package state

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestEncryptedTransactionsAndRestart(t *testing.T) {
	dir := t.TempDir()
	key, err := LoadKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, key); err == nil {
		t.Fatal("second instance accepted")
	}
	if err = s.Update(func(r Records) error { return Put(r, "account", "private password") }); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("rollback")
	if err = s.Update(func(r Records) error { Put(r, "account", "replacement"); return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "state.sealed"))
	if bytes.Contains(body, []byte("private password")) {
		t.Fatal("plaintext on disk")
	}
	info, _ := os.Stat(filepath.Join(dir, "state.sealed"))
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	s.Close()
	s, err = Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	s.View(func(r Records) error {
		v, _ := Get[string](r, "account")
		if v != "private password" {
			t.Fatal(v)
		}
		delete(r, "account")
		return nil
	})
	s.View(func(r Records) error {
		if _, err := Get[string](r, "account"); err != nil {
			t.Fatal("view changed state")
		}
		return nil
	})
	s.Close()
	wrong := bytes.Repeat([]byte{1}, 32)
	if _, err := Open(dir, wrong); err == nil {
		t.Fatal("wrong key accepted")
	}
	body[len(body)-1] ^= 1
	os.WriteFile(filepath.Join(dir, "state.sealed"), body, 0600)
	if _, err := Open(dir, key); err == nil {
		t.Fatal("corrupt state accepted")
	}
	os.Remove(filepath.Join(dir, "master.key"))
	if _, err := LoadKey(dir); err == nil {
		t.Fatal("lost key silently replaced")
	}
}
func TestConcurrentUpdatesDoNotLoseRecords(t *testing.T) {
	dir := t.TempDir()
	key, _ := LoadKey(dir)
	s, err := Open(dir, key)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Update(func(r Records) error { n, _ := Get[int](r, "n"); return Put(r, "n", n+1) }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	s.View(func(r Records) error {
		n, _ := Get[int](r, "n")
		if n != 20 {
			t.Fatal(n)
		}
		return nil
	})
}
