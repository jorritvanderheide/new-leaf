package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"codeberg.org/BW20/new-leaf/internal/cv"
	"codeberg.org/BW20/new-leaf/internal/render"
)

// LocalOptions configure New Leaf on your own computer: new-leaf.
type LocalOptions struct {
	Name      string        // the CV, named after the login
	Data      string        // where the CV is kept
	Cache     string        // files that can be made again
	Listen    string        // the address to try first; a free port if it is taken
	NoBrowser bool          // don't open the editor in the browser
	Idle      time.Duration // stop this long after the last editor was closed; 0: never
	DevAssets string        // read web/ from this folder (development)
	Typst     string        // the typst command
}

// Local runs New Leaf on your own computer: no sign-in, no share links. It
// opens the editor in the browser and stops when ctx ends, when someone
// chooses Quit (which calls stop), or when no editor has been open for a
// while. If New Leaf already runs, it opens that one instead.
func Local(ctx context.Context, stop func(), o LocalOptions) error {
	if url := runningAt(o.Listen); url != "" {
		fmt.Println("New Leaf is already running at", url)
		if !o.NoBrowser {
			openBrowser(url)
		}
		return nil
	}
	if _, err := exec.LookPath(o.Typst); err != nil {
		fmt.Fprintln(os.Stderr, render.TypstHelp)
	}

	store := &cv.Store{Root: filepath.Join(o.Data, "users")}
	assets := NewAssets(o.DevAssets)
	typst, err := render.NewTypst(o.Typst, o.Cache, assets.fs)
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", o.Listen)
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return err
		}
	}
	url := "http://" + ln.Addr().String() + "/"

	s := &Server{
		store:  store,
		typst:  typst,
		assets: assets,
		work:   o.Cache,
		auth:   &Auth{DevUser: o.Name, CVs: cv.NewRegistry(store, nil, true)},
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
	if o.Idle > 0 {
		go func() {
			for range time.Tick(30 * time.Second) {
				if time.Since(time.Unix(0, lastSeen.Load())) > o.Idle {
					fmt.Println("No editor open for", o.Idle, "- stopping.")
					stop()
					return
				}
			}
		}()
	}

	fmt.Printf("New Leaf is running at %s\nYour CV is kept in %s\nStop it with Ctrl+C or the Quit button.\n", url, o.Data)
	if !o.NoBrowser {
		openBrowser(url)
	}
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// localOnly answers only requests from this computer, addressed to it by
// name. The address keeps the network out, also with -listen set to one it
// can reach; the name stops other websites from reaching the editor by
// pointing a domain at 127.0.0.1 (DNS rebinding). It also notes when an
// editor was last seen.
func localOnly(lastSeen *atomic.Int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		peer, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(peer); ip == nil || !ip.IsLoopback() || host != "localhost" && host != "127.0.0.1" && host != "::1" && host != "[::1]" {
			http.Error(w, "New Leaf only answers on localhost", http.StatusForbidden)
			return
		}
		lastSeen.Store(time.Now().UnixNano())
		next.ServeHTTP(w, r)
	})
}

// runningAt returns the URL of a New Leaf already listening on addr.
func runningAt(addr string) string {
	client := http.Client{Timeout: time.Second}
	res, err := client.Get("http://" + addr + "/api/ping")
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	var ping struct{ App string }
	if json.NewDecoder(res.Body).Decode(&ping) != nil || ping.App != "new-leaf" {
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
