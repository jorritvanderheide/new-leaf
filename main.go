// Command cv-app is a multi-user CV editor. Each user's CV lives as one
// Markdown file per item and language; Hugo renders it, headless Chromium
// turns it into PDFs, and share links are published as static files into a
// separate webroot that a public web server serves.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // link expiry is evaluated in Europe/Amsterdam, also on hosts without zoneinfo
)

var userNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func main() {
	var (
		listen      = flag.String("listen", "127.0.0.1:8080", "editor listen address (put a tailnet-only reverse proxy in front)")
		dataDir     = flag.String("data", "/var/lib/cv-app", "persistent data: one content directory per user")
		workDir     = flag.String("work", "", "regenerable build output and caches (default: <data>/work)")
		publicDir   = flag.String("public", "/var/lib/cv-app/public", "webroot that the public share links are published into")
		publicURL   = flag.String("public-url", "https://cv.example.com", "base URL the public webroot is served at")
		siteDir     = flag.String("site", "", "Hugo site that renders a CV")
		uiDir       = flag.String("ui", "", "built editor UI")
		users       = flag.String("users", "", "comma-separated CVs, named after their owner's tailnet login; every tailnet user can edit all of them")
		devUser     = flag.String("dev-user", "", "skip tailnet identity and act as this user (local development only)")
		servePublic = flag.String("serve-public", "", "also serve the public webroot on this address (local development only)")
		hugoBin     = flag.String("hugo", "hugo", "hugo binary")
		chromeBin   = flag.String("chromium", "chromium", "chromium binary")
		tsBin       = flag.String("tailscale", "tailscale", "tailscale binary, used for identity lookups")
	)
	flag.Parse()

	if *siteDir == "" || *uiDir == "" {
		log.Fatal("-site and -ui are required")
	}
	if *workDir == "" {
		*workDir = filepath.Join(*dataDir, "work")
	}
	// Hugo resolves relative paths against the site, not the working directory.
	for _, p := range []*string{dataDir, workDir, publicDir, siteDir, uiDir} {
		abs, err := filepath.Abs(*p)
		if err != nil {
			log.Fatal(err)
		}
		*p = abs
	}

	cvs := map[string]bool{}
	for _, u := range strings.Split(*users, ",") {
		if u = strings.TrimSpace(strings.ToLower(u)); u != "" {
			cvs[u] = true
		}
	}
	if *devUser != "" {
		// Dev mode: every user with local data can be switched to.
		cvs[*devUser] = true
		users, _ := (&Store{Root: filepath.Join(*dataDir, "users")}).Users()
		for _, u := range users {
			cvs[u] = true
		}
	}
	for u := range cvs {
		if !userNameRe.MatchString(u) {
			log.Fatalf("invalid user name %q: use lowercase letters, digits and dashes", u)
		}
	}
	if len(cvs) == 0 {
		log.Fatal("no users configured: pass -users")
	}

	s := &Server{
		store:     &Store{Root: filepath.Join(*dataDir, "users")},
		renderer:  &Renderer{Hugo: *hugoBin, Chromium: *chromeBin, Site: *siteDir, Work: *workDir},
		publicDir: *publicDir,
		publicURL: strings.TrimRight(*publicURL, "/"),
		auth:      &Auth{DevUser: *devUser, CVs: cvs, Whois: tailscaleWhois(*tsBin)},
		uiDir:     *uiDir,
	}
	s.renderer.init()
	if err := claimPublicDir(*publicDir); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Bring every user's build and published links up to date (new app
	// version, links that expired while we were down), then keep expiring.
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

	srv := &http.Server{Addr: *listen, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Printf("editor listening on http://%s", *listen)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
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
