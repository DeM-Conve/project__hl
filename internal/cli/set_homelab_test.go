package cli

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeM-Conve/project__hl/internal/config"
)

// The homelab's own settings (projects, ignore, strip_prefix, name) through hl set
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
	r = f.must("set", "projects-=~/elsewhere")
	contains(t, "remove unknown", r.out, "projects has no ~/elsewhere", "nothing to change")

	f.must("set", "ignore+=old,scratch-*", "strip_prefix=project__")
	r = f.hl("set", "ignore+=[")
	if r.code != 2 || !strings.Contains(r.errOut, "not a valid pattern") {
		t.Errorf("bad pattern: %s", r)
	}
	f.must("set", "ignore-=old")
	cfg := load()
	if strings.Join(cfg.Ignore, ",") != "scratch-*" || strings.Join(cfg.StripPrefix, ",") != "project__" {
		t.Errorf("ignore = %q, strip_prefix = %q", cfg.Ignore, cfg.StripPrefix)
	}
	text := f.read(filepath.Join(lab, config.FileName))
	contains(t, "hl.yml", text, "# my homelab", "# none yet")
	if strings.Index(text, "strip_prefix") > strings.Index(text, "apps:") {
		t.Errorf("new settings go above apps:\n%s", text)
	}
	r = f.must("show")
	contains(t, "show", r.out, "~/my code", "scratch-*", "project__")

	r = f.must("unset", "ignore", "strip_prefix")
	contains(t, "unset", r.out, "unset ignore", "apply it: hl sync")
	lacks(t, "hl.yml", f.read(filepath.Join(lab, config.FileName)), "ignore", "strip_prefix")
}

// scan_depth is a removed key: projects folders are looked into one level deep.
func TestScanDepthIsRemoved(t *testing.T) {
	f := newFake(t)
	lab := f.lab("lab", "version: 1\nscan_depth: 2\napps: {}\n")
	r := f.hl("status")
	contains(t, "status", r.errOut, "scan_depth: removed from hl", "`hl set projects+=<folder>`", "`hl unset scan_depth`")
	r = f.must("unset", "scan_depth")
	contains(t, "unset", r.out, "unset scan_depth", "note: scan_depth was not a known setting")
	if got := f.read(filepath.Join(lab, config.FileName)); got != "version: 1\napps: {}\n" {
		t.Errorf("hl.yml:\n%s", got)
	}
	f.must("status")
	if r := f.hl("set", "scan_depth=2"); r.code != 2 {
		t.Errorf("hl set scan_depth= should be refused: %s", r)
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
		{[]string{"set", "host=x"}, 2, []string{"host is an app setting", "hint: hl set <app> host=x"}},
		{[]string{"unset", "env", "port"}, 2, []string{"port is a proxy setting", "hint: hl unset proxy port"}},
		{[]string{"set", "a", "prot=80"}, 2, []string{`unknown setting "prot" - did you mean "port"?`}},
		{[]string{"set", "b", "port=80"}, 2, []string{`no app "b"`}},
		{[]string{"unset", "a", "prot"}, 2, []string{`unknown setting "prot"`}},
		{[]string{"set", "a", "port="}, 2, []string{"no value"}},
		{[]string{"set", "env.FOO"}, 2, []string{"error: env.FOO needs a value\nhint: hl set env.FOO=<value>\n"}},
		{[]string{"set", "scan_depth=2"}, 2, []string{`no app or homelab setting called "scan_depth"`}},
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
	lab := f.lab("lab", "version: 1\nproxy: {routing: host}\napps:\n  # the blog\n  a: {path: '"+a+"', port: 80} # mine\n  b: {path: '"+a+"', host: c.localhost}\n")
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
		{[]string{"set", "a", "name=c"}, 1, "c.localhost"}, // b's host: hl.yml's check refuses the clash
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

// hl unset removes a key hl.yml has that isn't a setting (a typo, or one an older hl took), at
// any scope, so a file every command refuses can be fixed without an editor.
func TestUnsetUnknownKeyInFile(t *testing.T) {
	f := newFake(t)
	dir := f.app("blog", `{"services":{"web":{"ports":[{"target":80}]}}}`)
	lab := f.lab("lab", "# my lab\n\nversion: 1\nprojects_old: [x]\nenv: {TZ: UTC}\n\nproxy:\n  port: 8080\n  prot: 1\n\napps:\n  blog:\n    path: "+dir+"\n    servce: web\n")
	if r := f.hl("status"); r.code != 1 || !strings.Contains(r.errOut, "prot") {
		t.Fatalf("an unknown key should be refused: %s", r)
	}
	r := f.must("unset", "proxy", "prot")
	contains(t, "unset proxy prot", r.out, "unset proxy.prot", "note: proxy.prot was not a known setting; removed it from hl.yml")
	lacks(t, "unset proxy prot", r.out, "apply it")
	r = f.must("unset", "blog", "servce")
	contains(t, "unset blog servce", r.out, "note: apps.blog.servce was not a known setting")
	r = f.must("unset", "projects_old")
	contains(t, "unset projects_old", r.out, "note: projects_old was not a known setting")
	lacks(t, "unset projects_old", r.out, "apply it")
	if got := f.read(filepath.Join(lab, "hl.yml")); got != "# my lab\n\nversion: 1\nenv: {TZ: UTC}\n\nproxy:\n  port: 8080\n\napps:\n  blog:\n    path: "+dir+"\n" {
		t.Errorf("hl.yml:\n%s", got)
	}
	f.must("status")

	// A key that is neither a setting nor in the file is still refused, and so is a key hl manages.
	for _, args := range [][]string{{"unset", "proxy", "prot"}, {"unset", "blog", "servce"}, {"unset", "projects_old"}, {"unset", "apps"}, {"unset", "env.TZ", "version"}} {
		if r := f.hl(args...); r.code != 2 {
			t.Errorf("%v: want a usage error: %s", args, r)
		}
	}
	if r := f.must("unset", "proxy", "port"); strings.Contains(r.out, "note:") {
		t.Errorf("a known setting needs no note: %s", r)
	}
}

// A set or unset that changes nothing says so, leaves hl.yml alone and names nothing to apply;
// with other changes, only those are applied.
func TestSetNoChange(t *testing.T) {
	f := newFake(t)
	code := filepath.Join(f.home, "my code")
	dir := f.app("blog", `{"services":{"web":{"ports":[{"target":80}]}}}`)
	if err := os.MkdirAll(code, 0o755); err != nil {
		t.Fatal(err)
	}
	lab := f.lab("lab", "version: 1\nprojects: ['~/my code']\nignore: [old]\nproxy:\n  port: 8080\napps:\n  blog:\n    path: '"+dir+"'\n    port: 80\n")
	path := filepath.Join(lab, "hl.yml")
	before := f.read(path)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"set", "proxy", "port=8080"}, "proxy.port is already 8080"},
		{[]string{"set", "blog", "port=80"}, "apps.blog.port is already 80"},
		{[]string{"set", "projects+=" + code}, "projects already has"},
		{[]string{"set", "ignore-=new"}, "ignore has no new"},
		{[]string{"set", "ignore+=old"}, "ignore already has old"},
		{[]string{"unset", "proxy", "bind"}, "proxy.bind isn't set"},
		{[]string{"set", "blog", "name=blog"}, "blog is already called blog"},
	} {
		r := f.must(c.args...)
		contains(t, strings.Join(c.args, " "), r.out, c.want, "nothing to change")
		lacks(t, strings.Join(c.args, " "), r.out, "apply it", "set ")
	}
	if got := f.read(path); got != before {
		t.Errorf("hl.yml changed:\n%s", got)
	}
	if st, err := os.Stat(path); err != nil || !st.ModTime().Equal(old) {
		t.Errorf("hl.yml was written again: %v", err)
	}
	// One change among no-ops: only it is set and applied.
	r := f.must("set", "proxy", "port=8080", "bind=0.0.0.0")
	contains(t, "mixed", r.out, "proxy.port is already 8080", "set proxy.bind = 0.0.0.0", "apply it: hl up proxy\n")
}

// hl set proxy subnet= refuses a subnet that overlaps another Docker network or this machine's,
// with a free one in the hint; the proxy's own network doesn't count (hl down, hl up recreate it).
func TestSetSubnetChecksOverlap(t *testing.T) {
	f := newFake(t)
	lab := f.lab("lab", "version: 1\nproxy:\n  subnet: 172.30.0.0/16\napps: {}\n")
	before := f.read(filepath.Join(lab, "hl.yml"))
	f.on(rule{Match: []string{"network ls"}, Stdout: "n1\nn2\nn3\n"})
	f.on(rule{Match: []string{"network inspect", "n1"}, Stdout: "bridge\t172.17.0.0/16 \nmine\t10.200.5.0/24 \nhl-proxy\t172.30.0.0/16 \n"})
	r := f.hl("set", "proxy", "subnet=10.200.0.0/16")
	if r.code != 2 {
		t.Fatalf("an overlapping subnet: want exit 2: %s", r)
	}
	contains(t, "overlap", r.errOut, "proxy.subnet 10.200.0.0/16 overlaps Docker network mine (10.200.5.0/24)", "`hl set proxy subnet=172.31.0.0/16`")
	hostSubnets = func() []*net.IPNet {
		_, n, _ := net.ParseCIDR("192.168.1.0/24")
		return []*net.IPNet{n}
	}
	r = f.hl("set", "proxy", "subnet=192.168.0.0/16")
	if r.code != 2 {
		t.Fatalf("a host subnet: want exit 2: %s", r)
	}
	contains(t, "host overlap", r.errOut, "overlaps this machine's network 192.168.1.0/24")
	if got := f.read(filepath.Join(lab, "hl.yml")); got != before {
		t.Errorf("hl.yml changed:\n%s", got)
	}
	// A free one is taken, even though the proxy's network still has the old subnet.
	r = f.must("set", "proxy", "subnet=172.29.0.0/16")
	contains(t, "free subnet", r.out, "set proxy.subnet = 172.29.0.0/16", "apply it: hl down, then hl up")
}
