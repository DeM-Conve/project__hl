// Package scan finds apps in the `projects` folders of hl.yml and works out
// what `hl sync` (and `hl init`) would change. It never writes anything and
// never parses compose files: whether a folder has one is a file-name check,
// and services and ports come from the model `docker compose config` resolves.
package scan

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
)

// Folder is a project folder with a compose file.
type Folder struct {
	Dir  string // absolute
	Path string // as it goes into hl.yml: the projects entry as written, joined with the subfolder
	Name string // the app name it gets (see AppName)
}

// Diff is how the projects folders and hl.yml's apps differ, from the file
// system alone (no Docker).
type Diff struct {
	New          []Folder // folders with a compose file that no app uses
	Gone         []string // apps inside a projects folder whose folder no longer exists
	Kept         []string // apps inside a projects folder whose folder is still there
	MissingRoots []string // projects entries (as written) that don't exist; their apps are left alone
}

// Changes reports whether sync has anything to look at beyond the kept apps.
func (d *Diff) Changes() int { return len(d.New) + len(d.Gone) }

// Depth is the configured scan depth, or DefaultDepth.
func Depth(cfg *config.Config) int {
	if cfg.ScanDepth > 0 {
		return cfg.ScanDepth
	}
	return DefaultDepth
}

// Ignored reports whether a folder name matches DefaultIgnore or hl.yml's ignore.
func Ignored(cfg *config.Config, name string) bool {
	for _, list := range [][]string{DefaultIgnore, cfg.Ignore} {
		for _, p := range list {
			if ok, _ := filepath.Match(p, name); ok {
				return true
			}
		}
	}
	return false
}

// Compare lists the project folders and compares them with hl.yml's apps.
func Compare(cfg *config.Config) (*Diff, error) {
	d := &Diff{}
	used := map[string]string{} // canonical app folder -> app name
	for _, n := range cfg.Names() {
		used[canonical(cfg.AppDir(cfg.Apps[n]))] = n
	}
	var roots []string
	seen := map[string]bool{}
	for i, written := range cfg.Projects {
		root := cfg.ProjectDirs()[i]
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			d.MissingRoots = append(d.MissingRoots, written)
			continue
		}
		roots = append(roots, root)
		found, err := folders(cfg, root, written, Depth(cfg))
		if err != nil {
			return nil, err
		}
		for _, f := range found {
			c := canonical(f.Dir)
			if _, ok := used[c]; ok || seen[c] {
				continue
			}
			seen[c] = true
			d.New = append(d.New, f)
		}
	}
	for _, n := range cfg.Names() {
		dir := cfg.AppDir(cfg.Apps[n])
		if !inside(roots, dir) {
			continue
		}
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			d.Gone = append(d.Gone, n)
		} else {
			d.Kept = append(d.Kept, n)
		}
	}
	sort.SliceStable(d.New, func(i, j int) bool { return d.New[i].Name < d.New[j].Name })
	return d, nil
}

// folders are the subfolders of dir (down to depth levels) with a compose
// file, skipping ignored names and folders with an IgnoreFile. A folder with
// a compose file is an app; hl doesn't look inside it.
func folders(cfg *config.Config, dir, written string, depth int) ([]Folder, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Folder
	for _, e := range entries {
		name := e.Name()
		sub := filepath.Join(dir, name)
		if st, err := os.Stat(sub); err != nil || !st.IsDir() { // follows symlinks
			continue
		}
		if Ignored(cfg, name) {
			continue
		}
		if _, err := os.Stat(filepath.Join(sub, IgnoreFile)); err == nil {
			continue
		}
		path := filepath.Join(written, name)
		if _, err := compose.FindFile(sub); err == nil {
			out = append(out, Folder{Dir: sub, Path: path, Name: AppName(name)})
			continue
		}
		if depth > 1 {
			more, err := folders(cfg, sub, path, depth-1)
			if err != nil {
				return nil, err
			}
			out = append(out, more...)
		}
	}
	return out, nil
}

// AppName turns a folder name into an app name: lowercase, other characters
// than letters, digits, '.', '_' and '-' become '-'. "" if nothing is left.
func AppName(folder string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(folder) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.TrimLeft(b.String(), "._-")
	if config.ValidName(name) != nil {
		return ""
	}
	return name
}

// canonical is dir with symlinks resolved when possible, so two spellings of
// one folder match.
func canonical(dir string) string {
	if c, err := filepath.EvalSymlinks(dir); err == nil {
		return c
	}
	return filepath.Clean(dir)
}

func inside(roots []string, dir string) bool {
	for _, r := range roots {
		for _, pair := range [][2]string{{r, dir}, {canonical(r), canonical(dir)}} {
			if rel, err := filepath.Rel(pair[0], pair[1]); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				return true
			}
		}
	}
	return false
}
