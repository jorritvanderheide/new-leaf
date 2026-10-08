// Command new-leaf is New Leaf, a CV editor. Each CV lives as one Markdown
// file per item and language. Typst turns a selection into a PDF; share
// links are published as static HTML pages and PDFs into a separate webroot
// that a public web server serves. Templates, styles and fonts are embedded.
//
// The command reads flags and hands them to internal/server. A CV and its
// rules are in internal/cv, Typst in internal/render, and the files it
// serves in web.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"codeberg.org/BW20/new-leaf/internal/cv"
	"codeberg.org/BW20/new-leaf/internal/server"

	_ "time/tzdata"
)

// -timezone and TZ work on hosts without zoneinfo, such as the container

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		err = server.Serve(ctx, serveOptions(os.Args[2:]))
	} else {
		err = server.Local(ctx, stop, localOptions(os.Args[1:]))
	}
	if err != nil {
		log.Fatal(err)
	}
}

// serveOptions reads the flags of the multi-user server: tailnet sign-in and
// share links.
func serveOptions(args []string) server.ServeOptions {
	fl := flag.NewFlagSet("new-leaf serve", flag.ExitOnError)
	fl.Usage = func() {
		fmt.Fprint(fl.Output(), "Usage: new-leaf serve [flags]\n\nFlags (also as environment variables, e.g. NEW_LEAF_PUBLIC_URL for -public-url):\n")
		fl.PrintDefaults()
	}
	var (
		listen      = fl.String("listen", "127.0.0.1:8080", "editor address: host:port, unix:/path, or systemd (socket activation); put a tailnet-only reverse proxy in front")
		dataDir     = fl.String("data", "/var/lib/new-leaf", "persistent data: one content directory per user")
		workDir     = fl.String("work", "", "regenerable files such as extracted fonts (default: <data>/work)")
		publicDir   = fl.String("public", "/var/lib/new-leaf/public", "webroot that the public share links are published into")
		publicURL   = fl.String("public-url", "https://cv.example.com", "base URL the public webroot is served at")
		users       = fl.String("users", "", "comma-separated CVs that always exist, named after their owner's tailnet login; every tailnet user can edit all CVs")
		manage      = fl.Bool("manage", true, "let editor users create, rename and delete CVs (those in -users can't be deleted)")
		devUser     = fl.String("dev-user", "", "skip tailnet identity and act as this user (local development only)")
		devAssets   = fl.String("dev-assets", "", "read templates and static files from this directory instead of the binary, so edits show at once (development only)")
		servePublic = fl.String("serve-public", "", "also serve the public webroot on this address, with the headers it needs, e.g. behind tailscale funnel")
		tsBin       = fl.String("tailscale", "tailscale", "tailscale binary, used for identity lookups")
		typstBin    = fl.String("typst", "typst", "typst binary, which makes the PDFs")
		timeZone    = fl.String("timezone", "", "time zone of share links' end dates, e.g. Europe/Amsterdam (default: the system's, or TZ)")
	)
	parseFlags(fl, args)

	o := server.ServeOptions{
		Listen: *listen, Data: *dataDir, Work: *workDir, Public: *publicDir, PublicURL: *publicURL,
		ServePublic: *servePublic, Manage: *manage, DevUser: *devUser, DevAssets: *devAssets,
		Tailscale: *tsBin, Typst: findTypst(*typstBin),
	}
	if *timeZone != "" {
		loc, err := time.LoadLocation(*timeZone)
		if err != nil {
			log.Fatalf("-timezone: %v", err)
		}
		o.TimeZone = loc
	}
	if o.Work == "" {
		o.Work = filepath.Join(o.Data, "work")
	}
	for _, u := range strings.Split(*users, ",") {
		if u = strings.TrimSpace(strings.ToLower(u)); u != "" {
			o.Users = append(o.Users, u)
		}
	}
	if o.DevUser != "" {
		o.Users = append(o.Users, o.DevUser)
	}
	for _, u := range o.Users {
		if !cv.ValidName(u) {
			log.Fatalf("invalid user name %q: use lowercase letters, digits and dashes", u)
		}
	}
	if len(o.Users) == 0 && !o.Manage {
		log.Fatal("no CVs: pass -users, or leave -manage on to create them in the editor")
	}
	return o
}

// localOptions reads the flags of New Leaf on your own computer.
func localOptions(args []string) server.LocalOptions {
	fl := flag.NewFlagSet("new-leaf", flag.ExitOnError)
	fl.Usage = func() {
		fmt.Fprint(fl.Output(), "Usage:\n  new-leaf [flags]         edit your CV on this computer\n  new-leaf serve [flags]   run the multi-user server (see new-leaf serve -h)\n\nFlags (also as environment variables, e.g. NEW_LEAF_DATA for -data):\n")
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
	parseFlags(fl, args)
	if *dataDir == defaultDataDir() {
		moveOldData(*dataDir)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	return server.LocalOptions{
		Name: localName(), Data: *dataDir, Cache: filepath.Join(cache, "new-leaf"), Listen: *listen,
		NoBrowser: *noBrowser, Idle: *idle, DevAssets: *devAssets, Typst: findTypst(*typstBin),
	}
}
