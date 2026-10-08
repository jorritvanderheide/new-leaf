package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// version is set when building a release: -ldflags "-X main.version=v1.2.3".
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
