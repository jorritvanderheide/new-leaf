package cv

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Registry knows which CVs exist. A CV is a directory under <data>/users.
// CVs come from the server configuration (the NixOS module's users list)
// and, when manage is on, from the editor. Configured CVs can be renamed in
// the editor but not deleted: the configuration would only bring them back.
//
// With OwnersOnly, a tailnet login may only open and change the CVs it owns
// (see Owns), and those without owners. Making a CV makes you its owner,
// only owners change who they are, and the owners of a configured CV are
// the person it is named after.
type Registry struct {
	store      *Store
	declared   map[string]bool
	logins     map[string]string // configured CVs given as a full login: theirs
	Manage     bool              // CVs may be created, renamed and deleted in the editor
	OwnersOnly bool              // only a CV's owners may open it

	mu sync.Mutex
}

type CVInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`            // display name
	Owners   []string `json:"owners"`          // tailnet logins that open this CV by default
	Declared bool     `json:"declared"`        // from the server configuration
	Open     bool     `json:"open"`            // has no owners
	Label    string   `json:"label,omitempty"` // the name as set, without fallbacks
}

// cvMeta is <data>/users/<id>/cv.json.
type cvMeta struct {
	Name   string   `json:"name,omitempty"`
	Owners []string `json:"owners,omitempty"`
}

var loginRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._@+-]{0,63}$`)

// NewRegistry knows the CVs in store. The declared ones are given as a CV's
// name ("alice"), which is also the login it's for, in full or the part
// before "@"; or as a full login ("alice@example.com"), for a CV named after
// the part before "@" and owned by exactly that login.
func NewRegistry(store *Store, declared []string, manage bool) *Registry {
	r := &Registry{store: store, declared: map[string]bool{}, logins: map[string]string{}, Manage: manage}
	for _, d := range declared {
		id := DeclaredName(d)
		r.declared[id] = true
		if strings.Contains(d, "@") {
			r.logins[id] = strings.ToLower(d)
		}
	}
	return r
}

// DeclaredName is the CV a configured user is: the name itself, or the part
// of a login before "@". Empty if that can't name a CV.
func DeclaredName(user string) string {
	user = strings.ToLower(strings.TrimSpace(user))
	if local, _, ok := strings.Cut(user, "@"); ok {
		if !loginRe.MatchString(user) {
			return ""
		}
		return NameFor(local)
	}
	if !ValidName(user) {
		return ""
	}
	return user
}

func (r *Registry) metaPath(id string) string { return filepath.Join(r.store.Root, id, "cv.json") }

func (r *Registry) meta(id string) cvMeta {
	var m cvMeta
	if data, err := os.ReadFile(r.metaPath(id)); err == nil {
		json.Unmarshal(data, &m)
	}
	return m
}

// IDs lists the CVs: configured ones, plus those made in the editor.
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.declared))
	for id := range r.declared {
		ids = append(ids, id)
	}
	if r.Manage {
		dirs, _ := r.store.Users()
		for _, id := range dirs {
			if !r.declared[id] {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	return ids
}

func (r *Registry) Exists(id string) bool { return slices.Contains(r.IDs(), id) }

// List describes every CV, sorted by display name.
func (r *Registry) List() []CVInfo {
	out := []CVInfo{}
	for _, id := range r.IDs() {
		m := r.meta(id)
		if m.Owners == nil {
			m.Owners = []string{}
		}
		name := m.Name
		if name == "" {
			if p, err := r.store.Profile(id); err == nil {
				name = p.Name
			}
		}
		out = append(out, CVInfo{
			ID: id, Name: cmp.Or(name, id), Label: m.Name, Owners: m.Owners, Declared: r.declared[id], Open: r.open(id),
		})
	}
	slices.SortFunc(out, func(a, b CVInfo) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), strings.Compare(a.ID, b.ID))
	})
	return out
}

// ForLogin is the CV a tailnet login opens by default: one it owns, or one
// named after it (in full or the part before "@").
func (r *Registry) ForLogin(login string) string {
	if login == "" {
		return ""
	}
	login = strings.ToLower(login)
	local, _, _ := strings.Cut(login, "@")
	ids := r.IDs()
	for _, id := range ids {
		if r.logins[id] == login || slices.ContainsFunc(r.meta(id).Owners, func(o string) bool { return sameLogin(o, login) }) {
			return id
		}
	}
	for _, candidate := range []string{login, local} {
		if slices.Contains(ids, candidate) {
			return candidate
		}
	}
	return ""
}

func (r *Registry) checkOwners(owners []string) ([]string, error) {
	var out []string
	for _, o := range owners {
		o = strings.ToLower(strings.TrimSpace(o))
		if o == "" {
			continue
		}
		if !loginRe.MatchString(o) {
			return nil, fmt.Errorf("%q is not a tailnet login", o)
		}
		if !slices.Contains(out, o) {
			out = append(out, o)
		}
	}
	return out, nil
}

// Create makes a new, empty CV for a login and returns its ID, derived from
// the name. With OwnersOnly, the login is one of its owners.
func (r *Registry) Create(name string, owners []string, login string) (string, error) {
	if !r.Manage {
		return "", errManaged
	}
	name = Clean(name)
	if name == "" {
		return "", Invalid{errors.New("give the CV a name")}
	}
	if r.OwnersOnly && login != "" && !slices.ContainsFunc(owners, func(o string) bool { return sameLogin(strings.ToLower(strings.TrimSpace(o)), login) }) {
		owners = append(owners, strings.ToLower(login))
	}
	owners, err := r.checkOwners(owners)
	if err != nil {
		return "", Invalid{err}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	base := slugify(name)
	if len(base) > 28 {
		base = strings.Trim(base[:28], "-")
	}
	if !userNameRe.MatchString(base) {
		base = "cv"
	}
	id := base
	for n := 2; r.Exists(id) || r.dirExists(id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	if err := r.store.Init(id); err != nil {
		return "", err
	}
	return id, r.writeMeta(id, cvMeta{Name: name, Owners: owners})
}

func (r *Registry) dirExists(id string) bool {
	_, err := os.Stat(filepath.Join(r.store.Root, id))
	return !errors.Is(err, fs.ErrNotExist)
}

// Update sets a CV's display name and owners, for a login. With OwnersOnly,
// only an owner may, and must stay one; a configured CV keeps its owners.
func (r *Registry) Update(id, name string, owners []string, login string) error {
	if !r.Manage {
		return errManaged
	}
	if !r.Exists(id) {
		return ErrNotFound
	}
	owners, err := r.checkOwners(owners)
	if err != nil {
		return Invalid{err}
	}
	if r.OwnersOnly && login != "" {
		switch {
		case !r.MayEdit(id, login):
			return ErrNotOwner
		case r.declared[id] && !sameSet(owners, r.meta(id).Owners):
			return Invalid{errors.New("this CV's owner is set in the server configuration")}
		case len(owners) > 0 && !slices.ContainsFunc(owners, func(o string) bool { return sameLogin(o, login) }):
			return Invalid{errors.New("keep yourself as an owner, or you can't open this CV anymore")}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.store.Init(id); err != nil { // configured CVs may not exist yet
		return err
	}
	return r.writeMeta(id, cvMeta{Name: Clean(name), Owners: owners})
}

// Delete moves a CV to <data>/trash, where it can be recovered by hand.
// With OwnersOnly, only its owners may.
func (r *Registry) Delete(id, login string) error {
	if !r.Manage {
		return errManaged
	}
	if !r.Exists(id) {
		return ErrNotFound
	}
	if r.OwnersOnly && login != "" && !r.MayEdit(id, login) {
		return ErrNotOwner
	}
	if r.declared[id] {
		return Invalid{errors.New("this CV is defined in the server configuration; remove it there")}
	}
	if len(r.IDs()) == 1 {
		return Invalid{errors.New("this is the only CV")}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	trash := filepath.Join(filepath.Dir(r.store.Root), "trash")
	if err := os.MkdirAll(trash, 0o750); err != nil {
		return err
	}
	return os.Rename(filepath.Join(r.store.Root, id), filepath.Join(trash, id+"-"+time.Now().Format("2006-01-02-150405")))
}

func (r *Registry) writeMeta(id string, m cvMeta) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(r.metaPath(id), append(data, '\n'))
}

var errManaged = Invalid{errors.New("CVs are managed in the server configuration")}

// sameLogin reports whether an owner, as written in cv.json, is a tailnet
// login: in full, or the part before "@".
func sameLogin(owner, login string) bool {
	login = strings.ToLower(login)
	local, _, _ := strings.Cut(login, "@")
	return owner == login || owner == local
}

// Owns reports whether a tailnet login owns a CV: it is in the CV's owners,
// or the CV is from the server configuration, for that login. An owner or a
// configured CV given as a full login is that login only; one given as a
// name is everyone whose login starts with it, which is fine for a family
// tailnet but not for one with people from elsewhere.
func (r *Registry) Owns(id, login string) bool {
	if login == "" {
		return false
	}
	if slices.ContainsFunc(r.meta(id).Owners, func(o string) bool { return sameLogin(o, login) }) {
		return true
	}
	local, _, _ := strings.Cut(strings.ToLower(login), "@")
	if l := r.logins[id]; l != "" {
		return l == strings.ToLower(login)
	}
	return r.declared[id] && (sameLogin(id, login) || id == NameFor(local))
}

// open reports whether nobody owns a CV yet.
func (r *Registry) open(id string) bool { return !r.declared[id] && len(r.meta(id).Owners) == 0 }

// MayEdit reports whether a tailnet login may open and change a CV.
func (r *Registry) MayEdit(id, login string) bool {
	return !r.OwnersOnly || r.open(id) || r.Owns(id, login)
}

// Adopt makes a CV for a login that may edit none, named after it and owned
// by it, and returns its ID.
func (r *Registry) Adopt(login string) (string, error) {
	local, _, _ := strings.Cut(strings.ToLower(login), "@")
	base := cmp.Or(NameFor(local), "cv")
	if len(base) > 28 {
		base = strings.Trim(base[:28], "-")
	}
	owners, err := r.checkOwners([]string{login})
	if err != nil {
		return "", Invalid{err}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Two first requests at once make one CV.
	for _, id := range r.IDs() {
		if r.Owns(id, login) {
			return id, nil
		}
	}
	id := base
	for n := 2; r.Exists(id) || r.dirExists(id); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	if err := r.store.Init(id); err != nil {
		return "", err
	}
	return id, r.writeMeta(id, cvMeta{Owners: owners})
}

// ErrNotOwner is a change to a CV by someone who doesn't own it, with
// OwnersOnly.
var ErrNotOwner = errors.New("only this CV's owners can change it")
