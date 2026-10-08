package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestFlagsFromEnvironment(t *testing.T) {
	t.Setenv("NEW_LEAF_PUBLIC_URL", "https://cv.test")
	t.Setenv("NEW_LEAF_DATA", "/from/env")
	fl := flag.NewFlagSet("test", flag.ContinueOnError)
	publicURL := fl.String("public-url", "", "")
	data := fl.String("data", "", "")
	parseFlags(fl, []string{"-data", "/from/args"})
	if *publicURL != "https://cv.test" || *data != "/from/args" {
		t.Errorf("public-url %q, data %q: the environment fills in, the command line wins", *publicURL, *data)
	}
}

func TestFindTypst(t *testing.T) {
	if got := findTypst("/opt/typst"); got != "/opt/typst" {
		t.Errorf("an explicit binary is used as given: %q", got)
	}
	exe, _ := os.Executable()
	next := filepath.Join(filepath.Dir(exe), "typst")
	if err := os.WriteFile(next, nil, 0o755); err != nil {
		t.Skip(err)
	}
	defer os.Remove(next)
	if got := findTypst("typst"); got != next {
		t.Errorf("typst next to new-leaf: %q, want %q", got, next)
	}
}

// Data from when New Leaf was called cv-app moves along.
func TestMoveOldData(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	old := platformDataDir("cv-app")
	os.MkdirAll(filepath.Join(old, "users", "me"), 0o755)
	dir := defaultDataDir()
	moveOldData(dir)
	if _, err := os.Stat(filepath.Join(dir, "users", "me")); err != nil {
		t.Errorf("not moved: %v", err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("old directory still there")
	}
	os.MkdirAll(filepath.Join(old, "users", "other"), 0o755)
	moveOldData(dir)
	if _, err := os.Stat(filepath.Join(dir, "users", "other")); err == nil {
		t.Error("moved over existing data")
	}
}
