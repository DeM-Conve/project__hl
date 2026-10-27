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
	if r.code != 1 || !strings.Contains(r.errOut, `"~/elsewhere" is not in it (~/my code)`) {
		t.Errorf("remove unknown: %s", r)
	}

	f.must("set", "ignore+=old,scratch-*", "strip_prefix=project__", "scan_depth=2")
	r = f.hl("set", "ignore+=[")
	if r.code != 1 || !strings.Contains(r.errOut, "not a valid pattern") {
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
	if r := f.hl("set", "scan_depth+=1"); r.code != 1 || !strings.Contains(r.errOut, "not a list") {
		t.Errorf("+= on a number: %s", r)
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
