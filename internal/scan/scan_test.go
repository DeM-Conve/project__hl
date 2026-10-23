package scan

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// lab is a homelab and a projects folder in a temp home whose path has spaces.
type lab struct {
	t        *testing.T
	home     string
	projects string
	cfg      *config.Config
}

func newLab(t *testing.T, hlyml string) *lab {
	t.Helper()
	home := filepath.Join(t.TempDir(), "a home with spaces")
	t.Setenv("HOME", home)
	l := &lab{t: t, home: home, projects: filepath.Join(home, "my code")}
	l.mkdir("homelab")
	l.mkdir("my code")
	if err := os.WriteFile(filepath.Join(home, "homelab", config.FileName), []byte(hlyml), 0o644); err != nil {
		t.Fatal(err)
	}
	l.load()
	return l
}

func (l *lab) mkdir(rel string) string {
	d := filepath.Join(l.home, rel)
	if err := os.MkdirAll(d, 0o755); err != nil {
		l.t.Fatal(err)
	}
	return d
}

func (l *lab) app(rel string) {
	d := l.mkdir(rel)
	if err := os.WriteFile(filepath.Join(d, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		l.t.Fatal(err)
	}
}

func (l *lab) load() {
	c, err := config.Load(filepath.Join(l.home, "homelab"))
	if err != nil {
		l.t.Fatal(err)
	}
	l.cfg = c
}

func names(fs []Folder) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name+"="+f.Path)
	}
	return strings.Join(out, " ")
}

func TestCompareFindsNewAndGone(t *testing.T) {
	l := newLab(t, `
projects: ["~/my code", ../missing]
ignore: [scratch-*]
apps:
  blog: {path: ~/my code/blog}
  gone: {path: ../my code/gone}
  elsewhere: {path: /nowhere/at/all}
`)
	l.app("my code/blog")
	l.app("my code/New_Site")
	l.app("my code/scratch-1")
	l.app("my code/.hidden")
	l.app("my code/node_modules")
	l.app("my code/opted-out")
	if err := os.WriteFile(filepath.Join(l.projects, "opted-out", defaults.IgnoreFile), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	l.mkdir("my code/library")      // no compose file
	l.app("my code/group/deep-app") // below the default depth

	d, err := Compare(l.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(d.New); got != "new_site=~/my code/New_Site" {
		t.Errorf("New = %s", got)
	}
	if strings.Join(d.Gone, ",") != "gone" || strings.Join(d.Kept, ",") != "blog" {
		t.Errorf("Gone = %v, Kept = %v (elsewhere is outside projects and left alone)", d.Gone, d.Kept)
	}
	if strings.Join(d.MissingRoots, ",") != "../missing" {
		t.Errorf("MissingRoots = %v", d.MissingRoots)
	}

	l.cfg.ScanDepth = 2
	d, _ = Compare(l.cfg)
	if got := names(d.New); got != "deep-app=~/my code/group/deep-app new_site=~/my code/New_Site" {
		t.Errorf("depth 2: New = %s", got)
	}
}

func TestMissingProjectsFolderRemovesNothing(t *testing.T) {
	l := newLab(t, "projects: [../unmounted]\napps:\n  a: {path: ../unmounted/a}\n")
	d, err := Compare(l.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Gone) != 0 || len(d.MissingRoots) != 1 {
		t.Errorf("%+v", d)
	}
}

func TestAppName(t *testing.T) {
	for in, want := range map[string]string{"My App": "my-app", "my-app.io": "my-app.io", "_x": "x", "日本": "--"} {
		if got := AppName(in); got != want && !(want == "--" && got == "") {
			t.Errorf("AppName(%q) = %q, want %q", in, got, want)
		}
	}
}

func model(services map[string][]int) *compose.Model {
	m := &compose.Model{Services: map[string]compose.Service{}}
	for s, ports := range services {
		var ps []compose.Port
		for _, p := range ports {
			ps = append(ps, compose.Port{Target: p})
		}
		m.Services[s] = compose.Service{Ports: ps}
	}
	return m
}

func TestPlan(t *testing.T) {
	l := newLab(t, `
projects: ["~/my code"]
apps:
  same: {path: ~/my code/same, service: web, port: 80}
  moved-port: {path: ~/my code/moved-port, service: web, port: 80}
  renamed: {path: ~/my code/renamed, service: web, port: 80}
  unpinned: {path: ~/my code/unpinned}
  broken: {path: ~/my code/broken, service: web, port: 80}
  gone: {path: ~/my code/gone, service: web, port: 80}
`)
	for _, a := range []string{"same", "moved-port", "renamed", "unpinned", "broken", "new-one", "two-ui", "no-port", "Same", "Proxy"} {
		l.app("my code/" + a)
	}
	models := map[string]*compose.Model{
		"same":       model(map[string][]int{"web": {80}, "db": nil}),
		"moved-port": model(map[string][]int{"web": {3000}}),
		"renamed":    model(map[string][]int{"frontend": {8080}, "api": {9000}}),
		"new-one":    model(map[string][]int{"app": {5000}}),
		"two-ui":     model(map[string][]int{"a": {1}, "b": {2}}),
		"no-port":    model(map[string][]int{"worker": nil}),
	}
	calls := 0
	fn := func(name string, a *config.App) (*compose.Model, error) {
		calls++
		if name == "unpinned" {
			t.Error("an app without a pinned service needs no compose call")
		}
		if m, ok := models[name]; ok {
			return m, nil
		}
		return nil, errors.New("compose says no")
	}
	d, err := Compare(l.cfg)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range Plan(l.cfg, d, fn) {
		line := string(a.Kind) + " " + a.Name
		switch a.Kind {
		case Add, Update:
			line += " " + a.Service + ":" + strconv.Itoa(a.Port)
		case Skip:
			line += " (" + a.Reason + ")"
		}
		got = append(got, line)
	}
	want := []string{
		"add new-one app:5000",
		"remove gone",
		"update moved-port web:3000",
		"update renamed frontend:8080",
		"skip same (an app named same already exists",
		"skip proxy (proxy is a name hl keeps for itself",
		"skip no-port (no service publishes a port",
		"skip two-ui (several services publish ports",
		"skip broken (can't read its compose file: compose says no)",
	}
	all := strings.Join(got, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("plan lacks %q:\n%s", w, all)
		}
	}
	if len(got) != len(want) {
		t.Errorf("plan has %d actions, want %d:\n%s", len(got), len(want), all)
	}
	if !strings.HasPrefix(got[0], "add") || !strings.HasPrefix(got[len(got)-1], "skip") {
		t.Errorf("order: adds first, skips last:\n%s", all)
	}
}
