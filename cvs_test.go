package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRegistry(t *testing.T) {
	data := t.TempDir()
	store := &Store{Root: filepath.Join(data, "users")}
	r := NewRegistry(store, []string{"carol"}, true)

	if got := r.IDs(); !slices.Equal(got, []string{"carol"}) {
		t.Fatalf("declared CVs exist from the start: %v", got)
	}
	a, err := r.Create("  Bea   de Vries ", []string{"Bea@", ""})
	if err != nil || a != "bea-de-vries" {
		t.Fatalf("Create: %q, %v", a, err)
	}
	b, _ := r.Create("Bea de Vries", nil)
	if b != "bea-de-vries-2" {
		t.Errorf("same name again: %q", b)
	}
	if _, err := r.Create("  ", nil); err == nil {
		t.Error("a CV needs a name")
	}
	if _, err := r.Create("x", []string{"no spaces"}); err == nil {
		t.Error("owners must look like logins")
	}

	list := r.List()
	if len(list) != 3 || list[0].ID != a || list[0].Name != "Bea de Vries" || !slices.Equal(list[0].Owners, []string{"bea@"}) {
		t.Errorf("List: %+v", list)
	}
	if got := r.ForLogin("bea@"); got != a {
		t.Errorf("owner login opens %q", got)
	}
	if got := r.ForLogin("carol@"); got != "carol" {
		t.Errorf("a CV named after the login opens by default: %q", got)
	}
	if got := r.ForLogin(""); got != "" {
		t.Errorf("no login, no own CV: %q", got)
	}

	if err := r.Update("carol", "Carol", []string{"carol@"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Update("nobody", "x", nil); err == nil {
		t.Error("Update of a missing CV")
	}
	if list := r.List(); list[2].ID != "carol" || list[2].Label != "Carol" || !list[2].Declared {
		t.Errorf("after rename: %+v", list)
	}

	if err := r.Delete("carol"); err == nil {
		t.Error("configured CVs can't be deleted")
	}
	if err := r.Delete(b); err != nil {
		t.Fatal(err)
	}
	if r.Exists(b) {
		t.Error("deleted CV still listed")
	}
	trash, _ := os.ReadDir(filepath.Join(data, "trash"))
	if len(trash) != 1 {
		t.Errorf("deleted CV goes to the trash: %v", trash)
	}
	if err := r.Delete(a); err != nil {
		t.Fatal(err)
	}
	if err := NewRegistry(store, nil, true).Delete("carol"); err == nil {
		t.Error("the only CV can't be deleted")
	}

	fixed := NewRegistry(store, []string{"carol"}, false)
	if _, err := fixed.Create("x", nil); err == nil {
		t.Error("Create without -manage")
	}
	if err := fixed.Update("carol", "x", nil); err == nil {
		t.Error("Update without -manage")
	}
	store.Init("stray")
	if fixed.Exists("stray") {
		t.Error("without -manage only configured CVs exist")
	}
}

func TestFirstVisitorGetsACV(t *testing.T) {
	a := &Auth{
		CVs: NewRegistry(&Store{Root: t.TempDir()}, nil, true),
		Whois: func(context.Context, string) (Identity, error) {
			return Identity{LoginName: "Alice.B@example.com"}, nil
		},
	}
	if got, err := a.User(tailnetRequest("100.64.0.1", "")); got != "alice-b" || err != nil {
		t.Errorf("first visitor: %q, %v", got, err)
	}
	a.CVs.manage = false
	if _, err := a.User(tailnetRequest("100.64.0.1", "")); err == nil {
		t.Error("without -manage and CVs, nobody gets in")
	}
}
