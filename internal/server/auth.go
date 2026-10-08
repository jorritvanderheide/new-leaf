package server

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

	"codeberg.org/BW20/new-leaf/internal/cv"
)

// Auth decides who may use the editor and which CV a request edits. The
// editor is only reachable over the tailnet, and every (human) tailnet user
// may edit every CV, unless the CVs are for their owners only: the
// connecting IP is looked up with `tailscale whois` to reject non-peers and
// tagged (server) nodes, to pick a default CV, and to know whose CVs those
// are.
type Auth struct {
	DevUser string       // dev and local mode: skip the tailnet check, default to this CV
	CVs     *cv.Registry // the CVs that can be edited
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
	user, _, err := a.Visitor(r)
	return user, err
}

// Visitor returns the CV a request works on, and the visitor's tailnet login
// (none in dev and local mode). The CV is the one picked in the switcher,
// else the visitor's own, else the first they may edit. When there are none
// and the editor manages CVs, the visitor gets one named after their login.
func (a *Auth) Visitor(r *http.Request) (user, login string, err error) {
	if a.DevUser == "" {
		ip := clientIP(r)
		if ip == "" {
			return "", "", errForbidden
		}
		id, err := a.lookup(r.Context(), ip)
		if err != nil {
			return "", "", err
		}
		if id.Tagged || (a.CVs.OwnersOnly && id.LoginName == "") {
			return "", "", errForbidden
		}
		login = id.LoginName
	}
	ids := slices.DeleteFunc(a.CVs.IDs(), func(id string) bool { return !a.MayEdit(id, login) })
	if c, err := r.Cookie(cvCookie); err == nil && slices.Contains(ids, c.Value) {
		return c.Value, login, nil
	}
	if id := a.CVs.ForLogin(login); id != "" && slices.Contains(ids, id) {
		return id, login, nil
	}
	if a.DevUser != "" && (len(ids) == 0 || slices.Contains(ids, a.DevUser)) {
		return a.DevUser, login, nil
	}
	if len(ids) > 0 {
		return ids[0], login, nil
	}
	if a.CVs.Manage && a.CVs.OwnersOnly {
		user, err := a.CVs.Adopt(login)
		return user, login, err
	}
	if a.CVs.Manage {
		local, _, _ := strings.Cut(strings.ToLower(login), "@")
		if name := cv.NameFor(local); name != "" {
			return name, login, nil
		}
		return "cv", login, nil
	}
	return "", "", errForbidden
}

// MayEdit reports whether a login may open and change a CV. In dev and
// local mode, everyone may.
func (a *Auth) MayEdit(id, login string) bool {
	return a.DevUser != "" || a.CVs.MayEdit(id, login)
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
// proxy passes on (X-Real-IP, or the last X-Forwarded-For entry, as tailscale
// serve sets it) is only trusted from the proxy itself: a peer on the Unix
// socket (which only the proxy may open) or on loopback (TCP setups, a
// container sharing tailscale's network). A proxy sets one of the two and
// may pass the other on from the visitor as it came, so when both are there
// they must agree: nginx sets both to the same address, and a visitor who
// adds one of their own gets nothing.
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
	real := r.Header.Get("X-Real-IP")
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	last := hops[len(hops)-1]
	if real == "" {
		real = last
	}
	ip := net.ParseIP(strings.TrimSpace(real))
	if ip == nil {
		return ""
	}
	if strings.TrimSpace(last) != "" && !ip.Equal(net.ParseIP(strings.TrimSpace(last))) {
		return ""
	}
	return ip.String()
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
