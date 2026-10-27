package scan

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// Kind is what sync does with one app.
type Kind string

const (
	Add    Kind = "add"    // a new folder: add it to hl.yml
	Remove Kind = "remove" // its folder is gone: remove it from hl.yml (data stays)
	Update Kind = "update" // its pinned service or port is gone: pin the detected ones
	Move   Kind = "move"   // its folder moved: point path at the new one (name and data stay)
	Skip   Kind = "skip"   // something to tell the user, nothing changes
)

// Action is one line of the plan.
type Action struct {
	Kind    Kind
	Name    string
	Path    string // the app's folder as in hl.yml
	Service string // Add/Update: the service and port to pin
	Port    int
	Old     string            // Update: what was pinned, for display; Move: the old path
	Reason  string            // Skip: why, and what to do
	Env     map[string]string // Add: env mapped from the compose file's variables (AutoEnv)
	Note    string            // Add: something to tell the user, e.g. why env wasn't mapped
}

// ModelFunc resolves an app's compose model (`docker compose config`).
type ModelFunc func(name string, a *config.App) (*compose.Model, error)

// VarsFunc lists the variables an app's compose files use (`docker compose config --variables`).
type VarsFunc func(name string, a *config.App) ([]string, error)

// Plan is what sync would change: adds, then moves, then removes, then updates, then skips. With vars
// set, each add carries the env AutoEnv maps; nil leaves env alone.
func Plan(cfg *config.Config, d *Diff, model ModelFunc, vars VarsFunc) []Action {
	model = cached(model)
	moves, d := planMoves(cfg, d, model)
	adds := PlanAdds(cfg, d, model, vars)
	var out, skips []Action
	for _, a := range adds {
		if a.Kind == Skip {
			skips = append(skips, a)
		} else {
			out = append(out, a)
		}
	}
	out = append(out, moves...)
	for _, n := range d.Gone {
		out = append(out, Action{Kind: Remove, Name: n, Path: cfg.Apps[n].Path})
	}
	for _, n := range d.Kept {
		a := planUpdate(cfg, n, model)
		switch {
		case a == nil:
		case a.Kind == Skip:
			skips = append(skips, *a)
		default:
			out = append(out, *a)
		}
	}
	return append(out, skips...)
}

// cached is model, asked at most once per app folder.
func cached(model ModelFunc) ModelFunc {
	type res struct {
		m   *compose.Model
		err error
	}
	seen := map[string]res{}
	return func(name string, a *config.App) (*compose.Model, error) {
		key := a.Path + "\x00" + strings.Join(a.Compose, ",")
		if r, ok := seen[key]; ok {
			return r.m, r.err
		}
		m, err := model(name, a)
		seen[key] = res{m, err}
		return m, err
	}
}

// planMoves finds apps whose folder moved: an app whose folder is gone, and a new folder whose
// compose model exposes the same service on the same port as the app has pinned (and has the
// app's compose files, if it names any). Only a match that is unambiguous both ways counts: one
// new folder for the app and one app for the folder. It returns a Move per match and d without
// those apps and folders.
func planMoves(cfg *config.Config, d *Diff, model ModelFunc) ([]Action, *Diff) {
	if len(d.Gone) == 0 || len(d.New) == 0 {
		return nil, d
	}
	matches := map[string][]int{} // gone app -> new folders
	byFolder := map[int][]string{}
	for i, f := range d.New {
		if f.Name == "" {
			continue
		}
		for _, g := range d.Gone {
			a := cfg.Apps[g]
			if a.Service == "" || a.Port == 0 || cfg.Routing(a) == config.RoutingNone {
				continue // nothing pinned to recognise it by
			}
			if _, other := cfg.Apps[f.Name]; (other && f.Name != g) || !hasFiles(f.Dir, a.Compose) {
				continue // named like another app: a copy of that one, not g moved
			}
			// model is cached: one compose call per folder and compose list.
			na := &config.App{Path: f.Path, Compose: a.Compose}
			svc, port, err := detect(cfg, f.Name, na, model)
			if err != nil || svc != a.Service || port != a.Port {
				continue
			}
			if m, err := model(f.Name, na); err != nil || !hasVolumes(m, cfg.DataDir(g)) {
				continue
			}
			matches[g] = append(matches[g], i)
			byFolder[i] = append(byFolder[i], g)
		}
	}
	var moves []Action
	movedApp, movedFolder := map[string]bool{}, map[int]bool{}
	for _, g := range d.Gone {
		if fs := matches[g]; len(fs) == 1 && len(byFolder[fs[0]]) == 1 {
			f := d.New[fs[0]]
			moves = append(moves, Action{Kind: Move, Name: g, Path: f.Path, Old: cfg.Apps[g].Path})
			movedApp[g], movedFolder[fs[0]] = true, true
		}
	}
	if len(moves) == 0 {
		return nil, d
	}
	rest := &Diff{Kept: d.Kept, MissingRoots: d.MissingRoots}
	for i, f := range d.New {
		if !movedFolder[i] {
			rest.New = append(rest.New, f)
		}
	}
	for _, g := range d.Gone {
		if !movedApp[g] {
			rest.Gone = append(rest.Gone, g)
		}
	}
	return moves, rest
}

// hasVolumes reports whether m has a named volume for every folder in the app's data folder
// (data/<app>/<volume>), so a moved app finds its data again.
func hasVolumes(m *compose.Model, dataDir string) bool {
	entries, _ := os.ReadDir(dataDir)
	for _, e := range entries {
		if _, ok := m.Volumes[e.Name()]; e.IsDir() && !ok {
			return false
		}
	}
	return true
}

// hasFiles reports whether every file in files exists in dir.
func hasFiles(dir string, files []string) bool {
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return false
		}
	}
	return true
}

// PlanAdds is the part of the plan for new folders: an Add with the detected
// service and port, or a Skip saying why it can't be added.
func PlanAdds(cfg *config.Config, d *Diff, model ModelFunc, vars VarsFunc) []Action {
	var out []Action
	taken := map[string]bool{}
	for n := range cfg.Apps {
		taken[n] = true
	}
	for _, f := range d.New {
		act := Action{Kind: Add, Name: f.Name, Path: f.Path}
		dir := shellPath(f)
		switch {
		case f.Name == "":
			act.Kind, act.Name = Skip, f.Path
			act.Reason = fmt.Sprintf("no usable app name - hl add %s name=<name>", dir)
		case reserved(f.Name):
			act.Kind = Skip
			act.Reason = fmt.Sprintf("%s is a name hl keeps for itself - hl add %s name=<other>", f.Name, dir)
		case taken[f.Name]:
			act.Kind = Skip
			act.Reason = fmt.Sprintf("an app named %s already exists - hl add %s name=<other>", f.Name, dir)
		default:
			app := &config.App{Path: f.Path}
			svc, port, err := detect(cfg, f.Name, app, model)
			switch {
			case err != nil:
				act.Kind = Skip
				act.Reason = fmt.Sprintf("%v; add it with hl add %s", err, dir)
			case svc != "" && !LooksWeb(svc, port):
				act.Kind = Skip
				act.Reason = fmt.Sprintf("not a web app? (service %s, port %d) - run it without the proxy: hl add %s routing=none, "+
					"or skip it for good: hl set ignore+=%s", svc, port, dir, quote(filepath.Base(f.Dir)))
			}
			act.Service, act.Port = svc, port
			if act.Kind == Add && vars != nil {
				if vs, err := vars(f.Name, app); err != nil {
					act.Note = fmt.Sprintf("env not mapped: %v", err)
				} else {
					act.Env = AutoEnv(vs, cfg.Routing(app))
				}
			}
		}
		if act.Kind == Add {
			taken[f.Name] = true
		}
		out = append(out, act)
	}
	return out
}

// LooksWeb reports whether a service is likely served over HTTP: it has one of the names hl
// prefers (defaults.ExposedServices) or a common HTTP port (defaults.WebPorts).
func LooksWeb(service string, port int) bool {
	for _, s := range defaults.ExposedServices {
		if s == service {
			return true
		}
	}
	for _, p := range defaults.WebPorts {
		if p == port {
			return true
		}
	}
	return false
}

func detect(cfg *config.Config, name string, a *config.App, model ModelFunc) (string, int, error) {
	if cfg.Routing(a) == config.RoutingNone {
		return "", 0, nil
	}
	m, err := model(name, a)
	if err != nil {
		return "", 0, err
	}
	svc, err := m.Exposed()
	if err != nil {
		return "", 0, err
	}
	port, err := m.TargetPort(svc)
	return svc, port, err
}

// planUpdate checks a kept app's pinned service and port against its compose
// model: nil when nothing changed or nothing is pinned.
func planUpdate(cfg *config.Config, name string, model ModelFunc) *Action {
	a := cfg.Apps[name]
	if a.Service == "" || cfg.Routing(a) == config.RoutingNone {
		return nil // detected on every run, nothing pinned to go stale
	}
	m, err := model(name, a)
	if err != nil {
		return &Action{Kind: Skip, Name: name, Path: a.Path, Reason: fmt.Sprintf("can't read its compose file: %v", err)}
	}
	old := a.Service + ":" + strconv.Itoa(a.Port)
	s, ok := m.Services[a.Service]
	if !ok {
		svc, err := m.Exposed()
		if err == nil {
			var port int
			if port, err = m.TargetPort(svc); err == nil {
				return &Action{Kind: Update, Name: name, Path: a.Path, Service: svc, Port: port, Old: old}
			}
		}
		return &Action{Kind: Skip, Name: name, Path: a.Path,
			Reason: fmt.Sprintf("service %s is gone and %v - hl set %s service=<service>", a.Service, err, name)}
	}
	if a.Port == 0 || len(s.Ports) == 0 {
		return nil
	}
	for _, p := range s.Ports {
		if p.Target == a.Port {
			return nil
		}
	}
	port, err := m.TargetPort(a.Service)
	if err != nil {
		return nil
	}
	return &Action{Kind: Update, Name: name, Path: a.Path, Service: a.Service, Port: port, Old: old}
}

// shellPath is a new folder as hint commands show it: as it goes into hl.yml when that is
// short (~/code/blog), else the full path; quoted for a shell, leaving a leading ~/ to expand.
func shellPath(f Folder) string {
	if rest, ok := strings.CutPrefix(f.Path, "~/"); ok {
		return "~/" + quote(rest)
	}
	return quote(f.Dir)
}

// quote makes a folder safe to paste into a shell.
func quote(s string) string {
	if strings.ContainsAny(s, " \t'\"$\\`!*?[](){};&|<>#~") {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}

// reserved: a name config keeps for hl itself (config.ReservedAppNames).
func reserved(name string) bool {
	_, ok := config.IsReserved(name)
	return ok
}
