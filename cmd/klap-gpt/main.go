package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/auth"
	"github.com/leehyowon14/KLAP-for-GPT/internal/events"
	"github.com/leehyowon14/KLAP-for-GPT/internal/helper"
	"github.com/leehyowon14/KLAP-for-GPT/internal/jobs"
	"github.com/leehyowon14/KLAP-for-GPT/internal/klap"
	"github.com/leehyowon14/KLAP-for-GPT/internal/server"
	"github.com/leehyowon14/KLAP-for-GPT/internal/state"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func run() error {
	listen := flag.String("listen", env("KLAP_LISTEN", "127.0.0.1:3030"), "HTTP listen address")
	base := flag.String("public-url", env("KLAP_PUBLIC_URL", "http://127.0.0.1:3030"), "public HTTPS origin")
	dir := flag.String("data-dir", env("KLAP_DATA_DIR", "data"), "private persistent directory")
	mode := flag.String("mode", env("KLAP_MODE", "server"), "server, mac, or hybrid")
	ui := flag.String("ui", env("KLAP_UI_FILE", "web/dist/index.html"), "built self-contained MCP App")
	flag.Parse()
	if *mode != "server" && *mode != "mac" && *mode != "hybrid" {
		return errors.New("mode must be server, mac, or hybrid")
	}
	if *mode == "mac" && runtime.GOOS != "darwin" {
		return errors.New("Mac mode requires macOS")
	}
	absolute, e := filepath.Abs(*dir)
	if e != nil {
		return e
	}
	*dir = absolute
	html, e := os.ReadFile(*ui)
	if e != nil {
		return fmt.Errorf("build the MCP App before starting: %w", e)
	}
	if len(html) > 4*1024*1024 {
		return errors.New("MCP App exceeds 4 MiB")
	}
	key, e := state.LoadKey(*dir)
	if e != nil {
		return e
	}
	db, e := state.Open(*dir, key)
	if e != nil {
		return e
	}
	defer db.Close()
	bootstrap, e := bootstrapToken(*dir)
	if e != nil {
		return e
	}
	authentication, e := auth.New(db, *base, key, bootstrap)
	if e != nil {
		return e
	}
	var shared *core.Service
	if *mode == "mac" {
		shared, e = core.NewDesktop()
	} else {
		if e = os.Setenv("KLAP_CONFIG_DIR", filepath.Join(*dir, "core", "config")); e != nil {
			return e
		}
		if e = os.Setenv("KLAP_CACHE_DIR", filepath.Join(*dir, "core", "cache")); e != nil {
			return e
		}
		shared, e = core.NewWithCredentials(klap.Credentials{DB: db})
	}
	if e != nil {
		return e
	}
	backend := &klap.Service{Core: shared, Apple: *mode != "server"}
	var mac *helper.Hub
	if *mode == "hybrid" {
		mac = helper.New(db)
		backend.Core = shared.WithAppleAdapters(helper.Reminders{Hub: mac}, helper.Calendar{Hub: mac}, helper.Categories{Hub: mac})
		backend.AppleReady = mac.Connected
	}
	queue, e := jobs.New(db, backend)
	if e != nil {
		return e
	}
	monitor := events.New(db, backend, events.NewWebhook())
	monitor.Allowed = authentication.Allowed
	app := &server.App{Backend: backend, Jobs: queue, Events: monitor, PublicURL: *base, Mode: *mode, HTML: string(html), Apple: *mode != "server", Helper: mac}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var workers sync.WaitGroup
	failed := make(chan error, 2)
	workers.Add(2)
	go func() {
		defer workers.Done()
		if err := queue.Run(ctx); err != nil && ctx.Err() == nil {
			failed <- fmt.Errorf("attendance worker stopped: %w", err)
		}
	}()
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if e := monitor.Poll(ctx); e != nil && ctx.Err() == nil {
					log.Printf("event scan failed: %v", e)
				}
				if e := monitor.Dispatch(ctx); e != nil && ctx.Err() == nil {
					log.Printf("event delivery failed: %v", e)
				}
			}
		}
	}()
	httpServer := &http.Server{Addr: *listen, Handler: app.Handler(authentication, backend), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 75 * time.Second, MaxHeaderBytes: 32 * 1024, BaseContext: nil}
	go func() { failed <- httpServer.ListenAndServe() }()
	log.Printf("KLAP %s mode: %s (MCP: %s/mcp)", *mode, *base, *base)
	log.Printf("First setup token is in %s; it is accepted only before owner setup.", filepath.Join(*dir, "bootstrap.token"))
	select {
	case e = <-failed:
		stop()
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdown)
	stop()
	workers.Wait()
	if e != nil && !errors.Is(e, http.ErrServerClosed) {
		return e
	}
	return nil
}
func bootstrapToken(dir string) (string, error) {
	path := filepath.Join(dir, "bootstrap.token")
	if b, e := os.ReadFile(path); e == nil {
		return string(b), nil
	} else if !errors.Is(e, os.ErrNotExist) {
		return "", e
	}
	token := rand.Text() + rand.Text()
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return "", e
	}
	_, e = f.WriteString(token)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	return token, e
}
