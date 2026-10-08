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
// may edit every CV: the connecting IP is looked up with `tailscale whois`
// only to reject non-peers and tagged (server) nodes, and to pick a default
// CV.
type Auth struct {
	DevUser string    // dev and local mode: skip the tailnet check, default to this CV
	CVs     *Registry // the CVs that can be edited
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

// User returns the CV a request works on: the one picked in the switcher,
// else the visitor's own, else the first. When there are no CVs yet and the
// editor manages them, the first visitor gets one named after their login.
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
	ids := a.CVs.IDs()
	if c, err := r.Cookie(cvCookie); err == nil && slices.Contains(ids, c.Value) {
		return c.Value, nil
	}
	if cv := a.CVs.ForLogin(login); cv != "" {
		return cv, nil
	}
	if a.DevUser != "" && (len(ids) == 0 || slices.Contains(ids, a.DevUser)) {
		return a.DevUser, nil
	}
	if len(ids) > 0 {
		return ids[0], nil
	}
	if a.CVs.manage {
		local, _, _ := strings.Cut(strings.ToLower(login), "@")
		if name := strings.Trim(nonName.ReplaceAllString(local, "-"), "-"); userNameRe.MatchString(name) {
			return name, nil
		}
		return "cv", nil
	}
	return "", errForbidden
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

// clientIP is the tailnet address a request came from. The visitor address a
// proxy passes on (X-Real-IP, or else the last X-Forwarded-For entry, as
// tailscale serve sets it) is only trusted from the proxy itself: a peer on
// the Unix socket (which only the proxy may open) or on loopback (TCP setups,
// a container sharing tailscale's network).
func clientIP(r *http.Request) string {
	trusted := r.RemoteAddr == "" || r.RemoteAddr == "@" // Unix socket peer
	if !trusted {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return ""
		}
		peer := net.ParseIP(host)
		if peer == nil {
			return ""
		}
		if !peer.IsLoopback() {
			return peer.String()
		}
	}
	forwarded := r.Header.Get("X-Real-IP")
	if forwarded == "" {
		hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		forwarded = hops[len(hops)-1]
	}
	if real := net.ParseIP(strings.TrimSpace(forwarded)); real != nil {
		return real.String()
	}
	return ""
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
