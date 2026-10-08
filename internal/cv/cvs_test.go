package cv

import (
	"errors"
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
	a, err := r.Create("  Bea   de Vries ", []string{"Bea@", ""}, "")
	if err != nil || a != "bea-de-vries" {
		t.Fatalf("Create: %q, %v", a, err)
	}
	b, _ := r.Create("Bea de Vries", nil, "")
	if b != "bea-de-vries-2" {
		t.Errorf("same name again: %q", b)
	}
	if _, err := r.Create("  ", nil, ""); err == nil {
		t.Error("a CV needs a name")
	}
	if _, err := r.Create("x", []string{"no spaces"}, ""); err == nil {
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

	if err := r.Update("carol", "Carol", []string{"carol@"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Update("nobody", "x", nil, ""); err == nil {
		t.Error("Update of a missing CV")
	}
	if list := r.List(); list[2].ID != "carol" || list[2].Label != "Carol" || !list[2].Declared {
		t.Errorf("after rename: %+v", list)
	}

	if err := r.Delete("carol", ""); err == nil {
		t.Error("configured CVs can't be deleted")
	}
	if err := r.Delete(b, ""); err != nil {
		t.Fatal(err)
	}
	if r.Exists(b) {
		t.Error("deleted CV still listed")
	}
	trash, _ := os.ReadDir(filepath.Join(data, "trash"))
	if len(trash) != 1 {
		t.Errorf("deleted CV goes to the trash: %v", trash)
	}
	if err := r.Delete(a, ""); err != nil {
		t.Fatal(err)
	}
	if err := NewRegistry(store, nil, true).Delete("carol", ""); err == nil {
		t.Error("the only CV can't be deleted")
	}

	fixed := NewRegistry(store, []string{"carol"}, false)
	if _, err := fixed.Create("x", nil, ""); err == nil {
		t.Error("Create without -manage")
	}
	if err := fixed.Update("carol", "x", nil, ""); err == nil {
		t.Error("Update without -manage")
	}
	store.Init("stray")
	if fixed.Exists("stray") {
		t.Error("without -manage only configured CVs exist")
	}
}

// With OwnersOnly, a login opens and changes only its own CVs, and those
// without owners.
func TestOwnersOnly(t *testing.T) {
	store := &Store{Root: filepath.Join(t.TempDir(), "users")}
	r := NewRegistry(store, []string{"carol"}, true)
	r.OwnersOnly = true

	bea, err := r.Create("Bea", nil, "bea@example.com")
	if err != nil {
		t.Fatal(err)
	}
	open, _ := r.Create("Shared", nil, "")
	for _, c := range []struct {
		id, login string
		may       bool
	}{
		{bea, "bea@example.com", true}, // made it
		{bea, "carol@", false},
		{"carol", "carol@", true}, // configured, named after her
		{"carol", "bea@example.com", false},
		{open, "carol@", true}, // nobody owns it
		{bea, "", false},
	} {
		if got := r.MayEdit(c.id, c.login); got != c.may {
			t.Errorf("MayEdit(%s, %q) = %v", c.id, c.login, got)
		}
	}

	if err := r.Update(bea, "Bea", []string{"bea@example.com"}, "carol@"); !errors.Is(err, ErrNotOwner) {
		t.Errorf("someone else renamed it: %v", err)
	}
	if err := r.Delete(bea, "carol@"); !errors.Is(err, ErrNotOwner) {
		t.Errorf("someone else deleted it: %v", err)
	}
	if err := r.Update(bea, "Bea", []string{"dave@"}, "bea@example.com"); err == nil {
		t.Error("an owner gave the CV away from themselves")
	}
	if err := r.Update(bea, "Bea", []string{"bea@example.com", "dave@"}, "bea@example.com"); err != nil || !r.MayEdit(bea, "dave@") {
		t.Errorf("adding an owner: %v", err)
	}
	if err := r.Update("carol", "Carol", []string{"dave@"}, "carol@"); err == nil {
		t.Error("the owner of a configured CV changed in the editor")
	}
	if err := r.Update(open, "Shared", []string{"carol@"}, "carol@"); err != nil || r.MayEdit(open, "bea@example.com") {
		t.Errorf("claiming an open CV: %v", err)
	}

	// Someone who may edit none gets one of their own, once.
	id, err := r.Adopt("Dave.X@example.com")
	if err != nil || id != "dave-x" || !r.Owns(id, "dave.x@example.com") {
		t.Fatalf("Adopt = %q, %v", id, err)
	}
	if again, _ := r.Adopt("dave.x@example.com"); again != id {
		t.Errorf("adopted twice: %q", again)
	}
}

// A configured CV given as a full login is owned by exactly that login; one
// given as a name by every login that starts with it.
func TestConfiguredLogins(t *testing.T) {
	for user, want := range map[string]string{"alice": "alice", "Dave.X@example.com": "dave-x", "alice@": "alice", "no spaces": "", "@x": "", "": ""} {
		if got := DeclaredName(user); got != want {
			t.Errorf("DeclaredName(%q) = %q, want %q", user, got, want)
		}
	}
	r := NewRegistry(&Store{Root: t.TempDir()}, []string{"carol", "dave@example.com"}, false)
	r.OwnersOnly = true
	if !slices.Equal(r.IDs(), []string{"carol", "dave"}) {
		t.Errorf("IDs = %v", r.IDs())
	}
	for _, c := range []struct {
		id, login string
		owns      bool
	}{
		{"dave", "dave@example.com", true},
		{"dave", "Dave@Example.com", true},
		{"dave", "dave@other.com", false},
		{"carol", "carol@", true},
		{"carol", "carol@other.com", true}, // a name: anyone called carol
	} {
		if got := r.Owns(c.id, c.login); got != c.owns {
			t.Errorf("Owns(%s, %s) = %v", c.id, c.login, got)
		}
	}
	if got := r.ForLogin("dave@example.com"); got != "dave" {
		t.Errorf("ForLogin = %q", got)
	}
}

// With OwnersOnly, a cv.json that can't be read doesn't open a CV to all.
func TestBrokenMetaIsClosed(t *testing.T) {
	store := &Store{Root: t.TempDir()}
	r := NewRegistry(store, nil, true)
	r.OwnersOnly = true
	id, _ := r.Create("Bea", nil, "bea@example.com")
	os.WriteFile(r.metaPath(id), []byte(`{"owners": ["bea@example.com"`), 0o640) // cut off
	if r.MayEdit(id, "mallory@example.com") || r.MayEdit(id, "bea@example.com") {
		t.Error("a CV with a broken cv.json is open")
	}
	if err := r.Update(id, "Mine", []string{"mallory@example.com"}, "mallory@example.com"); err == nil {
		t.Error("someone claimed a CV with a broken cv.json")
	}
	if !slices.Equal(r.Broken(), []string{id}) {
		t.Errorf("Broken = %v", r.Broken())
	}
}
