package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/leehyowon14/KLAP-cli/pkg/core"
	"github.com/leehyowon14/KLAP-for-GPT/internal/helper"
	"github.com/zalando/go-keyring"
)

const keychainService = "dev.leehyowon14.klap.helper"

func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func run() error {
	exe, _ := os.Executable()
	base := flag.String("server", "", "KLAP public HTTPS origin")
	pair := flag.Bool("pair", false, "read a one-time pairing code from stdin")
	pairOnly := flag.Bool("pair-only", false, "pair and exit before connecting")
	bridgeDir := flag.String("bridge-dir", filepath.Join(filepath.Dir(exe), "bridges", "macos"), "installed Swift bridge directory")
	flag.Parse()
	if runtime.GOOS != "darwin" {
		return errors.New("the Apple helper runs only on macOS")
	}
	u, e := url.Parse(strings.TrimRight(*base, "/"))
	if e != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("--server must be a public HTTPS origin")
	}
	*base = u.String()
	if *pair || *pairOnly {
		fmt.Fprint(os.Stderr, "KLAP 설정 화면의 일회용 연결 코드: ")
		line, e := bufio.NewReader(os.Stdin).ReadString('\n')
		if e != nil {
			return e
		}
		body, _ := json.Marshal(map[string]string{"code": strings.TrimSpace(line)})
		client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, e := client.Post(*base+"/api/helper/redeem", "application/json", bytes.NewReader(body))
		if e != nil {
			return e
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return errors.New("pairing failed; create a new code in KLAP settings")
		}
		var result struct {
			Token string `json:"token"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result) != nil || len(result.Token) < 32 {
			return errors.New("invalid pairing response")
		}
		if e = keyring.Set(keychainService, *base, result.Token); e != nil {
			return e
		}
		log.Print("Mac 연결 자격증명을 Keychain에 저장했습니다.")
		if *pairOnly {
			return nil
		}
	}
	token, e := keyring.Get(keychainService, *base)
	if e != nil {
		return errors.New("no paired credential; run with --pair first")
	}
	r, c, k := core.NativeAppleAdapters(filepath.Join(*bridgeDir, "ReminderBridge"), filepath.Join(*bridgeDir, "CalendarBridge"), filepath.Join(*bridgeDir, "CategoryBridge"))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	e = helper.RunClient(ctx, *base, token, helper.Executor{Reminders: r, Calendar: c, Categories: k}, func(connected bool) {
		if connected {
			log.Print("KLAP 서버에 연결되었습니다.")
		} else {
			log.Print("연결이 끊겼습니다. 진행 중이던 Apple 작업은 재실행하지 않습니다.")
		}
	})
	if errors.Is(e, context.Canceled) {
		return nil
	}
	return e
}
