package main

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"codeberg.org/BW20/new-leaf/internal/cv"
)

// version is set when building a release: -ldflags "-X main.version=1.2.3".
var version = "dev"

// parseFlags parses a command's flags, which can also come from the
// environment as NEW_LEAF_<FLAG>, e.g. NEW_LEAF_PUBLIC_URL for -public-url (for
// containers); the command line wins. -version prints the version and exits.
func parseFlags(fl *flag.FlagSet, args []string) {
	showVersion := fl.Bool("version", false, "print the version and exit")
	fl.VisitAll(func(f *flag.Flag) {
		name := envName(f.Name)
		if v, ok := os.LookupEnv(name); ok && f.Name != "version" {
			if err := fl.Set(f.Name, v); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
				os.Exit(2)
			}
		}
	})
	fl.Parse(args)
	if *showVersion {
		fmt.Println("new-leaf", version)
		os.Exit(0)
	}
}

func envName(flagName string) string {
	return "NEW_LEAF_" + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

// findTypst prefers a typst binary next to new-leaf itself, as in the release
// archives, over one on the PATH; an explicit -typst is used as given.
func findTypst(bin string) string {
	if bin != "typst" {
		return bin
	}
	exe, err := os.Executable()
	if err != nil {
		return bin
	}
	next := filepath.Join(filepath.Dir(exe), "typst")
	if runtime.GOOS == "windows" {
		next += ".exe"
	}
	if _, err := os.Stat(next); err == nil {
		return next
	}
	return bin
}

// defaultDataDir follows the platform's convention, e.g.
// ~/.local/share/new-leaf on Linux.
func defaultDataDir() string { return platformDataDir("new-leaf") }

func platformDataDir(name string) string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, name)
	}
	if home, err := os.UserHomeDir(); err == nil && runtime.GOOS == "linux" {
		return filepath.Join(home, ".local", "share", name)
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, name)
	}
	return name
}

// moveOldData moves the CVs of cv-app, as New Leaf was called before, to
// where New Leaf keeps them: once, while there are no New Leaf CVs yet.
func moveOldData(dir string) {
	old := platformDataDir("cv-app")
	if _, err := os.Stat(dir); err == nil {
		return
	}
	if _, err := os.Stat(old); err != nil {
		return
	}
	if err := os.Rename(old, dir); err != nil {
		fmt.Fprintf(os.Stderr, "Could not move your CVs from %s to %s: %v\n", old, dir, err)
		return
	}
	fmt.Println("Moved your CVs from", old, "to", dir)
}

// localName names the CV after the login, so it can later move to a server
// where CVs are named after tailnet logins.
func localName() string {
	if u, err := user.Current(); err == nil {
		if name := cv.NameFor(u.Username); name != "" {
			return name
		}
	}
	return "me"
}
