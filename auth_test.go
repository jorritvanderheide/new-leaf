package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
		"100.64.0.1": {LoginName: "jorrit@"},
		"100.64.0.2": {LoginName: "Alice@example.com"},
		"100.64.0.3": {LoginName: "mallory@"},
		"100.64.0.4": {LoginName: "jorrit@", Tagged: true},
	}
	calls := 0
	a := &Auth{
		CVs: NewRegistry(&Store{Root: t.TempDir()}, []string{"jorrit", "alice"}, false),
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
	want := map[string]string{"100.64.0.1": "jorrit", "100.64.0.2": "alice", "100.64.0.3": "alice", "100.64.0.4": "", "100.64.0.5": ""}
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
		CVs: NewRegistry(&Store{Root: t.TempDir()}, []string{"jorrit", "bob"}, false),
		Whois: func(_ context.Context, ip string) (Identity, error) {
			if ip == "100.64.0.1" {
				return Identity{LoginName: "jorrit@"}, nil
			}
			return Identity{}, errForbidden
		},
	}
	for cookie, want := range map[string]string{"bob": "bob", "jorrit": "jorrit", "mallory": "jorrit", "": "jorrit"} {
		if got, _ := a.User(tailnetRequest("100.64.0.1", cookie)); got != want {
			t.Errorf("cookie %q: CV %q, want %q", cookie, got, want)
		}
	}
	if _, err := a.User(tailnetRequest("100.64.0.9", "bob")); !errors.Is(err, errForbidden) {
		t.Errorf("a cookie must not let a non-peer in: %v", err)
	}

	dev := &Auth{DevUser: "jorrit", CVs: NewRegistry(&Store{Root: t.TempDir()}, []string{"jorrit", "bob"}, false)}
	for cookie, want := range map[string]string{"bob": "bob", "mallory": "jorrit", "": "jorrit"} {
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
