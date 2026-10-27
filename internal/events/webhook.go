package events

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxBody = 256 * 1024

type CallbackError struct{ Reason string }

func (e *CallbackError) Error() string { return "callback verification failed: " + e.Reason }

type Sender interface {
	Verify(context.Context, string, string, string) error
	Send(context.Context, string, string, []string, string, []byte) (int, error)
}
type Webhook struct {
	client *http.Client
	now    func() time.Time
}

func NewWebhook() *Webhook {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{Proxy: nil, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxIdleConns: 8, IdleConnTimeout: 30 * time.Second}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("callback has no addresses")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("callback resolves to a non-public address")
			}
		}
		var last error
		for _, ip := range ips {
			c, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return c, nil
			}
			last = err
		}
		return nil, last
	}
	return &Webhook{client: &http.Client{Transport: tr, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, now: time.Now}
}
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 2048 {
		return errors.New("callback must be an absolute HTTPS URL without credentials or fragments")
	}
	if p := u.Port(); p != "" && p != "443" {
		return errors.New("callback must use HTTPS port 443")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(ip) {
		return errors.New("callback must be publicly reachable")
	}
	return nil
}

var blockedPrefixes = func() []netip.Prefix {
	var result []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16"} {
		result = append(result, netip.MustParsePrefix(s))
	}
	return result
}()

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Zone() != "" {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
func secretBytes(secret string) ([]byte, error) {
	if !strings.HasPrefix(secret, "whsec_") {
		return nil, errors.New("signing secret must have whsec_ prefix")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil || len(key) < 24 || len(key) > 64 {
		return nil, errors.New("signing key must decode to 24–64 bytes")
	}
	return key, nil
}
func signature(secret, id, stamp string, body []byte) (string, error) {
	key, err := secretBytes(secret)
	if err != nil {
		return "", err
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte(id + "." + stamp + "."))
	h.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}
func (w *Webhook) post(ctx context.Context, target, subscription string, secrets []string, id string, body []byte) (*http.Response, error) {
	if err := validateURL(target); err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, errors.New("event exceeds 256 KiB")
	}
	if len(secrets) == 0 {
		return nil, errors.New("missing signing secret")
	}
	stamp := strconv.FormatInt(w.now().Unix(), 10)
	var signatures []string
	for _, secret := range secrets {
		s, err := signature(secret, id, stamp, body)
		if err != nil {
			return nil, err
		}
		signatures = append(signatures, s)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", id)
	req.Header.Set("webhook-timestamp", stamp)
	req.Header.Set("webhook-signature", strings.Join(signatures, " "))
	req.Header.Set("X-MCP-Subscription-Id", subscription)
	return w.client.Do(req)
}
func (w *Webhook) Verify(ctx context.Context, target, subscription, secret string) error {
	challenge := rand.Text()
	body, _ := json.Marshal(map[string]string{"type": "verification", "challenge": challenge})
	res, err := w.post(ctx, target, subscription, []string{secret}, "msg_verification_"+rand.Text(), body)
	if err != nil {
		reason := "unreachable"
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			reason = "timeout"
		}
		return &CallbackError{reason}
	}
	defer res.Body.Close()
	var echo struct {
		Challenge string `json:"challenge"`
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &CallbackError{"challenge_failed"}
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&echo); err != nil || subtle.ConstantTimeCompare([]byte(echo.Challenge), []byte(challenge)) != 1 {
		return &CallbackError{"challenge_failed"}
	}
	return nil
}
func (w *Webhook) Send(ctx context.Context, target, subscription string, secrets []string, id string, body []byte) (int, error) {
	res, err := w.post(ctx, target, subscription, secrets, id, body)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 8192))
	return res.StatusCode, nil
}
func eventID(parts ...string) string {
	b, _ := json.Marshal(parts)
	return fmt.Sprintf("evt_%x", sha256.Sum256(b))
}
