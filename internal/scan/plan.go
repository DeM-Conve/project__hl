package scan

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
)

// Kind is what sync does with one app.
type Kind string

const (
	Add    Kind = "add"    // a new folder: add it to hl.yml
	Remove Kind = "remove" // its folder is gone: remove it from hl.yml (data stays)
	Update Kind = "update" // its pinned service or port is gone: pin the detected ones
	Skip   Kind = "skip"   // something to tell the user, nothing changes
)

// Action is one line of the plan.
type Action struct {
	Kind    Kind
	Name    string
	Path    string // the app's folder as in hl.yml
	Service string // Add/Update: the service and port to pin
	Port    int
	Old     string // Update: what was pinned, for display
	Reason  string // Skip: why, and what to do
}

// ModelFunc resolves an app's compose model (`docker compose config`).
type ModelFunc func(name string, a *config.App) (*compose.Model, error)

// Plan is what sync would change: adds, then removes, then updates, then skips.
func Plan(cfg *config.Config, d *Diff, model ModelFunc) []Action {
	adds := PlanAdds(cfg, d, model)
	var out, skips []Action
	for _, a := range adds {
		if a.Kind == Skip {
			skips = append(skips, a)
		} else {
			out = append(out, a)
		}
	}
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

// PlanAdds is the part of the plan for new folders: an Add with the detected
// service and port, or a Skip saying why it can't be added.
func PlanAdds(cfg *config.Config, d *Diff, model ModelFunc) []Action {
	var out []Action
	taken := map[string]bool{}
	for n := range cfg.Apps {
		taken[n] = true
	}
	for _, f := range d.New {
		act := Action{Kind: Add, Name: f.Name, Path: f.Path}
		switch {
		case f.Name == "":
			act.Kind, act.Name = Skip, f.Path
			act.Reason = fmt.Sprintf("no usable app name - hl add %s --name <name>", quote(f.Dir))
		case reserved(f.Name):
			act.Kind = Skip
			act.Reason = fmt.Sprintf("%s is a name hl keeps for itself - hl add %s --name <other>", f.Name, quote(f.Dir))
		case taken[f.Name]:
			act.Kind = Skip
			act.Reason = fmt.Sprintf("an app named %s already exists - hl add %s --name <other>", f.Name, quote(f.Dir))
		default:
			app := &config.App{Path: f.Path}
			svc, port, err := detect(cfg, f.Name, app, model)
			if err != nil {
				act.Kind = Skip
				act.Reason = fmt.Sprintf("%v; add it with hl add %s", err, quote(f.Dir))
			}
			act.Service, act.Port = svc, port
		}
		if act.Kind == Add {
			taken[f.Name] = true
		}
		out = append(out, act)
	}
	return out
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
			Reason: fmt.Sprintf("service %s is gone and %v - set apps.%s.service in hl.yml", a.Service, err, name)}
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
