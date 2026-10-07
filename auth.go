package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

// Auth decides who may use the editor and which CV a request edits. The
// editor is only reachable over the tailnet, and every (human) tailnet user
// may edit every configured CV: the connecting IP is looked up with
// `tailscale whois` only to reject non-peers and tagged (server) nodes, and to
// pick a default CV.
type Auth struct {
	DevUser string          // dev mode: skip the tailnet check, default to this CV
	CVs     map[string]bool // the CVs that can be edited, by owner name
	Whois   func(ctx context.Context, ip string) (Identity, error)

	mu    sync.Mutex
	cache map[string]cachedIdentity
}

type Identity struct {
	LoginName string
	Tagged    bool // tagged nodes (servers) have no human owner
}

type cachedIdentity struct {
	id      Identity
	expires time.Time
}

var errForbidden = errors.New("forbidden")

// cvCookie holds the CV picked in the editor's switcher.
const cvCookie = "cv-user"

// User returns the CV a request works on.
func (a *Auth) User(r *http.Request) (string, error) {
	login := ""
	if a.DevUser == "" {
		ip := clientIP(r)
		if ip == "" {
			return "", errForbidden
		}
		id, err := a.lookup(r.Context(), ip)
		if err != nil {
			return "", err
		}
		if id.Tagged {
			return "", errForbidden
		}
		login = id.LoginName
	}
	if c, err := r.Cookie(cvCookie); err == nil && a.CVs[c.Value] {
		return c.Value, nil
	}
	if cv := a.match(login); cv != "" {
		return cv, nil
	}
	if a.DevUser != "" {
		return a.DevUser, nil
	}
	if names := a.Names(); len(names) > 0 {
		return names[0], nil
	}
	return "", errForbidden
}

// Names lists the CVs, sorted.
func (a *Auth) Names() []string {
	var names []string
	for n := range a.CVs {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// match finds the CV owned by a tailnet login: a CV name equal to the full
// login name or to its part before "@" (headscale reports e.g. "jorrit@").
func (a *Auth) match(login string) string {
	login = strings.ToLower(login)
	local, _, _ := strings.Cut(login, "@")
	for _, candidate := range []string{login, local} {
		if a.CVs[candidate] {
			return candidate
		}
	}
	return ""
}

func (a *Auth) lookup(ctx context.Context, ip string) (Identity, error) {
	a.mu.Lock()
	if c, ok := a.cache[ip]; ok && time.Now().Before(c.expires) {
		a.mu.Unlock()
		return c.id, nil
	}
	a.mu.Unlock()

	id, err := a.Whois(ctx, ip)
	if err != nil {
		return Identity{}, err
	}
	a.mu.Lock()
	if a.cache == nil {
		a.cache = map[string]cachedIdentity{}
	}
	a.cache[ip] = cachedIdentity{id, time.Now().Add(time.Minute)}
	a.mu.Unlock()
	return id, nil
}

// clientIP trusts X-Real-IP only from a loopback peer, i.e. the local
// reverse proxy; the listener itself is bound to loopback in production.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return ""
	}
	if peer.IsLoopback() {
		if real := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); real != nil {
			return real.String()
		}
		return ""
	}
	return peer.String()
}

func tailscaleWhois(bin string) func(context.Context, string) (Identity, error) {
	return func(ctx context.Context, ip string) (Identity, error) {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "whois", "--json", ip).Output()
		if err != nil {
			// Not a tailnet peer (or tailscaled unreachable): no identity.
			return Identity{}, fmt.Errorf("%w: whois %s: %v", errForbidden, ip, err)
		}
		var res struct {
			Node        struct{ Tags []string }
			UserProfile struct{ LoginName string }
		}
		if err := json.Unmarshal(out, &res); err != nil {
			return Identity{}, fmt.Errorf("whois %s: %w", ip, err)
		}
		return Identity{LoginName: res.UserProfile.LoginName, Tagged: len(res.Node.Tags) > 0}, nil
	}
}
