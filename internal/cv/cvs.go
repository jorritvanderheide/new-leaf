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
type Registry struct {
	store    *Store
	declared map[string]bool
	Manage   bool // CVs may be created, renamed and deleted in the editor

	mu sync.Mutex
}

type CVInfo struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`            // display name
	Owners   []string `json:"owners"`          // tailnet logins that open this CV by default
	Declared bool     `json:"declared"`        // from the server configuration
	Label    string   `json:"label,omitempty"` // the name as set, without fallbacks
}

// cvMeta is <data>/users/<id>/cv.json.
type cvMeta struct {
	Name   string   `json:"name,omitempty"`
	Owners []string `json:"owners,omitempty"`
}

var loginRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._@+-]{0,63}$`)

func NewRegistry(store *Store, declared []string, manage bool) *Registry {
	r := &Registry{store: store, declared: map[string]bool{}, Manage: manage}
	for _, id := range declared {
		r.declared[id] = true
	}
	return r
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
			ID: id, Name: cmp.Or(name, id), Label: m.Name, Owners: m.Owners, Declared: r.declared[id],
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
		for _, o := range r.meta(id).Owners {
			if o == login || o == local {
				return id
			}
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

// Create makes a new, empty CV and returns its ID, derived from the name.
func (r *Registry) Create(name string, owners []string) (string, error) {
	if !r.Manage {
		return "", errManaged
	}
	name = Clean(name)
	if name == "" {
		return "", Invalid{errors.New("give the CV a name")}
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

// Update sets a CV's display name and owners.
func (r *Registry) Update(id, name string, owners []string) error {
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.store.Init(id); err != nil { // configured CVs may not exist yet
		return err
	}
	return r.writeMeta(id, cvMeta{Name: Clean(name), Owners: owners})
}

// Delete moves a CV to <data>/trash, where it can be recovered by hand.
func (r *Registry) Delete(id string) error {
	if !r.Manage {
		return errManaged
	}
	if !r.Exists(id) {
		return ErrNotFound
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
