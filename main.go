// Command cv-app is a CV editor. Each CV lives as one Markdown file per item
// and language. Typst turns a selection into a PDF; share links are
// published as static HTML pages and PDFs into a separate webroot that a
// public web server serves. Templates, styles and fonts are embedded.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // link expiry is evaluated in Europe/Amsterdam, also on hosts without zoneinfo
)

var userNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		serve(os.Args[2:])
		return
	}
	local(os.Args[1:])
}

// serve runs the multi-user server: tailnet sign-in and share links.
func serve(args []string) {
	fl := flag.NewFlagSet("cv-app serve", flag.ExitOnError)
	var (
		listen      = fl.String("listen", "127.0.0.1:8080", "editor address: host:port, unix:/path, or systemd (socket activation); put a tailnet-only reverse proxy in front")
		dataDir     = fl.String("data", "/var/lib/cv-app", "persistent data: one content directory per user")
		workDir     = fl.String("work", "", "regenerable files such as extracted fonts (default: <data>/work)")
		publicDir   = fl.String("public", "/var/lib/cv-app/public", "webroot that the public share links are published into")
		publicURL   = fl.String("public-url", "https://cv.example.com", "base URL the public webroot is served at")
		users       = fl.String("users", "", "comma-separated CVs that always exist, named after their owner's tailnet login; every tailnet user can edit all CVs")
		manage      = fl.Bool("manage", true, "let editor users create, rename and delete CVs (those in -users can't be deleted)")
		devUser     = fl.String("dev-user", "", "skip tailnet identity and act as this user (local development only)")
		devAssets   = fl.String("dev-assets", "", "read templates and static files from this directory instead of the binary, so edits show at once (development only)")
		servePublic = fl.String("serve-public", "", "also serve the public webroot on this address (local development only)")
		tsBin       = fl.String("tailscale", "tailscale", "tailscale binary, used for identity lookups")
		typstBin    = fl.String("typst", "typst", "typst binary, which makes the PDFs")
	)
	fl.Parse(args)

	if *workDir == "" {
		*workDir = filepath.Join(*dataDir, "work")
	}

	var declared []string
	for _, u := range strings.Split(*users, ",") {
		if u = strings.TrimSpace(strings.ToLower(u)); u != "" {
			declared = append(declared, u)
		}
	}
	if *devUser != "" {
		declared = append(declared, *devUser)
	}
	for _, u := range declared {
		if !userNameRe.MatchString(u) {
			log.Fatalf("invalid user name %q: use lowercase letters, digits and dashes", u)
		}
	}
	if len(declared) == 0 && !*manage {
		log.Fatal("no CVs: pass -users, or leave -manage on to create them in the editor")
	}

	store := &Store{Root: filepath.Join(*dataDir, "users")}
	assets := NewAssets(*devAssets)
	typst, err := NewTypst(*typstBin, *workDir, assets)
	if err != nil {
		log.Fatal(err)
	}
	s := &Server{
		store:     store,
		typst:     typst,
		assets:    assets,
		publicDir: *publicDir,
		publicURL: strings.TrimRight(*publicURL, "/"),
		auth:      &Auth{DevUser: *devUser, CVs: NewRegistry(store, declared, *manage), Whois: tailscaleWhois(*tsBin)},
		sharing:   true,
	}
	if err := claimPublicDir(*publicDir); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	if *servePublic != "" {
		go func() {
			log.Printf("serving public webroot on http://%s", *servePublic)
			log.Print(http.ListenAndServe(*servePublic, publicHandler(*publicDir)))
		}()
	}

	ln, err := listener(*listen)
	if err != nil {
		log.Fatal(err)
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
		log.Fatal(err)
	}
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

// publicHandler serves the webroot the way the production web server does:
// noindex headers, no caching, and no directory listings.
func publicHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		w.Header().Set("Cache-Control", "no-cache")
		if strings.HasSuffix(r.URL.Path, "/") {
			index := filepath.Join(dir, filepath.FromSlash(path.Clean(r.URL.Path)), "index.html")
			if _, err := os.Stat(index); err != nil {
				http.NotFound(w, r)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}
