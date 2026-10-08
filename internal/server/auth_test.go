package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/BW20/new-leaf/internal/cv"
	"codeberg.org/BW20/new-leaf/internal/render"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		remote, realIP, want string
	}{
		{"127.0.0.1:5000", "100.64.0.7", "100.64.0.7"},  // via local reverse proxy
		{"127.0.0.1:5000", "", ""},                      // proxy that didn't say: no identity
		{"100.64.0.9:5000", "100.64.0.7", "100.64.0.9"}, // direct peers can't claim another IP
		{"[::1]:5000", "fd7a:115c:a1e0::1", "fd7a:115c:a1e0::1"},
		{"127.0.0.1:5000", "not-an-ip", ""},
		{"@", "100.64.0.7", "100.64.0.7"}, // Unix socket: only the proxy can connect
		{"", "100.64.0.7", "100.64.0.7"},  // Unix socket, unnamed peer
		{"@", "", ""},                     // proxy that didn't say
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.realIP != "" {
			r.Header.Set("X-Real-IP", c.realIP)
		}
		if got := clientIP(r); got != c.want {
			t.Errorf("clientIP(%s, %q) = %q, want %q", c.remote, c.realIP, got, c.want)
		}
	}
}

// tailscale serve passes the visitor on as X-Forwarded-For.
func TestClientIPForwardedFor(t *testing.T) {
	for _, c := range []struct{ remote, xff, want string }{
		{"127.0.0.1:5000", "100.64.0.7", "100.64.0.7"},
		{"127.0.0.1:5000", "6.6.6.6, 100.64.0.7", "100.64.0.7"}, // the last hop is the proxy's own view
		{"100.64.0.9:5000", "100.64.0.7", "100.64.0.9"},         // not from a proxy: ignored
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		r.Header.Set("X-Forwarded-For", c.xff)
		if got := clientIP(r); got != c.want {
			t.Errorf("clientIP(%s, XFF %q) = %q, want %q", c.remote, c.xff, got, c.want)
		}
	}
}

func TestAuthUser(t *testing.T) {
	ids := map[string]Identity{
		"100.64.0.1": {LoginName: "carol@"},
		"100.64.0.2": {LoginName: "Alice@example.com"},
		"100.64.0.3": {LoginName: "mallory@"},
		"100.64.0.4": {LoginName: "carol@", Tagged: true},
	}
	calls := 0
	a := &Auth{
		CVs: cv.NewRegistry(&cv.Store{Root: t.TempDir()}, []string{"carol", "alice"}, false),
		Whois: func(_ context.Context, ip string) (Identity, error) {
			calls++
			if id, ok := ids[ip]; ok {
				return id, nil
			}
			return Identity{}, errForbidden
		},
	}
	// Every human tailnet user gets in; their own CV by default, otherwise
	// the first. Tagged nodes and non-peers are refused.
	want := map[string]string{"100.64.0.1": "carol", "100.64.0.2": "alice", "100.64.0.3": "alice", "100.64.0.4": "", "100.64.0.5": ""}
	for ip, user := range want {
		got, err := a.User(tailnetRequest(ip, ""))
		if got != user || (user == "") != errors.Is(err, errForbidden) {
			t.Errorf("User(%s) = %q, %v; want %q", ip, got, err, user)
		}
	}

	before := calls
	a.User(tailnetRequest("100.64.0.1", ""))
	if calls != before {
		t.Errorf("whois result should be cached")
	}
}

func TestCVCookie(t *testing.T) {
	a := &Auth{
		CVs: cv.NewRegistry(&cv.Store{Root: t.TempDir()}, []string{"carol", "bob"}, false),
		Whois: func(_ context.Context, ip string) (Identity, error) {
			if ip == "100.64.0.1" {
				return Identity{LoginName: "carol@"}, nil
			}
			return Identity{}, errForbidden
		},
	}
	for cookie, want := range map[string]string{"bob": "bob", "carol": "carol", "mallory": "carol", "": "carol"} {
		if got, _ := a.User(tailnetRequest("100.64.0.1", cookie)); got != want {
			t.Errorf("cookie %q: CV %q, want %q", cookie, got, want)
		}
	}
	if _, err := a.User(tailnetRequest("100.64.0.9", "bob")); !errors.Is(err, errForbidden) {
		t.Errorf("a cookie must not let a non-peer in: %v", err)
	}

	dev := &Auth{DevUser: "carol", CVs: cv.NewRegistry(&cv.Store{Root: t.TempDir()}, []string{"carol", "bob"}, false)}
	for cookie, want := range map[string]string{"bob": "bob", "mallory": "carol", "": "carol"} {
		if got, _ := dev.User(tailnetRequest("", cookie)); got != want {
			t.Errorf("dev mode, cookie %q: CV %q, want %q", cookie, got, want)
		}
	}
}

func tailnetRequest(ip, cookie string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	if ip != "" {
		r.Header.Set("X-Real-IP", ip)
	}
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: cvCookie, Value: cookie})
	}
	return r
}

func TestFirstVisitorGetsACV(t *testing.T) {
	a := &Auth{
		CVs: cv.NewRegistry(&cv.Store{Root: t.TempDir()}, nil, true),
		Whois: func(context.Context, string) (Identity, error) {
			return Identity{LoginName: "Alice.B@example.com"}, nil
		},
	}
	if got, err := a.User(tailnetRequest("100.64.0.1", "")); got != "alice-b" || err != nil {
		t.Errorf("first visitor: %q, %v", got, err)
	}
	a.CVs.Manage = false
	if _, err := a.User(tailnetRequest("100.64.0.1", "")); err == nil {
		t.Error("without -manage and CVs, nobody gets in")
	}
}

// With -owners-only, the switcher can't open someone else's CV, and someone
// without one gets their own.
func TestOwnersOnlyAuth(t *testing.T) {
	logins := map[string]string{"100.64.0.1": "carol@", "100.64.0.2": "mallory@"}
	a := &Auth{
		CVs: cv.NewRegistry(&cv.Store{Root: t.TempDir()}, []string{"carol", "alice"}, true),
		Whois: func(_ context.Context, ip string) (Identity, error) {
			return Identity{LoginName: logins[ip]}, nil
		},
	}
	a.CVs.OwnersOnly = true
	for _, c := range []struct{ ip, cookie, want string }{
		{"100.64.0.1", "", "carol"},
		{"100.64.0.1", "alice", "carol"}, // not hers
		{"100.64.0.2", "carol", "mallory"},
		{"100.64.0.2", "", "mallory"},
	} {
		if got, _ := a.User(tailnetRequest(c.ip, c.cookie)); got != c.want {
			t.Errorf("%s with cookie %q: %q, want %q", logins[c.ip], c.cookie, got, c.want)
		}
	}

	if _, err := a.User(tailnetRequest("100.64.0.3", "")); err == nil {
		t.Error("an identity without a login got in")
	}
	logins["100.64.0.3"] = "dave@"
	a.CVs.Manage = false
	if _, err := a.User(tailnetRequest("100.64.0.3", "")); err == nil {
		t.Error("without -manage, someone without a CV gets none")
	}
}

// /healthz answers monitoring, which has no tailnet identity, and says when
// New Leaf can't write.
func TestHealth(t *testing.T) {
	if _, err := exec.LookPath("typst"); err != nil {
		t.Skip("typst not in PATH")
	}
	assets := NewAssets("")
	typst, err := render.NewTypst("typst", t.TempDir(), assets.fs)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		store: &cv.Store{Root: filepath.Join(t.TempDir(), "users")}, typst: typst, assets: assets, sharing: true, publicDir: t.TempDir(),
		auth: &Auth{CVs: cv.NewRegistry(&cv.Store{Root: t.TempDir()}, []string{"alice"}, false),
			Whois: func(context.Context, string) (Identity, error) { return Identity{}, errForbidden }},
	}
	h := s.routes()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, tailnetRequest("100.64.0.9", ""))
	if w.Code != http.StatusForbidden {
		t.Fatalf("the editor let a non-peer in: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != http.StatusOK || w.Body.String() != "ok\n" {
		t.Fatalf("healthz: %d %s", w.Code, w.Body)
	}

	if os.Geteuid() == 0 {
		return // root writes anyway
	}
	os.Chmod(s.publicDir, 0o500)
	defer os.Chmod(s.publicDir, 0o700)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "share links") {
		t.Errorf("healthz with a read-only share links folder: %d %s", w.Code, w.Body)
	}
}
