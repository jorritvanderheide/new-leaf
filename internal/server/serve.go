package server

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codeberg.org/BW20/new-leaf/internal/cv"
	"codeberg.org/BW20/new-leaf/internal/render"
)

// ServeOptions configure the multi-user server: new-leaf serve.
type ServeOptions struct {
	Listen      string         // the editor's address: host:port, unix:/path, or systemd
	Data        string         // the CVs
	Work        string         // files that can be made again
	Public      string         // the folder share links are published into
	PublicURL   string         // where that folder is served
	ServePublic string         // also serve it here, if set
	Users       []string       // CVs that always exist
	Manage      bool           // make, rename and delete CVs in the editor
	DevUser     string         // act as this user, without Tailscale (development)
	DevAssets   string         // read web/ from this folder (development)
	Tailscale   string         // the tailscale command, for whois
	Typst       string         // the typst command
	TimeZone    *time.Location // of dates, such as when links expire; nil for the system's
}

// Serve runs the multi-user server until ctx ends.
func Serve(ctx context.Context, o ServeOptions) error {
	if o.TimeZone != nil {
		cv.LinkZone = o.TimeZone
	}
	store := &cv.Store{Root: filepath.Join(o.Data, "users")}
	assets := NewAssets(o.DevAssets)
	typst, err := render.NewTypst(o.Typst, o.Work, assets.fs)
	if err != nil {
		return err
	}
	s := &Server{
		store:     store,
		typst:     typst,
		assets:    assets,
		publicDir: o.Public,
		publicURL: strings.TrimRight(o.PublicURL, "/"),
		work:      o.Work,
		auth:      &Auth{DevUser: o.DevUser, CVs: cv.NewRegistry(store, o.Users, o.Manage), Whois: tailscaleWhois(o.Tailscale)},
		sharing:   true,
	}
	if err := claimPublicDir(o.Public); err != nil {
		return err
	}

	// Bring every user's published links up to date (new app version, links
	// that expired while we were down), then keep expiring.
	go func() {
		s.publishAll(ctx)
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := s.reconcile(); err != nil {
					log.Printf("reconcile: %v", err)
				}
			}
		}
	}()

	if o.ServePublic != "" {
		go func() {
			log.Printf("serving public webroot on http://%s", o.ServePublic)
			log.Print(http.ListenAndServe(o.ServePublic, publicHandler(o.Public)))
		}()
	}

	ln, err := listener(o.Listen)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Printf("editor listening on %s", ln.Addr())
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// listener opens the editor's address: host:port, unix:/path, or "systemd"
// for a socket passed by systemd. On a Unix socket only processes that may
// open it (the reverse proxy) can reach the editor, which is what makes it
// safe to trust the visitor address the proxy passes on.
func listener(addr string) (net.Listener, error) {
	switch {
	case addr == "systemd":
		if os.Getenv("LISTEN_PID") != strconv.Itoa(os.Getpid()) || os.Getenv("LISTEN_FDS") != "1" {
			return nil, errors.New("-listen systemd: no socket was passed by systemd")
		}
		return net.FileListener(os.NewFile(3, "systemd socket"))
	case strings.HasPrefix(addr, "unix:"):
		path := strings.TrimPrefix(addr, "unix:")
		os.Remove(path)
		ln, err := net.Listen("unix", path)
		if err != nil {
			return nil, err
		}
		return ln, os.Chmod(path, 0o660)
	default:
		return net.Listen("tcp", addr)
	}
}

// publicHandler serves the webroot the way the NixOS module's nginx does:
// noindex, no caching, a strict content security policy, no directory
// listings, and nothing that starts with a dot (the marker file, publishes
// in progress).
func publicHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; font-src 'self'; img-src data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		clean := path.Clean("/" + r.URL.Path)
		if strings.Contains(clean, "/.") {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") {
			index := filepath.Join(dir, filepath.FromSlash(clean), "index.html")
			if _, err := os.Stat(index); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}
