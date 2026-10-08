package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const typstHelp = `Typst makes the PDFs and was not found; everything else works without it.
Install it from https://github.com/typst/typst#installation, for example
with "nix profile install nixpkgs#typst", "brew install typst", "cargo install
typst-cli", or a binary from the releases page.`

// local runs cv-app on your own computer: no sign-in, no share
// links. It opens the editor in the browser and stops when no editor has
// been open for a while.
func local(args []string) {
	fl := flag.NewFlagSet("cv-app", flag.ExitOnError)
	fl.Usage = func() {
		fmt.Fprint(fl.Output(), "Usage:\n  cv-app [flags]         edit your CV on this computer\n  cv-app serve [flags]   run the multi-user server (see cv-app serve -h)\n\nFlags:\n")
		fl.PrintDefaults()
	}
	var (
		dataDir   = fl.String("data", defaultDataDir(), "where your CV is kept")
		listen    = fl.String("listen", "127.0.0.1:8484", "address to listen on; a free port is used if this one is taken")
		noBrowser = fl.Bool("no-browser", false, "don't open the editor in the browser")
		idle      = fl.Duration("idle", 5*time.Minute, "stop this long after the last editor was closed (0: never)")
		typstBin  = fl.String("typst", "typst", "typst binary, which makes the PDFs")
		devAssets = fl.String("dev-assets", "", "read templates and static files from this directory (development only)")
	)
	fl.Parse(args)

	// Already running? Then open that one.
	if url := runningAt(*listen); url != "" {
		fmt.Println("cv-app is already running at", url)
		if !*noBrowser {
			openBrowser(url)
		}
		return
	}
	if _, err := exec.LookPath(*typstBin); err != nil {
		fmt.Fprintln(os.Stderr, typstHelp)
	}

	name := localName()
	store := &Store{Root: filepath.Join(*dataDir, "users")}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	assets := NewAssets(*devAssets)
	typst, err := NewTypst(*typstBin, filepath.Join(cache, "cv-app"), assets)
	if err != nil {
		log.Fatal(err)
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			log.Fatal(err)
		}
	}
	url := "http://" + ln.Addr().String() + "/"

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s := &Server{
		store:  store,
		typst:  typst,
		assets: assets,
		work:   filepath.Join(cache, "cv-app"),
		auth:   &Auth{DevUser: name, CVs: NewRegistry(store, nil, true)},
		local:  true,
		quit:   stop,
	}
	var lastSeen atomic.Int64
	lastSeen.Store(time.Now().UnixNano())
	srv := &http.Server{
		Handler:           localOnly(&lastSeen, s.routes()),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	// Open editors ping every minute; stop once none has for a while.
	if *idle > 0 {
		go func() {
			for range time.Tick(30 * time.Second) {
				if time.Since(time.Unix(0, lastSeen.Load())) > *idle {
					fmt.Println("No editor open for", *idle, "- stopping.")
					stop()
					return
				}
			}
		}()
	}

	fmt.Printf("cv-app is running at %s\nYour CV is kept in %s\nStop it with Ctrl+C or the Quit button.\n", url, *dataDir)
	if !*noBrowser {
		openBrowser(url)
	}
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// localOnly answers only requests addressed to this computer by name. That
// stops other websites from reaching the editor by pointing a domain at
// 127.0.0.1 (DNS rebinding). It also notes when an editor was last seen.
func localOnly(lastSeen *atomic.Int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "localhost" && host != "127.0.0.1" && host != "::1" && host != "[::1]" {
			http.Error(w, "cv-app only answers on localhost", http.StatusForbidden)
			return
		}
		lastSeen.Store(time.Now().UnixNano())
		next.ServeHTTP(w, r)
	})
}

// runningAt returns the URL of a cv-app already listening on addr.
func runningAt(addr string) string {
	client := http.Client{Timeout: time.Second}
	res, err := client.Get("http://" + addr + "/api/ping")
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	var ping struct{ App string }
	if json.NewDecoder(res.Body).Decode(&ping) != nil || ping.App != "cv-app" {
		return ""
	}
	return "http://" + addr + "/"
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("Open", url, "in your browser.")
		return
	}
	go cmd.Wait()
}

// defaultDataDir follows the platform's convention, e.g.
// ~/.local/share/cv-app on Linux.
func defaultDataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "cv-app")
	}
	if home, err := os.UserHomeDir(); err == nil && runtime.GOOS == "linux" {
		return filepath.Join(home, ".local", "share", "cv-app")
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "cv-app")
	}
	return "cv-app"
}

var nonName = regexp.MustCompile(`[^a-z0-9-]+`)

// localName names the CV after the login, so it can later move to a server
// where CVs are named after tailnet logins.
func localName() string {
	if u, err := user.Current(); err == nil {
		name := strings.Trim(nonName.ReplaceAllString(strings.ToLower(u.Username), "-"), "-")
		if len(name) > 32 {
			name = name[:32]
		}
		if userNameRe.MatchString(name) {
			return name
		}
	}
	return "me"
}
