package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/config"
)

// The homelab's own settings (projects, ignore, scan_depth, strip_prefix, name) through hl set
// and hl unset: lists are replaced with =, changed with += and -=.
func TestSetHomelabSettings(t *testing.T) {
	f := newFake(t)
	code := filepath.Join(f.home, "my code")
	more := filepath.Join(f.home, "more")
	for _, d := range []string{code, more} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lab := f.lab("lab", "# my homelab\nversion: 1\napps: {} # none yet\n")
	load := func() *config.Config {
		t.Helper()
		cfg, err := config.Load(lab)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}

	r := f.must("set", "projects="+code+",~/more")
	contains(t, "set projects", r.out, "set projects = ~/my code,~/more", "apply it: hl sync")
	if got := strings.Join(load().Projects, "|"); got != "~/my code|~/more" {
		t.Errorf("projects = %q", got)
	}
	f.must("set", "projects+=~/my code") // already there: no change
	if got := strings.Join(load().Projects, "|"); got != "~/my code|~/more" {
		t.Errorf("projects after adding one again = %q", got)
	}
	r = f.hl("set", "projects+="+filepath.Join(f.home, "nope"))
	if r.code != 1 || !strings.Contains(r.errOut, "not found") {
		t.Errorf("missing folder: %s", r)
	}
	// Removed by its absolute path too.
	f.must("set", "projects-="+more)
	if got := strings.Join(load().Projects, "|"); got != "~/my code" {
		t.Errorf("projects after -= = %q", got)
	}
	r = f.hl("set", "projects-=~/elsewhere")
	if r.code != 2 || !strings.Contains(r.errOut, `error: projects: "~/elsewhere" is not in it (~/my code)`) {
		t.Errorf("remove unknown: %s", r)
	}

	f.must("set", "ignore+=old,scratch-*", "strip_prefix=project__", "scan_depth=2")
	r = f.hl("set", "ignore+=[")
	if r.code != 2 || !strings.Contains(r.errOut, "not a valid pattern") {
		t.Errorf("bad pattern: %s", r)
	}
	f.must("set", "ignore-=old")
	cfg := load()
	if strings.Join(cfg.Ignore, ",") != "scratch-*" || strings.Join(cfg.StripPrefix, ",") != "project__" || cfg.ScanDepth != 2 {
		t.Errorf("ignore = %q, strip_prefix = %q, scan_depth = %d", cfg.Ignore, cfg.StripPrefix, cfg.ScanDepth)
	}
	text := f.read(filepath.Join(lab, config.FileName))
	contains(t, "hl.yml", text, "# my homelab", "# none yet")
	if strings.Index(text, "scan_depth") > strings.Index(text, "apps:") {
		t.Errorf("new settings go above apps:\n%s", text)
	}
	r = f.must("show")
	contains(t, "show", r.out, "~/my code", "scratch-*", "scan_depth", "2", "project__")

	r = f.must("unset", "ignore", "strip_prefix", "scan_depth")
	contains(t, "unset", r.out, "unset ignore", "apply it: hl sync")
	lacks(t, "hl.yml", f.read(filepath.Join(lab, config.FileName)), "ignore", "strip_prefix", "scan_depth")
	if r := f.hl("set", "scan_depth+=1"); r.code != 2 || !strings.Contains(r.errOut, "scan_depth: is not a list") {
		t.Errorf("+= on a number: %s", r)
	}
}

// hl set <app> path= takes a folder relative to the current one, like hl add, and writes it
// like hl init writes folders (relative inside the homelab, ~/ inside the home).
func TestSetPathRelativeToCurrentFolder(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	b := f.app("b", webModel2)
	lab := f.lab("lab", "version: 1\napps:\n  a: {path: '"+a+"'}\n")
	sub := filepath.Join(lab, "sub")
	if err := os.MkdirAll(filepath.Join(lab, "apps", "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lab, "apps", "c", "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)
	rel, err := filepath.Rel(sub, b)
	if err != nil {
		t.Fatal(err)
	}
	r := f.must("set", "a", "path="+rel)
	contains(t, "set path", r.out, "set apps.a.path = ~/my code/b")
	if cfg, err := config.Load(lab); err != nil || cfg.AppDir(cfg.Apps["a"]) != b {
		t.Fatalf("path: %v %+v", err, cfg.Apps["a"])
	}
	f.must("set", "a", "path=../apps/c")
	if cfg, _ := config.Load(lab); cfg.Apps["a"].Path != filepath.Join("apps", "c") {
		t.Errorf("inside the homelab: %q", cfg.Apps["a"].Path)
	}
	if r := f.hl("set", "a", "path=nope"); r.code != 1 || !strings.Contains(r.errOut, filepath.Join(sub, "nope")) {
		t.Errorf("a missing folder is named as resolved: %s", r)
	}
}

func TestSetNameOnlyWhileNothingRuns(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n")
	f.containers(ctr("hl-a", "web", "running", "Up", filepath.Join(lab, ".hl", "a", "override.yml")))
	r := f.hl("set", "name=lab2")
	if r.code != 1 {
		t.Fatalf("want a refusal: %s", r)
	}
	contains(t, "running", r.errOut, "a still run under the old names", "hint: stop them first: hl down")
	lacks(t, "hl.yml", f.read(filepath.Join(lab, config.FileName)), "name:")

	// Another homelab, with nothing running.
	lab = f.lab("other lab", "apps:\n  b: {path: '"+a+"'}\n")
	r = f.must("set", "name=lab2")
	contains(t, "set name", r.out, "set name = lab2", "apply it: hl up (the apps and the proxy start under the new names)")
	if cfg, err := config.Load(lab); err != nil || cfg.Name != "lab2" || cfg.ProjectName("b") != "hl-lab2-b" {
		t.Errorf("name: %v %+v", err, cfg)
	}
	f.must("unset", "name")
	lacks(t, "hl.yml", f.read(filepath.Join(lab, config.FileName)), "name:")
}

// Mistakes in hl set/unset: unknown keys and apps are usage errors (exit 2) naming the scope
// that has the key; a missing folder or service is refused before hl.yml is written.
func TestSetUnsetMistakes(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	lab := f.lab("lab", "version: 1\nproxy:\n  port: 8080\napps:\n  a: {path: '"+a+"'}\n")
	before := f.read(filepath.Join(lab, config.FileName))
	for _, c := range []struct {
		args []string
		code int
		want []string
	}{
		{[]string{"unset", "nosuchkey"}, 2, []string{`no app or homelab setting called "nosuchkey"`}},
		{[]string{"set", "nosuch"}, 2, []string{`no app or homelab setting called "nosuch"`}},
		{[]string{"set", "routing=host"}, 2, []string{"routing is a proxy setting", "hint: hl set proxy routing=host"}},
		{[]string{"set", "prefix=/x"}, 2, []string{"prefix is an app setting", "hint: hl set <app> prefix=/x"}},
		{[]string{"unset", "env", "port"}, 2, []string{"port is a proxy setting", "hint: hl unset proxy port"}},
		{[]string{"set", "a", "prot=80"}, 2, []string{`unknown setting "prot" - did you mean "port"?`}},
		{[]string{"set", "b", "port=80"}, 2, []string{`no app "b"`}},
		{[]string{"unset", "a", "prot"}, 2, []string{`unknown setting "prot"`}},
		{[]string{"set", "a", "port="}, 2, []string{"no value"}},
		// A wrong value: the full key, the fix as the hint, no place in hl.yml.
		{[]string{"set", "a", "routing=hots"}, 2, []string{"error: apps.a.routing: \"hots\" is not path, host or none\nhint: did you mean \"host\"?\n"}},
		{[]string{"set", "a", "port=0"}, 2, []string{"error: apps.a.port: 0 is not a port\nhint: use a number from 1 to 65535\n"}},
		{[]string{"set", "proxy", "debug=maybe"}, 2, []string{`error: proxy.debug: "maybe" is not true or false`}},
		{[]string{"set", "a", "path=/no/such/folder"}, 1, []string{"app folder /no/such/folder not found"}},
		{[]string{"set", "a", "compose=nope.yml"}, 1, []string{"nope.yml not found"}},
		{[]string{"set", "a", "service=nosuch"}, 1, []string{`no service "nosuch"`, "hint: services: db, web"}},
	} {
		r := f.hl(c.args...)
		if r.code != c.code {
			t.Errorf("%v: want exit %d: %s", c.args, c.code, r)
		}
		contains(t, strings.Join(c.args, " "), r.errOut, c.want...)
		if c.code == 2 {
			lacks(t, strings.Join(c.args, " "), r.errOut, config.FileName+":")
		}
	}
	if f.read(filepath.Join(lab, config.FileName)) != before {
		t.Error("a refused change was written")
	}
	f.must("set", "a", "service=db", "port=5432")

	// Without a readable compose config the service is left to hl up.
	f.on(rule{Match: []string{"config --format json"}, Exit: 1, Stderr: "no docker"})
	f.must("set", "a", "service=other")

	// The last proxy setting gone: no empty proxy: left.
	f.must("unset", "proxy", "port")
	lacks(t, "hl.yml", f.read(filepath.Join(lab, config.FileName)), "proxy")
}

// hl set <app> name=<new> renames an app: settings, comments, data and generated files move.
func TestRenameApp(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	lab := f.lab("lab", "version: 1\napps:\n  # the blog\n  a: {path: '"+a+"', port: 80} # mine\n  b: {path: '"+a+"', prefix: /c}\n")
	for _, d := range []string{filepath.Join(lab, "data", "a", "pgdata"), filepath.Join(lab, ".hl", "a")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Running: refused, nothing moves.
	f.containers(ctr("hl-a", "web", "running", "Up", filepath.Join(lab, ".hl", "a", "override.yml")))
	r := f.hl("set", "a", "name=c")
	if r.code != 1 {
		t.Fatalf("want a refusal while it runs: %s", r)
	}
	contains(t, "running", r.errOut, "a has containers", "hint: stop it first: hl down a")
	f.rules = nil
	f.containers()

	for _, bad := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"set", "a", "name=proxy"}, 1, "reserved"},
		{[]string{"set", "a", "name=B"}, 1, "use lowercase letters"},
		{[]string{"set", "a", "name=b"}, 1, `app "b" exists already`},
		{[]string{"set", "a", "name=c", "port=81"}, 2, "give it on its own"},
		{[]string{"set", "a", "name=c"}, 1, "/c"}, // b's prefix: hl.yml's check refuses the clash
	} {
		if r := f.hl(bad.args...); r.code != bad.code || !strings.Contains(r.errOut, bad.want) {
			t.Errorf("%v: want exit %d and %q: %s", bad.args, bad.code, bad.want, r)
		}
	}
	if err := os.MkdirAll(filepath.Join(lab, "data", "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := f.hl("set", "a", "name=d"); r.code != 1 || !strings.Contains(r.errOut, "exists already") {
		t.Errorf("data folder taken: %s", r)
	}
	if !strings.Contains(f.read(filepath.Join(lab, config.FileName)), "  a: {") || !exists(filepath.Join(lab, "data", "a", "pgdata")) {
		t.Fatal("a refused rename changed something")
	}

	r = f.must("set", "a", "name=e")
	contains(t, "rename", r.out, "renamed apps.a to apps.e", "moved its data to "+filepath.Join(lab, "data", "e"), "apply it: hl up e")
	y := f.read(filepath.Join(lab, config.FileName))
	contains(t, "hl.yml", y, "# the blog", "e: {path: '"+a+"', port: 80} # mine")
	lacks(t, "hl.yml", y, "  a: {")
	if !exists(filepath.Join(lab, "data", "e", "pgdata")) || exists(filepath.Join(lab, "data", "a")) || !exists(filepath.Join(lab, ".hl", "e")) {
		t.Error("data and .hl/ didn't move")
	}
}
