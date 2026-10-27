package events

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var testSecret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))

func TestCallbackAddressBoundary(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "172.31.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "198.18.0.1", "203.0.113.1", "0.0.0.0", "224.0.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "2001:db8::1", "2002:7f00:1::"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Errorf("allowed %s", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(ip)) {
			t.Errorf("blocked %s", ip)
		}
	}
	for _, u := range []string{"http://example.com", "https://localhost:8080", "https://u:p@example.com", "https://example.com/#fragment", "https://[::1]/", "https://127.0.0.1/"} {
		if validateURL(u) == nil {
			t.Errorf("accepted %s", u)
		}
	}
}
func TestChallengeAndDualSignatures(t *testing.T) {
	w := NewWebhook()
	w.now = func() time.Time { return time.Unix(123456, 0) }
	w.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var v map[string]string
		json.Unmarshal(body, &v)
		want, _ := signature(testSecret, r.Header.Get("webhook-id"), "123456", body)
		if r.Header.Get("webhook-signature") != want {
			t.Fatal("invalid signature")
		}
		if r.Header.Get("X-MCP-Subscription-Id") != "sub" {
			t.Fatal("missing subscription")
		}
		out, _ := json.Marshal(map[string]string{"challenge": v["challenge"]})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(out))), Header: http.Header{}}, nil
	})}
	if err := w.Verify(context.Background(), "https://example.com/", "sub", testSecret); err != nil {
		t.Fatal(err)
	}
	w.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if len(strings.Split(r.Header.Get("webhook-signature"), " ")) != 2 {
			t.Fatal("rotation must dual sign")
		}
		return &http.Response{StatusCode: 204, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	})
	if code, err := w.Send(context.Background(), "https://example.com/", "sub", []string{testSecret, testSecret}, "evt", []byte(`{}`)); err != nil || code != 204 {
		t.Fatalf("%d %v", code, err)
	}
}
func TestRejectsChallengeMismatchRedirectAndInvalidSecret(t *testing.T) {
	w := NewWebhook()
	for _, code := range []int{200, 302, 500} {
		w.client = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(`{"challenge":"wrong"}`)), Header: http.Header{}}, nil
		})}
		if err := w.Verify(context.Background(), "https://example.com", "sub", testSecret); err == nil {
			t.Fatal("bad verification accepted")
		}
	}
	for _, s := range []string{"", "whsec_x", "whsec_" + base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := secretBytes(s); err == nil {
			t.Fatal("bad secret accepted")
		}
	}
	if _, err := w.post(context.Background(), "https://example.com", "sub", []string{testSecret}, "evt", make([]byte, maxBody+1)); err == nil {
		t.Fatal("oversized body accepted")
	}
}
