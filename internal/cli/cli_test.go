package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/interrupt"
	"github.com/DeM-Conve/project__hl/internal/scan"
	"github.com/DeM-Conve/project__hl/internal/version"
)

// The journey of a fresh user: init, add, up, status, down - and a second
// homelab next to the first, with nothing leaking between them.
func TestFreshUserJourney(t *testing.T) {
	f := newFake(t)
	blog := f.app("blog", webModel2)
	lab := filepath.Join(f.home, "my homelab")

	r := f.must("init", lab, "--yes")
	contains(t, "init", r.out, lab)
	if b := f.read(config.DefaultHomeFile()); strings.TrimSpace(b) != lab {
		t.Errorf("default homelab = %q", b)
	}
	if !strings.HasPrefix(config.DefaultHomeFile(), f.home) {
		t.Errorf("hl state outside the user's config dir: %s", config.DefaultHomeFile())
	}
	t.Setenv("HL_HOME", "") // from here on, the default recorded by init

	r = f.must("add", blog)
	contains(t, "add", r.out, "added blog (service web, port 80)", "url: http://localhost/blog/")
	contains(t, "hl.yml", f.read(filepath.Join(lab, "hl.yml")), "blog:", "service: web", "port: 80", "# hl homelab.")

	f.reset()
	r = f.must("up")
	ups := f.find("compose", "up --detach")
	if len(ups) != 2 || !strings.Contains(ups[0].String(), "--project-name hl-proxy") || !strings.Contains(ups[1].String(), "--project-name hl-blog") {
		t.Fatalf("want the proxy, then blog, started: %v", ups)
	}
	app := ups[1]
	override := filepath.Join(lab, ".hl", "blog", "override.yml")
	wantArgs := []string{"compose", "--project-name", "hl-blog", "--project-directory", blog,
		"--file", filepath.Join(blog, "compose.yml"), "--file", override, "up", "--detach"}
	if strings.Join(app.Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Errorf("args (no shell, paths with spaces as one argument each):\n got %q\nwant %q", app.Args, wantArgs)
	}
	if app.Dir != blog || app.Env["HL_URL"] != "http://localhost/blog" || app.Env["HL_DATA"] != filepath.Join(lab, "data", "blog") {
		t.Errorf("app runs in %s with %v", app.Dir, app.Env)
	}
	contains(t, "override", f.read(override), "device: '"+filepath.Join(lab, "data", "blog", "pgdata")+"'", "PathPrefix(`/blog/`)")
	if _, err := os.Stat(filepath.Join(lab, "data", "blog", "pgdata")); err != nil {
		t.Errorf("data folder not created: %v", err)
	}
	contains(t, "up's status", r.out, "APP", "blog")

	// A second homelab with the same app name: its projects are its own when it has a name.
	lab2 := f.lab("second lab", "name: lab2\nproxy: {port: 8081}\napps:\n  blog: {path: '"+blog+"'}\n")
	f.reset()
	f.must("up")
	ups = f.find("compose", "up --detach")
	if len(ups) != 2 || !strings.Contains(ups[0].String(), "--project-name hl-lab2-proxy") || !strings.Contains(ups[1].String(), "--project-name hl-lab2-blog") {
		t.Fatalf("named homelab projects: %v", ups)
	}
	contains(t, "lab2's override", f.read(filepath.Join(lab2, ".hl", "blog", "override.yml")), "name: 'hl-lab2-proxy'", filepath.Join(lab2, "data"))
	lacks(t, "lab2's override", f.read(filepath.Join(lab2, ".hl", "blog", "override.yml")), filepath.Join(lab, "data"))

	// Without a name, the first homelab's running project is refused, not taken over.
	f.lab("third lab", "proxy: {port: 8082}\napps:\n  blog: {path: '"+blog+"'}\n")
	f.containers(ctr("hl-blog", "web", "running", "Up 1 minute", override))
	f.reset()
	r = f.hl("up", "blog")
	if r.code != 1 {
		t.Fatalf("want a refusal: %s", r)
	}
	contains(t, "clash", r.errOut, "belongs to another homelab ("+lab+")", "hint:", "hl set name=")
	if ups := f.find("--project-name hl-blog", "up"); len(ups) > 0 {
		t.Errorf("the other homelab's app was started: %v", ups)
	}
	r = f.must("down")
	contains(t, "down", r.errOut, "left alone")
	if downs := f.find("--project-name hl-blog", "down"); len(downs) > 0 {
		t.Errorf("the other homelab's app was stopped: %v", downs)
	}
}

func TestAddSettings(t *testing.T) {
	f := newFake(t)
	dir := f.app("My Tool", `{"services":{"ui":{"ports":[{"target":8080}]},"web":{"ports":[{"target":80}]},"worker":{}}}`)
	if err := os.WriteFile(filepath.Join(dir, "compose.prod.yml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lab := f.lab("lab", "apps: {}\n")

	r := f.must("add", dir, "name=tool", "routing=host", "host=tool.example.com",
		"service=ui", "port=8081", "compose=compose.yml,compose.prod.yml", "prefix=/t")
	contains(t, "add", r.out, "added tool (service ui, port 8081)", "http://tool.example.com/")
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Apps["tool"]
	if a.Routing != "host" || a.Host != "tool.example.com" || a.Service != "ui" || a.Port != 8081 || len(a.Compose) != 2 || a.Prefix != "/t" {
		t.Errorf("app = %+v", a)
	}
	if !strings.Contains(a.Path, "my code") {
		t.Errorf("path = %q", a.Path)
	}
	call := f.find("config --format json")[0]
	contains(t, "compose files", call.String(), "--file "+filepath.Join(dir, "compose.prod.yml"))

	// Several published services: without a terminal to ask on, the usual web name, with a note.
	// The folder name becomes a valid app name.
	r = f.must("add", dir)
	contains(t, "add", r.out, "note: several services publish ports (ui, web); picked web - add service=<name> to route another",
		"added my-tool (service web, port 80)", "hint: if its links break under /my-tool: hl set my-tool env.<BASE_PATH_VAR>='${HL_PREFIX}' (or routing=host)")

	r = f.must("add", dir, "name=db", "routing=none", "compose=compose.prod.yml")
	contains(t, "add none", r.out, "not behind the proxy")

	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"add", dir, "name=tool"}, 1, `app "tool" is already added`},
		{[]string{"add", dir, "name=x2"}, 1, "is already added as app"},
		{[]string{"add", dir, "name=Bad Name"}, 1, "use lowercase letters"},
		{[]string{"add", dir, "name=proxy"}, 1, "reserved"},
		{[]string{"add", dir, "name=x3", "compose=c.yml", "service=nope"}, 1, "compose file"},
		{[]string{"add", dir, "name=x4", "compose=compose.yml", "service=nope"}, 1, `no service "nope"`},
		{[]string{"add", dir, "name=x5", "compose=compose.yml", "routing=sideways"}, 1, "is not path, host or none"},
		{[]string{"add", filepath.Join(f.home, "missing")}, 1, "not found"},
		{[]string{"add"}, 2, "requires at least 1 arg"},
		{[]string{"add", dir, "port=x"}, 1, "not a whole number"},
		{[]string{"add", dir, "colour=red"}, 1, `unknown setting "colour"`},
		{[]string{"add", dir, "path=x"}, 2, "path is the folder"},
		{[]string{"add", dir, "port"}, 2, "use key=value"},
		{[]string{"add", dir, "port="}, 2, "no value"},
		{[]string{"add", dir, "--service", "ui"}, 2, "unknown flag"},
	} {
		r := f.hl(c.args...)
		if r.code != c.code || !strings.Contains(r.errOut, c.want) {
			t.Errorf("hl %v: want exit %d and %q, got %s", c.args, c.code, c.want, r)
		}
	}
	two := f.app("two", `{"services":{"a":{"ports":[{"target":1}]},"b":{"ports":[{"target":2}]}}}`)
	r = f.hl("add", two)
	contains(t, "several services", r.errOut, "several services publish ports (a, b) and none has a usual web name (frontend, web, app, nginx, proxy, ui) - name the one to route, e.g. service=a")
}

func TestAddMapsEnv(t *testing.T) {
	f := newFake(t)
	dir := f.app("notes", apiModel)
	f.vars(dir, "NOTES_BASE_PATH", "NOTES_URL", "UPSTREAM_URL", "DATA_LOCATION", "BACKUP_LOCATION", "DB_DATA_LOCATION", "TZ")
	lab := f.lab("lab", "apps: {}\n")

	r := f.must("add", dir)
	contains(t, "add", r.out, "env NOTES_BASE_PATH=${HL_PREFIX}", "env NOTES_URL=${HL_URL}",
		"env DATA_LOCATION=${HL_DATA}", "env BACKUP_LOCATION=${HL_DATA}/backups")
	lacks(t, "add", r.out, "UPSTREAM_URL", "DB_DATA_LOCATION", "hint:")
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(scan.EnvList(cfg.Apps["notes"].Env), " "); got !=
		"BACKUP_LOCATION=${HL_DATA}/backups DATA_LOCATION=${HL_DATA} NOTES_BASE_PATH=${HL_PREFIX} NOTES_URL=${HL_URL}" {
		t.Errorf("env = %s", got)
	}
	// The mapped values reach compose as interpolation variables.
	f.reset()
	f.must("show", "notes", "--compose")
	c := f.find("config")[0]
	if c.Env["HL_PREFIX"] != "/notes" {
		t.Errorf("HL_* env = %v", c.Env)
	}

	// env.NAME= leaves one variable out; host routing doesn't map the base path.
	vars := []string{"NOTES_BASE_PATH", "NOTES_URL", "TZ"}
	plain, hosted, old := f.app("plain", apiModel), f.app("hosted", apiModel), f.app("old", apiModel)
	for _, d := range []string{plain, hosted, old} {
		f.vars(d, vars...)
	}
	r = f.must("add", plain, "env.NOTES_URL=", "routing=host")
	lacks(t, "add env.NOTES_URL=", r.out, "env ")
	r = f.must("add", hosted, "routing=host")
	contains(t, "add host", r.out, "env NOTES_URL=${HL_URL}")
	lacks(t, "add host", r.out, "NOTES_BASE_PATH")
	cfg, _ = config.Load(lab)
	if cfg.Apps["plain"].Env != nil {
		t.Errorf("plain env = %v", cfg.Apps["plain"].Env)
	}

	// env.NAME=value adds values and wins over the mapped ones; a bad one changes nothing.
	extra := f.app("extra", apiModel)
	f.vars(extra, vars...)
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"add", extra, "env.NOPE"}, 2, `"env.NOPE": use key=value`},
		{[]string{"add", extra, "=x"}, 2, "use key=value"},
		{[]string{"add", extra, "env.BAD NAME=x"}, 1, "not a valid variable name"},
	} {
		if r := f.hl(c.args...); r.code != c.code || !strings.Contains(r.errOut, c.want) {
			t.Errorf("hl %v: want exit %d and %q, got %s", c.args, c.code, c.want, r)
		}
	}
	r = f.must("add", extra, "env.NOTES_URL=https://notes.example.com", "env.TZ=Europe/Berlin", "env.EMPTY=")
	contains(t, "add env", r.out, "env NOTES_URL=https://notes.example.com", "env TZ=Europe/Berlin", "env NOTES_BASE_PATH=${HL_PREFIX}")
	cfg, _ = config.Load(lab)
	if e := cfg.Apps["extra"].Env; e["NOTES_URL"] != "https://notes.example.com" || e["TZ"] != "Europe/Berlin" ||
		e["NOTES_BASE_PATH"] != "${HL_PREFIX}" || len(e) != 3 {
		t.Errorf("extra env = %v", e)
	}
	// Every mapped variable left out: only the given ones.
	flagged := f.app("flagged", apiModel)
	f.vars(flagged, vars...)
	f.must("add", flagged, "env.NOTES_BASE_PATH=", "env.NOTES_URL=", "env.TZ=UTC")
	cfg, _ = config.Load(lab)
	if e := cfg.Apps["flagged"].Env; len(e) != 1 || e["TZ"] != "UTC" {
		t.Errorf("flagged env = %v", e)
	}

	// Compose too old to list variables: added anyway, with a warning.
	f.on(rule{Match: []string{"config --variables"}, Stderr: "unknown flag: --variables", Exit: 1})
	r = f.must("add", old)
	contains(t, "old compose", r.errOut, "env not mapped")
}

// hl add warns about a service that doesn't look like a web app, as init and sync skip one.
func TestAddWarnsAboutNonWebApps(t *testing.T) {
	f := newFake(t)
	db := f.app("db", `{"services":{"postgres":{"ports":[{"target":5432,"published":"5432"}]}}}`)
	f.lab("lab", "apps: {}\n")
	r := f.must("add", db)
	contains(t, "add db", r.errOut, "warning: not a web app? (service postgres, port 5432) - run it without the proxy: hl set db routing=none")
	lacks(t, "add db", r.out, "if its links break")
	r = f.must("add", f.app("web", apiModel))
	lacks(t, "add web", r.errOut, "not a web app")
	r = f.must("add", f.app("db2", `{"services":{"postgres":{"ports":[{"target":5432}]}}}`), "routing=none")
	lacks(t, "add db routing=none", r.errOut, "not a web app")
}

func TestRemove(t *testing.T) {
	f := newFake(t)
	blog := f.app("blog", webModel2)
	lab := f.lab("lab", "apps:\n  blog: {path: '"+blog+"'}\n  gone: {path: /nowhere/gone}\n")
	if err := os.MkdirAll(filepath.Join(lab, "data", "blog"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.containers(
		ctr("hl-blog", "web", "running", "Up", filepath.Join(lab, ".hl", "blog", "override.yml")),
		ctr("hl-gone", "web", "running", "Up", filepath.Join(lab, ".hl", "gone", "override.yml")),
	)
	r := f.must("remove", "blog")
	contains(t, "remove", r.out, "its data is still in "+filepath.Join(lab, "data", "blog"))
	if len(f.find("--project-name hl-blog", "down")) != 1 {
		t.Errorf("blog not stopped: %v", f.calls())
	}
	// A folder that is gone is stopped by its project name alone.
	r = f.must("remove", "gone")
	contains(t, "remove gone", r.errOut, "stopping it by its project name")
	if d := f.find("--project-name hl-gone", "down"); len(d) != 1 || strings.Contains(d[0].String(), "--file") {
		t.Errorf("gone: %v", d)
	}
	cfg, _ := config.Load(lab)
	if len(cfg.Apps) != 0 {
		t.Errorf("apps left: %v", cfg.Names())
	}
	r = f.hl("remove", "blog")
	if r.code != 1 || !strings.Contains(r.errOut, "no apps yet") {
		t.Errorf("removing twice: %s", r)
	}
}

func TestStatusShowsHealth(t *testing.T) {
	f := newFake(t)
	a, b := f.app("a", webModel2), f.app("b", apiModel)
	c, d := f.app("c", apiModel), f.app("d", apiModel)
	lab := f.lab("lab", "proxy: {port: 8080}\napps:\n  a: {path: '"+a+"'}\n  b: {path: '"+b+"'}\n  c: {path: '"+c+"'}\n  d: {path: '"+d+"', routing: none}\n  e: {path: '"+d+"'}\n")
	cf := func(app string) string { return filepath.Join(lab, ".hl", app, "override.yml") }
	f.containers(
		ctr("hl-proxy", "traefik", "running", "Up 2 hours (healthy)", filepath.Join(lab, ".hl", "proxy", "compose.yml")),
		ctr("hl-a", "web", "running", "Up 2 hours (healthy)", cf("a")),
		ctr("hl-a", "db", "running", "Up 2 hours (unhealthy)", cf("a")),
		ctr("hl-b", "api", "running", "Up 3 seconds (health: starting)", cf("b")),
		ctr("hl-c", "api", "running", "Up 2 hours (healthy)", cf("c")),
		ctr("hl-d", "api", "exited", "Exited (1) 1 minute ago", cf("d")),
	)
	r := f.must("status")
	contains(t, "status", r.out, "proxy running  127.0.0.1:8080",
		"APP  STATE", "HEALTH",
		"a    up 2/2       unhealthy: db",
		"b    up 1/1       starting: api",
		"c    up 1/1       healthy",
		"d    partial 0/1  unhealthy: api exited  -",
		"e    down", "http://localhost:8080/e/")

	// Docker down: hl.yml is still shown, states unknown, with what to do.
	f.on(rule{Match: []string{"ps --all"}, Stderr: "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?\n", Exit: 1})
	f.rules = f.rules[len(f.rules)-1:]
	f.on(f.rules[0])
	r = f.hl("status")
	if r.code != 0 {
		t.Fatalf("status without docker should still work: %s", r)
	}
	contains(t, "status without docker", r.errOut, "can't ask Docker", "start Docker")
	contains(t, "status without docker", r.out, "proxy unknown", "a    ?")
}

// After `hl set proxy port=` without `hl up proxy`, status shows where the proxy really
// listens, the URLs on that port, and how to apply hl.yml's.
func TestStatusShowsTheRunningProxyAddress(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	lab := f.lab("lab", "proxy: {port: 8088}\napps:\n  a: {path: '"+a+"'}\n")
	f.containers(
		ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")),
		ctr("hl-a", "web", "running", "Up", filepath.Join(lab, ".hl", "a", "override.yml")),
	)
	f.on(rule{Match: []string{"ps --filter label=com.docker.compose.project=hl-proxy --format {{.Ports}}"}, Stdout: "127.0.0.1:8080->80/tcp\n"})
	r := f.must("status")
	contains(t, "status", r.out, "proxy running  127.0.0.1:8080",
		"note: hl.yml says 127.0.0.1:8088 - apply it: hl up proxy, then hl up", "http://localhost:8080/a/")
	lacks(t, "status", r.out, "localhost:8088")

	// Applied: no note, hl.yml's port.
	f = newFake(t)
	lab = f.lab("lab", "proxy: {port: 8088}\napps:\n  a: {path: '"+a+"'}\n")
	f.containers(ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")))
	f.on(rule{Match: []string{"--format {{.Ports}}"}, Stdout: "0.0.0.0:8088->80/tcp, [::]:8088->80/tcp\n"})
	r = f.must("status")
	contains(t, "status", r.out, "proxy running  127.0.0.1:8088", "http://localhost:8088/a/")
	lacks(t, "status", r.out, "note:")
}

func TestUpRestartRebuild(t *testing.T) {
	f := newFake(t)
	a, b := f.app("a", webModel2), f.app("b", apiModel)
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n  b: {path: '"+b+"'}\n")
	proxyFile := filepath.Join(lab, ".hl", "proxy", "compose.yml")

	// A stopped app isn't restarted (compose would do nothing): hl says so and starts nothing.
	r := f.must("restart", "b")
	contains(t, "restart of a stopped app", r.out, "b isn't running - start it with: hl up b")
	lacks(t, "restart of a stopped app", r.out, "==> b")
	if calls := f.find("docker compose"); len(calls) != 0 {
		t.Errorf("restart of a stopped app ran compose: %v", calls)
	}

	f.containers(ctr("hl-b", "api", "running", "Up", filepath.Join(lab, ".hl", "b", "override.yml")))
	f.reset()
	f.must("restart", "b")
	calls := f.find("docker compose --project-name")
	if len(calls) < 2 || !strings.Contains(calls[0].String(), "hl-proxy") || !strings.Contains(calls[len(calls)-1].String(), "--project-name hl-b") ||
		!strings.HasSuffix(calls[len(calls)-1].String(), " restart") {
		t.Errorf("restart starts the proxy first, then restarts b only: %v", calls)
	}

	// With the proxy running, it is left alone; rebuild builds.
	f.rules = nil
	f.containers(ctr("hl-proxy", "traefik", "running", "Up", proxyFile))
	f.reset()
	f.must("rebuild", "a", "a")
	calls = f.find("compose", "up")
	if len(calls) != 1 || !strings.HasSuffix(calls[0].String(), "up --detach --build") {
		t.Errorf("rebuild once, the proxy untouched: %v", calls)
	}

	// One broken app doesn't stop the others; the summary names it.
	if err := os.Remove(filepath.Join(a, "compose.yml")); err != nil {
		t.Fatal(err)
	}
	f.reset()
	r = f.hl("up")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "up", r.errOut, "a: no compose file in "+a, "hint: add a compose file", "1 of 2 apps failed: a")
	if len(f.find("--project-name hl-b", "up --detach")) != 1 {
		t.Error("b was not started")
	}

	// Up twice is fine: compose does nothing the second time.
	f.must("up", "b")
	f.must("up", "b")
}

// An image keeps the build args it was built with: up rebuilds the services whose resolved
// build section changed since the app last came up (say a base path from ${HL_PREFIX}).
func TestUpRebuildsChangedBuildArgs(t *testing.T) {
	f := newFake(t)
	model := func(base string) string {
		return `{"services":{"web":{"ports":[{"target":80}],"build":{"context":"/src","args":{"BASE_PATH":"` + base + `"}}},` +
			`"worker":{"build":{"context":"/src/worker"}},"db":{}}}`
	}
	a := f.app("a", model("/a"))
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n")
	state := filepath.Join(lab, ".hl", "a", "builds.json")

	// The first up builds what is missing, as compose does, and records the build settings.
	f.must("up", "a")
	if len(f.find("compose", " build")) != 0 {
		t.Errorf("first up: no separate build: %v", f.calls())
	}
	contains(t, "builds.json", f.read(state), `"web"`, `"worker"`)
	lacks(t, "builds.json", f.read(state), `"db"`)

	// Nothing changed: nothing is rebuilt.
	f.reset()
	f.must("up", "a")
	if b := f.find("compose", " build"); len(b) != 0 {
		t.Errorf("unchanged: %v", b)
	}

	// The prefix changed: only web is rebuilt, before up.
	if err := os.WriteFile(filepath.Join(a, "model.json"), []byte(model("/g")), 0o644); err != nil {
		t.Fatal(err)
	}
	f.reset()
	r := f.must("up", "a")
	contains(t, "up", r.out, "build settings changed - rebuilding web")
	var order []string
	for _, c := range f.find("--project-name hl-a") {
		if s := c.String(); strings.HasSuffix(s, " build web") || strings.HasSuffix(s, "up --detach") || strings.Contains(s, " build ") {
			order = append(order, s[strings.LastIndex(s, ".yml ")+5:])
		}
	}
	if !slices.Equal(order, []string{"build web", "up --detach"}) {
		t.Errorf("want web rebuilt (not worker), then up: %q", order)
	}
	f.reset()
	f.must("up", "a")
	if b := f.find("compose", " build"); len(b) != 0 {
		t.Errorf("rebuilt again: %v", b)
	}
}

func TestUpWait(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"', routing: none}\n")
	f.on(rule{Match: []string{"--project-name hl-a", "up --detach --wait --wait-timeout 30"}, Stderr: "container hl-a-db-1 is unhealthy\n", Exit: 1})
	f.on(rule{Match: []string{"--project-name hl-a", "logs"}, Stdout: "db | FATAL: password authentication failed\n"})
	f.containers(
		ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")),
		ctr("hl-a", "web", "running", "Up (healthy)", filepath.Join(lab, ".hl", "a", "override.yml")),
		ctr("hl-a", "db", "running", "Up (unhealthy)", filepath.Join(lab, ".hl", "a", "override.yml")),
	)
	// An app that isn't behind the proxy: --wait waits for every service in the one up call.
	r := f.hl("up", "--wait", "--wait-timeout", "30s")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "up --wait", r.errOut, "a: not running and healthy: db", "hint: see `hl logs a db`")
	contains(t, "up --wait", r.out, "last 20 log lines of a/db", "password authentication failed")
	if l := f.find("logs --no-color --tail 20 db"); len(l) != 1 {
		t.Errorf("logs of the unhealthy service: %v", f.calls())
	}
	if u := f.find("--project-name hl-a ", "up --detach --wait --wait-timeout 30"); len(u) != 1 || strings.HasSuffix(u[0].String(), " web") {
		t.Errorf("--wait: want one up that waits for everything: %v", u)
	}
}

// --wait waits for the routed service and what it depends on, not for unrelated sidecars.
func TestUpWaitSkipsSidecars(t *testing.T) {
	f := newFake(t)
	// web (routed) -> api -> db; backup is a sidecar that depends on db but nothing depends on it.
	model := `{"services":{"web":{"ports":[{"target":80,"published":"8080"}],"depends_on":{"api":{"condition":"service_started"}}},` +
		`"api":{"depends_on":{"db":{"condition":"service_healthy"}}},"db":{},"backup":{"depends_on":{"db":{}}}}}`
	a := f.app("a", model)
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n")
	override := filepath.Join(lab, ".hl", "a", "override.yml")
	f.containers(
		ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")),
		ctr("hl-a", "web", "running", "Up (healthy)", override),
		ctr("hl-a", "api", "running", "Up", override),
		ctr("hl-a", "db", "running", "Up (healthy)", override),
		ctr("hl-a", "backup", "running", "Up 2 minutes (health: starting)", override),
	)
	// compose would fail waiting for the sidecar, but hl doesn't ask it to.
	f.on(rule{Match: []string{"--project-name hl-a ", "--wait", "backup"}, Stderr: "container hl-a-backup-1 is starting\n", Exit: 1})
	r := f.must("up", "a", "--wait", "--wait-timeout", "30s")
	contains(t, "up --wait", r.out, "waiting for api, db, web")
	ups := f.find("--project-name hl-a ", "up --detach")
	if len(ups) != 2 || strings.Contains(ups[0].String(), "--wait") ||
		!strings.HasSuffix(ups[1].String(), "up --detach --wait --wait-timeout 30 api db web") {
		t.Fatalf("want up (all services), then up --wait for web and its dependencies: %v", ups)
	}
	contains(t, "status after up", r.out, "starting: backup") // status still shows the sidecar

	// A dependency that doesn't come up is reported; the sidecar isn't.
	f = newFake(t)
	a = f.app("a", model)
	lab = f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n")
	override = filepath.Join(lab, ".hl", "a", "override.yml")
	f.containers(
		ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")),
		ctr("hl-a", "web", "running", "Up (healthy)", override),
		ctr("hl-a", "db", "running", "Up (unhealthy)", override),
		ctr("hl-a", "backup", "running", "Up (health: starting)", override),
	)
	f.on(rule{Match: []string{"--project-name hl-a ", "--wait", "api db web"}, Stderr: "container hl-a-db-1 is unhealthy\n", Exit: 1})
	r = f.hl("rebuild", "a", "--wait")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "rebuild --wait", r.errOut, "a: not running and healthy: db\n")
	lacks(t, "rebuild --wait", r.errOut+r.out, "a/backup")
	if u := f.find("--project-name hl-a ", "up --detach --build"); len(u) != 1 || strings.Contains(u[0].String(), "--wait") {
		t.Errorf("rebuild builds once, without waiting: %v", u)
	}

	// An app that isn't behind the proxy has no service that matters more: --wait waits for all.
	f = newFake(t)
	a = f.app("a", model)
	f.lab("lab", "apps:\n  a: {path: '"+a+"', routing: none}\n")
	f.must("up", "a", "--wait")
	if u := f.find("--project-name hl-a ", "up --detach --wait"); len(u) != 1 || strings.Contains(u[0].String(), " web") {
		t.Errorf("routing none: %v", u)
	}
}

// --wait returns once the app's route answers through the proxy: Traefik's own 404 means the
// route isn't loaded yet; any answer of the app (its own 404 too) means it is.
func TestUpWaitWaitsForTheRoute(t *testing.T) {
	answers := []struct {
		code int
		body string
	}{{404, "404 page not found\n"}, {404, "404 page not found\n"}, {404, "no such page"}}
	var n atomic.Int32
	var never atomic.Bool // the route never shows up
	var host, path atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host.Store(r.Host)
		path.Store(r.URL.Path)
		a := answers[min(int(n.Add(1))-1, len(answers)-1)]
		if never.Load() {
			a = answers[0]
		}
		w.WriteHeader(a.code)
		fmt.Fprint(w, a.body)
	}))
	defer srv.Close()
	_, port, _ := strings.Cut(srv.Listener.Addr().String(), ":")

	f := newFake(t)
	routeReady = probeRoute
	a := f.app("a", webModel2)
	lab := f.lab("lab", "proxy: {port: 8095}\napps:\n  a: {path: '"+a+"'}\n")
	f.containers(ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")))
	// The proxy really listens on the test server's port.
	f.on(rule{Match: []string{"--format {{.Ports}}"}, Stdout: "127.0.0.1:" + port + "->80/tcp\n"})
	r := f.must("up", "a", "--wait")
	contains(t, "up --wait", r.out, "waiting for the route http://localhost:8095/a/")
	if got := n.Load(); got != 3 {
		t.Errorf("want 3 requests (two proxy 404s, then the app's own 404), got %d", got)
	}
	if host.Load() != "localhost:8095" || path.Load() != "/a/" {
		t.Errorf("asked for %v%v", host.Load(), path.Load())
	}

	// A route that never shows up fails at the timeout, with what to check.
	never.Store(true)
	r = f.hl("up", "a", "--wait", "--wait-timeout", "1s")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "up --wait", r.errOut, "a: the proxy has no route to http://localhost:8095/a/ yet", "hl logs proxy")

	// Not behind the proxy: nothing to ask.
	f.lab("lab2", "apps:\n  a: {path: '"+a+"', routing: none}\n")
	n.Store(0)
	f.must("up", "a", "--wait")
	if n.Load() != 0 {
		t.Errorf("routing none asked the proxy")
	}
}

func TestShow(t *testing.T) {
	f := newFake(t)
	a, b := f.app("a", webModel2), f.app("b", apiModel)
	lab := f.lab("lab", "proxy: {port: 8082}\nenv: {TZ: UTC, LANG: C}\napps:\n"+
		"  a:\n    path: '"+a+"'\n    env: {TZ: Europe/Berlin, A_BASE: '${HL_PREFIX}'}\n"+
		"  b: {path: '"+b+"', routing: host, service: api, port: 3000}\n")

	r := f.must("show")
	contains(t, "show", r.out, "homelab", "folder  ~/lab  env ($HL_HOME)", "port", "8082", "hl.yml", "listen", "http://127.0.0.1:8082", "hl show proxy",
		"TZ=UTC", "LANG=C", "http://localhost:8082/a/", "http://b.localhost:8082/")
	lacks(t, "show", r.out, "bind", "dashboard_public") // settings left at their default are in hl show proxy

	r = f.must("show", "a")
	contains(t, "show a", r.out, "folder   ~/my code/a  hl.yml\n", "project", "hl-a", "compose.yml", "found in the folder",
		"service", "web", "detected", "port", "80", "routing", "default (proxy.routing)", "http://localhost:8082/a/",
		"prefix", "/a", filepath.Join(lab, "data", "a"),
		"HL_URL=http://localhost:8082/a", "A_BASE=/a", "apps.a.env: ${HL_PREFIX}",
		"TZ=Europe/Berlin", "apps.a.env, over the shared env", "LANG=C", "shared env")
	if n := len(f.find("compose", "up")); n != 0 {
		t.Errorf("show started something: %v", f.calls())
	}

	r = f.must("show", "b")
	contains(t, "show b", r.out, "api", "3000", "host", "b.localhost")
	lacks(t, "show b", r.out, "detected", "prefix")

	r = f.hl("show", "c")
	if r.code != 1 {
		t.Fatalf("unknown app: %s", r)
	}
	contains(t, "show c", r.errOut, `no app "c"`, "hint:")

	// An app whose compose config can't be read: hl.yml's values, and a note.
	f.on(rule{Match: []string{"--project-name hl-a", "config"}, Stderr: "yaml: broken\n", Exit: 1})
	r = f.must("show", "a")
	contains(t, "show a (broken)", r.out, "detected when the app starts", "note:")
}

func TestHostTimeZoneIsTheDefaultTZ(t *testing.T) {
	f := newFake(t)
	config.HostTimeZone = func() (string, string) { return "Asia/Kolkata", "host ($TZ)" }
	a, b := f.app("a", webModel2), f.app("b", apiModel)
	f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n  b: {path: '"+b+"', env: {TZ: UTC}}\n")
	f.must("up")
	if u := f.find("--project-name hl-a ", "up --detach"); len(u) != 1 || u[0].Env["TZ"] != "Asia/Kolkata" {
		t.Errorf("a gets the host's TZ: %v", u)
	}
	if u := f.find("--project-name hl-b ", "up --detach"); len(u) != 1 || u[0].Env["TZ"] != "UTC" {
		t.Errorf("b's own TZ wins: %v", u)
	}
	r := f.must("show")
	contains(t, "show", r.out, "TZ=Asia/Kolkata", "host ($TZ)")
	r = f.must("show", "a")
	contains(t, "show a", r.out, "TZ=Asia/Kolkata", "host ($TZ)")

	// A shared TZ wins over the host's, everywhere.
	f.lab("lab2", "env: {TZ: Europe/Paris}\napps:\n  a: {path: '"+a+"'}\n")
	r = f.must("show")
	contains(t, "show", r.out, "TZ=Europe/Paris")
	lacks(t, "show", r.out, "Asia/Kolkata")

	// Unknown host zone: no TZ, and defaults says so.
	config.HostTimeZone = func() (string, string) { return "", "" }
	f.lab("lab3", "apps: {}\n")
	r = f.must("show")
	contains(t, "show", r.out, "none: the host's time zone can't be told")
}

func TestDown(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n")
	// Nothing runs: down does nothing and succeeds, every time.
	f.must("down")
	f.must("down")
	if d := f.find("down"); len(d) != 0 {
		t.Errorf("nothing to stop, but: %v", d)
	}
	f.containers(
		ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")),
		ctr("hl-a", "web", "running", "Up", filepath.Join(lab, ".hl", "a", "override.yml")),
	)
	f.must("down", "a")
	if d := f.find(" down"); len(d) != 1 || !strings.Contains(d[0].String(), "hl-a") {
		t.Errorf("down a: %v", d)
	}
	f.reset()
	f.must("down")
	d := f.find(" down")
	if len(d) != 2 || !strings.Contains(d[1].String(), "hl-proxy") {
		t.Errorf("down: apps, then the proxy: %v", d)
	}
	// A homelab with no apps still stops its proxy.
	f.lab("empty", "apps: {}\n")
	f.reset()
	r := f.hl("down")
	if r.code != 0 {
		t.Errorf("%s", r)
	}
}

func TestRebuildPull(t *testing.T) {
	f := newFake(t)
	clean, dirty, plain, diverged := f.app("clean", apiModel), f.app("dirty", apiModel), f.app("plain", apiModel), f.app("diverged", apiModel)
	f.lab("lab", "apps:\n  clean: {path: '"+clean+"'}\n  dirty: {path: '"+dirty+"'}\n  plain: {path: '"+plain+"'}\n  diverged: {path: '"+diverged+"'}\n")
	f.on(rule{Name: "git", Match: []string{dirty, "status --porcelain"}, Stdout: " M main.go\n?? notes.txt\n"})
	f.on(rule{Name: "git", Match: []string{plain, "status"}, Stderr: "fatal: not a git repository (or any of the parent directories): .git\n", Exit: 128})
	f.on(rule{Name: "git", Match: []string{diverged, "pull"}, Stderr: "fatal: Not possible to fast-forward, aborting.\n", Exit: 128})

	r := f.hl("rebuild", "--pull")
	if r.code != 1 {
		t.Fatalf("a failed pull fails the command: %s", r)
	}
	// A folder hl may not pull is rebuilt from its own code; the reason is said once.
	contains(t, "rebuild --pull", r.out, "pulled and rebuilt: clean",
		"pull skipped, rebuilt from the code in the folder: dirty (uncommitted changes in 2 files); plain (not a git repository)")
	if n := strings.Count(r.out, "not a git repository"); n != 1 {
		t.Errorf("the reason is said %d times:\n%s", n, r.out)
	}
	contains(t, "rebuild --pull", r.errOut, "diverged: git pull --ff-only failed", "hl rebuild diverged --pull", "1 of 4 apps failed: diverged")
	if p := f.find("-C "+dirty, "pull"); len(p) != 0 {
		t.Errorf("pulled over uncommitted work: %v", p)
	}
	for _, n := range []string{"dirty", "plain"} {
		if u := f.find("--project-name hl-"+n, "up --detach --build"); len(u) != 1 {
			t.Errorf("%s not rebuilt: %v", n, u)
		}
	}
	if p := f.find("-C "+clean, "pull --ff-only"); len(p) != 1 {
		t.Errorf("clean app not pulled: %v", f.calls())
	}
	if u := f.find("--project-name hl-clean", "up --detach --build"); len(u) != 1 {
		t.Errorf("clean app not rebuilt")
	}
	if u := f.find("--project-name hl-diverged", "up"); len(u) != 0 {
		t.Errorf("diverged app rebuilt anyway")
	}

	// Without git: a clear error.
	if err := os.Remove(filepath.Join(f.bin, "git")); err != nil {
		t.Fatal(err)
	}
	r = f.hl("rebuild", "clean", "--pull")
	if r.code != 1 || !strings.Contains(r.errOut, "git not found") {
		t.Errorf("no git: %s", r)
	}
	// Without --pull, no git at all; the proxy's --pull pulls its image.
	f.reset()
	f.must("rebuild", "clean")
	f.must("rebuild", "proxy", "--pull")
	if u := f.find("--project-name hl-proxy", "up --detach --force-recreate --pull always"); len(u) != 1 {
		t.Errorf("rebuild proxy --pull: %v", f.calls())
	}
}

func TestLogs(t *testing.T) {
	f := newFake(t)
	a, b := f.app("a", webModel2), f.app("bb", apiModel)
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n  bb: {path: '"+b+"'}\n")
	f.on(rule{Match: []string{"--project-name hl-a ", "logs"}, Stdout: "web-1  | GET /\nweb-1  | GET /about\n"})
	f.on(rule{Match: []string{"--project-name hl-bb ", "logs"}, Stdout: "api-1  | ready\n"})

	// A stopped app (or proxy) has no containers: say so instead of printing nothing.
	r := f.must("logs", "a")
	contains(t, "logs of a stopped app", r.out, "a isn't running", "hl up a")
	r = f.must("logs", "proxy")
	contains(t, "logs of a stopped proxy", r.out, "proxy isn't running", "hl up proxy")
	r = f.must("logs")
	contains(t, "logs with nothing running", r.out, "no app is running", "hl up")
	if l := f.find("logs --tail"); len(l) != 0 {
		t.Errorf("compose logs ran for nothing: %v", l)
	}

	f.containers(ctr("hl-a", "web", "exited", "Exited (0) 1 minute ago", filepath.Join(lab, ".hl", "a", "override.yml")))
	r = f.must("logs", "a", "web", "-f", "--tail", "5")
	if l := f.find("--project-name hl-a ", "logs"); len(l) != 1 || !strings.HasSuffix(l[0].String(), "logs --tail 5 --follow web") {
		t.Errorf("logs args: %v", l)
	}
	contains(t, "logs a", r.out, "web-1  | GET /about")

	// Every app's logs: only the apps that have containers.
	f.reset()
	r = f.must("logs")
	contains(t, "all logs", r.out, "a | web-1  | GET /")
	if l := f.find("logs --tail 200 --no-color"); len(l) != 1 {
		t.Errorf("only a's logs: %v", f.calls())
	}
	f.rules = f.rules[:2] // the logs rules; a new inventory
	f.containers(ctr("hl-a", "web", "exited", "Exited (0) 1 minute ago", filepath.Join(lab, ".hl", "a", "override.yml")),
		ctr("hl-bb", "api", "running", "Up", filepath.Join(lab, ".hl", "bb", "override.yml")))
	f.reset()
	r = f.must("logs")
	contains(t, "all logs", r.out, "a  | web-1  | GET /", "bb | api-1  | ready")
	if l := f.find("logs --tail 200 --no-color"); len(l) != 2 {
		t.Errorf("every app's logs: %v", f.calls())
	}

	for _, args := range [][]string{{"logs", "a", "-x"}, {"logs", "a", "--", "-x"}} {
		if r := f.hl(args...); r.code != 2 {
			t.Errorf("hl %v: want a usage error, got %s", args, r)
		}
	}
	r = f.hl("logs", "nope")
	if r.code != 1 || !strings.Contains(r.errOut, `no app "nope"`) {
		t.Errorf("unknown app: %s", r)
	}
	// A service the app doesn't have: its services are listed.
	r = f.hl("logs", "a", "wbe")
	if r.code != 2 || !strings.Contains(r.errOut, `a has no service "wbe"`) || !strings.Contains(r.errOut, "hint: its services: db, web") {
		t.Errorf("unknown service: %s", r)
	}

	// The proxy logs nothing until debug is on: hl says how to see more.
	f.rules = nil
	f.containers(ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")))
	r = f.must("logs", "proxy")
	contains(t, "empty proxy logs", r.out, "hl set proxy debug=true")
	f.on(rule{Match: []string{"--project-name hl-proxy", "logs"}, Stdout: "traefik-1  | an error\n"})
	r = f.must("logs", "proxy")
	lacks(t, "proxy logs", r.out, "debug=true")
}

func TestShellOpenCompose(t *testing.T) {
	f := newFake(t)
	a, db := f.app("a", webModel2), f.app("db", apiModel)
	lab := f.lab("lab", "proxy: {port: 8080}\napps:\n  a: {path: '"+a+"'}\n  db: {path: '"+db+"', routing: none}\n")
	f.containers(
		ctr("hl-proxy", "traefik", "running", "Up 1 hour", filepath.Join(lab, ".hl", "proxy", "compose.yml")),
		ctr("hl-a", "web", "running", "Up 1 hour", filepath.Join(lab, ".hl", "a", "override.yml")),
		ctr("hl-a", "db", "running", "Up 1 hour", filepath.Join(lab, ".hl", "a", "override.yml")),
		ctr("hl-a", "api", "exited", "Exited (0) 1 hour ago", filepath.Join(lab, ".hl", "a", "override.yml")),
		ctr("other", "x", "running", "Up", "/elsewhere/compose.yml"),
	)
	f.must("shell", "a")
	if s := f.find("exec web sh -c"); len(s) != 1 || s[0].Args[len(s[0].Args)-1] != "command -v bash >/dev/null && exec bash || exec sh" {
		t.Errorf("shell: %v", s)
	}
	f.on(rule{Match: []string{"exec db"}, Exit: 3})
	if r := f.hl("shell", "a", "db"); r.code != 3 || r.errOut != "" {
		t.Errorf("the shell's own exit status: %s", r)
	}
	f.on(rule{Match: []string{"exec api"}, Stderr: "service \"api\" is not running\n", Exit: 1})
	r := f.hl("shell", "a", "api")
	// A stopped service: one message from hl, not docker's error line too.
	if r.code != 1 || strings.Count(r.errOut, "not running")+strings.Count(r.errOut, "isn't running") != 1 {
		t.Errorf("shell of a stopped service: %s", r)
	}
	contains(t, "shell not running", r.errOut, "a: service api isn't running", "hl up a")
	if s := f.find("exec api"); len(s) != 0 {
		t.Errorf("docker exec ran for a stopped service: %v", s)
	}
	if r := f.hl("shell", "db"); r.code != 2 || !strings.Contains(r.errOut, "which service") {
		t.Errorf("routing none needs a service: %s", r)
	}

	r = f.must("open", "a")
	if r.out != "http://localhost:8080/a/\n" {
		t.Errorf("open: %q", r.out)
	}
	if r := f.hl("open", "db"); r.code != 1 || !strings.Contains(r.errOut, "not behind the proxy") {
		t.Errorf("open db: %s", r)
	}
	// With a display, the browser is started (no shell).
	if err := os.Symlink(filepath.Join(f.bin, "docker"), filepath.Join(f.bin, "xdg-open")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISPLAY", ":0")
	goos = "linux"
	f.must("open", "a")
	deadline := time.Now().Add(5 * time.Second)
	for len(f.find("http://localhost:8080/a/")) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if o := f.find("http://localhost:8080/a/"); len(o) != 1 || o[0].Name != "xdg-open" {
		t.Errorf("browser: %v", f.calls())
	}

	f.reset()
	f.must("show", "a", "--compose")
	if c := f.find("--project-name hl-a", " config"); len(c) < 2 || !strings.HasSuffix(c[len(c)-1].String(), " config") {
		t.Errorf("show --compose: %v", c)
	}
	f.must("show", "proxy", "--compose")
	if c := f.find("--project-name hl-proxy", " config"); len(c) != 1 {
		t.Errorf("show proxy --compose: %v", f.calls())
	}
	if r := f.hl("show", "aa", "--compose"); r.code != 1 || !strings.Contains(r.errOut, `did you mean "a"`) {
		t.Errorf("close name: %s", r)
	}
	for _, gone := range []string{"ps", "path", "config", "update", "proxy"} {
		if r := f.hl(gone); r.code != 2 {
			t.Errorf("hl %s: want an unknown command: %s", gone, r)
		}
	}
}

func TestProxyIsAPseudoApp(t *testing.T) {
	f := newFake(t)
	lab := f.lab("lab", "proxy: {port: 8080, subnet: 172.29.0.0/16}\napps: {}\n")
	f.on(rule{Match: []string{"network ls"}, Stdout: "n1\nn2\n"})
	f.on(rule{Match: []string{"network inspect", "n1"}, Stdout: "bridge\t172.17.0.0/16 \nmine\t172.29.5.0/24 \n"})
	r := f.hl("up", "proxy")
	if r.code != 1 {
		t.Fatalf("subnet clash: %s", r)
	}
	contains(t, "subnet clash", r.errOut, "proxy.subnet 172.29.0.0/16 overlaps Docker network mine (172.29.5.0/24)", "`hl set proxy subnet=172.31.0.0/16`", "`hl unset proxy subnet`")

	f.lab("lab", "proxy: {port: 8080}\napps: {}\n")
	portInUse = func(bind string, port int) bool { return port == 8080 }
	f.on(rule{Match: []string{"ps --filter publish=8080"}, Stdout: "some-nginx\n"})
	r = f.hl("up", "proxy")
	if r.code != 1 {
		t.Fatalf("port in use: %s", r)
	}
	contains(t, "port in use", r.errOut, "port 8080 on 127.0.0.1 is already in use by Docker container some-nginx", "`hl set proxy port=8000`", "(8000 is free)")

	portInUse = func(string, int) bool { return false }
	f.reset()
	r = f.must("up", "proxy")
	if u := f.find("--project-name hl-proxy", "up --detach"); len(u) != 1 {
		t.Errorf("up proxy: %v", f.calls())
	}
	if u := f.find("compose", "up"); len(u) != 1 {
		t.Errorf("up proxy started more than the proxy: %v", u)
	}
	contains(t, "up proxy", r.out, "proxy", "no apps yet")
	contains(t, "proxy compose", f.read(filepath.Join(lab, ".hl", "proxy", "compose.yml")), "'127.0.0.1:8080:80'")
	lacks(t, "proxy compose", f.read(filepath.Join(lab, ".hl", "proxy", "compose.yml")), "api@internal")

	f.containers(ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")))
	f.reset()
	f.must("up", "proxy")
	f.must("restart", "proxy")
	f.must("rebuild", "proxy")
	f.must("logs", "proxy", "-f", "-n", "5")
	f.must("down", "proxy")
	for _, want := range [][]string{
		{"--project-name hl-proxy", "up --detach"},
		{"--project-name hl-proxy", "restart"},
		{"--project-name hl-proxy", "up --detach --force-recreate"},
		{"--project-name hl-proxy", "logs --tail 5 --follow"},
		{"--project-name hl-proxy", "down"},
	} {
		if c := f.find(want...); len(c) == 0 {
			t.Errorf("want a call with %q: %v", want, f.calls())
		}
	}

	// Moving the running proxy to a busy port is refused before compose replaces it.
	f.containers(ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")))
	f.on(rule{Match: []string{"ps --filter label=com.docker.compose.project=hl-proxy --format {{.Ports}}"}, Stdout: "127.0.0.1:8080->80/tcp\n"})
	f.lab("lab", "proxy: {port: 8000}\napps: {}\n")
	portInUse = func(bind string, port int) bool { return port == 8000 }
	f.reset()
	r = f.hl("up", "proxy")
	if r.code != 1 || len(f.find("compose", "up")) != 0 {
		t.Fatalf("a busy new port: want a refusal and no compose up: %s\n%v", r, f.calls())
	}
	contains(t, "busy new port", r.errOut, "port 8000 on 127.0.0.1 is already in use", "the running proxy is left as it is", "hint: stop it, or move the proxy")
	// The same port on another address the old one doesn't cover: checked too; on a wildcard: not.
	f.lab("lab", "proxy: {port: 8080, bind: 10.0.0.5}\napps: {}\n")
	portInUse = func(bind string, port int) bool { return bind == "10.0.0.5" }
	if r = f.hl("up", "proxy"); r.code != 1 {
		t.Fatalf("a busy new address: %s", r)
	}
	f.lab("lab", "proxy: {port: 8080, bind: 0.0.0.0}\napps: {}\n")
	portInUse = func(string, int) bool { return true }
	f.reset()
	f.must("up", "proxy")
	if len(f.find("--project-name hl-proxy", "up --detach")) != 1 {
		t.Errorf("same port, wildcard bind: compose up expected: %v", f.calls())
	}
	portInUse = func(string, int) bool { return false }
	f.lab("lab", "proxy: {port: 8080}\napps: {}\n")

	r = f.must("show", "proxy")
	contains(t, "show proxy", r.out, "listen", "http://127.0.0.1:8080", "port", "8080", "hl.yml", "image", "default", "traefik config")
	for _, args := range [][]string{{"proxy"}, {"proxy", "up"}, {"shell", "proxy"}, {"open", "proxy"}} {
		if r := f.hl(args...); r.code == 0 {
			t.Errorf("hl %v: want an error: %s", args, r)
		}
	}
	r = f.hl("shell", "proxy")
	contains(t, "shell proxy", r.errOut, "the proxy is not an app here", "hl logs proxy")

	out := f.must("__complete", "up", "").out
	contains(t, "complete up", out, "proxy")
	out = f.must("__complete", "logs", "proxy", "").out
	lacks(t, "complete logs proxy", out, "traefik")
}

func TestDoctor(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	f.lab("lab", "proxy: {bind: 0.0.0.0}\napps:\n  a: {path: '"+a+"'}\n  b: {path: /nowhere}\n")
	r := f.hl("doctor")
	if r.code != 1 {
		t.Fatalf("app b is broken: %s", r)
	}
	contains(t, "doctor", r.out, "hl ", "go1.", "docker 27.3.1", "docker daemon 27.3.1 on Fake Linux", "docker compose 2.29.1",
		"homelab ", "(2 apps)", "reachable from your network", "✓ app a", "✗ app b", "proxy port 0.0.0.0:80 is free")
	contains(t, "doctor", r.errOut, "1 problem found", "bug report")

	f.lab("lab2", "apps: {}\n")
	r = f.must("doctor")
	contains(t, "doctor", r.out, "all good")

	f.on(rule{Match: []string{"compose version"}, Stdout: "2.20.0\n"})
	f.on(rule{Match: []string{"info"}, Stderr: "Cannot connect to the Docker daemon\n", Exit: 1})
	r = f.hl("doctor")
	contains(t, "doctor", r.out, "✗ docker daemon", "start Docker", "docker compose 2.20.0 is too old")

	f.lab("lab3", "proxy: {port: x}\n")
	r = f.hl("doctor")
	contains(t, "doctor bad hl.yml", r.out, "hl.yml:1:15: proxy.port", "is not a whole number")
}

func TestDoctorChecksTheLatestRelease(t *testing.T) {
	f := newFake(t)
	f.lab("lab", "apps: {}\n")
	var latest atomic.Value // the server runs in its own goroutine
	var hits atomic.Int32
	latest.Store(`{"tag_name": "v0.2.0"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if latest.Load() == "" {
			http.Error(w, "rate limited", http.StatusForbidden)
			return
		}
		fmt.Fprint(w, latest.Load())
	}))
	defer srv.Close()
	oldURL, oldVersion := latestReleaseURL, version.Version
	latestReleaseURL = srv.URL
	t.Cleanup(func() { latestReleaseURL, version.Version = oldURL, oldVersion })

	version.Version = "v0.1.0"
	r := f.must("doctor")
	contains(t, "out of date", r.out, "! hl v0.1.0 is out of date: v0.2.0 is the latest release", "update: curl -fsSL", "/releases", "all good (1 warning)")

	latest.Store(`{"tag_name": "v0.1.0"}`)
	r = f.must("doctor")
	contains(t, "latest", r.out, "✓ hl v0.1.0 is the latest release")

	// GitHub not answering (offline, rate limit): nothing is said.
	latest.Store("")
	r = f.must("doctor")
	lacks(t, "no answer", r.out, "latest release", "out of date")

	// Builds that aren't releases ask nothing.
	hits.Store(0)
	version.Version = "v0.0.0-20261016123456-abcdef123456"
	r = f.must("doctor")
	contains(t, "pseudo-version", r.out, "is a build of a commit (go install), not a release", "install a release: curl")
	version.Version = "dev"
	r = f.must("doctor")
	lacks(t, "dev build", r.out, "release")
	if n := hits.Load(); n != 0 {
		t.Errorf("a build that isn't a release asked GitHub %d time(s)", n)
	}
}

func TestErrors(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	f.lab("lab", "proxy:\n  port: 8080\n  rooting: host\napps:\n  a: {path: '"+a+"', port: http}\n")
	r := f.hl("up")
	if r.code != 1 {
		t.Fatalf("%s", r)
	}
	contains(t, "bad hl.yml", r.errOut, "has 2 problems", `hl.yml:3:3: proxy.rooting: unknown key - did you mean "routing"?`, "hl.yml:5:", `apps.a.port: "http" is not a whole number`)

	f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n")
	for _, c := range []struct {
		args []string
		code int
		want []string
	}{
		{[]string{"sideways"}, 2, []string{`unknown command "sideways"`, "hl --help"}},
		{[]string{"up", "--nope"}, 2, []string{"unknown flag: --nope", "hint: see `hl up --help`"}},
		{[]string{"up", "b"}, 1, []string{`no app "b"`}},
		{[]string{"status", "x"}, 2, []string{"unknown command"}},
	} {
		r := f.hl(c.args...)
		if r.code != c.code {
			t.Errorf("hl %v: want exit %d: %s", c.args, c.code, r)
		}
		contains(t, strings.Join(c.args, " "), r.errOut, c.want...)
	}

	// Hints name the command that fixes a setting, not the file to edit.
	f.lab("removed", "proxy: {dashboard: true}\napps: {}\n")
	r = f.hl("status")
	contains(t, "removed key", r.errOut, "proxy.dashboard: removed from hl", "`hl unset proxy dashboard`")
	// proxy.network and proxy.project come from the homelab's name now; hl unset removes them.
	f.lab("removed2", "proxy: {network: mine, project: mine-proxy}\napps: {}\n")
	r = f.hl("status")
	contains(t, "removed name keys", r.errOut, "proxy.network: removed from hl", "proxy.project: removed from hl", "hl set name=")
	f.must("unset", "proxy", "network", "project")
	f.must("status")
	// env_files: secrets live in the app's own .env.
	f.lab("removed3", "apps:\n  a: {path: '"+a+"', env_files: [secrets.env]}\n")
	r = f.hl("status")
	contains(t, "removed app key", r.errOut, "apps.a.env_files: removed from hl", "`hl unset a env_files`")
	f.must("unset", "a", "env_files")
	f.must("status")
	if r := f.hl("set", "a", "env_files=x.env"); r.code == 0 {
		t.Errorf("hl set a env_files= should be refused: %s", r)
	}
	f.lab("hints", "proxy: {bind: 127.0.0.1}\napps:\n  gone: {path: /nowhere}\n  a: {path: '"+a+"', prefix: /x}\n  b: {path: '"+a+"', prefix: /x}\n")
	r = f.hl("status")
	contains(t, "validation hints", r.errOut, "`hl set b prefix=/<path>`")
	f.lab("hints2", "apps:\n  gone: {path: /nowhere}\n  a: {path: '"+a+"', compose: [nope.yml]}\n")
	r = f.hl("up")
	contains(t, "app hints", r.errOut, "`hl set gone path=<folder>`", "`hl remove gone`", "`hl set a compose=<file>[,<file>...]`", "`hl unset a compose`")
	lacks(t, "app hints", r.errOut, "in "+filepath.Join(f.home, "hints2", "hl.yml"))

	// Docker's errors are explained.
	f.on(rule{Match: []string{"up --detach"}, Stderr: "Error response from daemon: driver failed programming external connectivity: Bind for 127.0.0.1:80 failed: port is already allocated\n", Exit: 1})
	r = f.hl("up")
	contains(t, "port allocated", r.errOut, "starting the proxy: docker compose up failed (exit status 1)", "hint: another program already uses that port")

	f.on(rule{Match: []string{"ps --all"}, Stderr: "permission denied while trying to connect to the Docker daemon socket\n", Exit: 1})
	f.rules = f.rules[len(f.rules)-1:]
	f.on(f.rules[0])
	r = f.hl("up")
	contains(t, "permission", r.errOut, "permission denied", "docker group")

	// No docker at all.
	for _, n := range []string{"docker", "git"} {
		_ = os.Remove(filepath.Join(f.bin, n))
	}
	r = f.hl("up")
	if r.code != 1 {
		t.Fatalf("%s", r)
	}
	contains(t, "no docker", r.errOut, "docker not found on PATH", "install Docker")
	r = f.hl("doctor")
	contains(t, "doctor without docker", r.out, "docker is not installed")

	// No homelab.
	t.Setenv("HL_HOME", "")
	r = f.hl("status")
	contains(t, "no homelab", r.errOut, "no homelab found", "hl init")
	r = f.hl("--home", filepath.Join(f.home, "nothing here"), "status")
	contains(t, "no hl.yml", r.errOut, "no hl.yml in", "hl init")
}

func TestWindowsIsUnsupported(t *testing.T) {
	f := newFake(t)
	goos = "windows"
	r := f.hl("status")
	if r.code != 1 || !strings.Contains(r.errOut, "doesn't support Windows") || !strings.Contains(r.errOut, "WSL") {
		t.Errorf("%s", r)
	}
	f.must("version")
}

func TestVersionAndCompletion(t *testing.T) {
	f := newFake(t)
	r := f.must("version")
	contains(t, "version", r.out, "hl ", "go1.", "/")
	// One way to ask: no aliases, no --version flag, no --short.
	for _, args := range [][]string{{"--version"}, {"-v"}, {"version", "--short"}, {"ls"}, {"start"}, {"stop"}} {
		if r := f.hl(args...); r.code != 2 {
			t.Errorf("hl %v: want a usage error, got %s", args, r)
		}
	}
	for _, sh := range []string{"bash", "zsh", "fish", "powershell"} {
		if r := f.must("completion", sh); len(r.out) < 100 {
			t.Errorf("completion %s: %q", sh, r.out)
		}
	}
	a, b := f.app("alpha", webModel2), f.app("beta", apiModel)
	f.lab("lab", "apps:\n  alpha: {path: '"+a+"'}\n  beta: {path: '"+b+"'}\n")
	r = f.must("__complete", "up", "alpha", "")
	contains(t, "complete up", r.out, "beta")
	lacks(t, "complete up", r.out, "alpha")
	r = f.must("__complete", "logs", "alpha", "")
	contains(t, "complete services", r.out, "db\nweb\n")
	r = f.must("__complete", "open", "alpha", "")
	lacks(t, "complete open", r.out, "beta")
}

func TestInterruptStopsCleanly(t *testing.T) {
	f := newFake(t)
	a, b := f.app("a", webModel2), f.app("b", apiModel)
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n  b: {path: '"+b+"'}\n")
	f.containers(ctr("hl-a", "web", "running", "Up", filepath.Join(lab, ".hl", "a", "override.yml")))
	f.on(rule{Match: []string{"logs"}, Wait: true})
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan result)
	go func() { done <- f.hlCtx(ctx, "logs", "a", "-f") }()
	waitFor(t, f, "started")
	cancel(&interrupt.Error{Signal: syscall.SIGTERM})
	r := <-done
	if r.code != 128+int(syscall.SIGTERM) {
		t.Errorf("exit code: %s", r)
	}
	waitFor(t, f, "signal: terminated")
	contains(t, "interrupted", r.errOut, "interrupted")

	// Up over several apps stops after the interrupted one.
	f.reset()
	f.on(rule{Match: []string{"--project-name hl-a", "up --detach"}, Wait: true})
	ctx, cancel = context.WithCancelCause(context.Background())
	go func() { done <- f.hlCtx(ctx, "up") }()
	waitFor(t, f, "started")
	cancel(&interrupt.Error{Signal: syscall.SIGTERM})
	r = <-done
	if r.code != 143 || len(f.find("--project-name hl-b", "up")) != 0 {
		t.Errorf("b started after the interrupt: %s %v", r, f.calls())
	}
}

func waitFor(t *testing.T, f *fake, note string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, c := range f.calls() {
			if c.Note == note {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %q in %v", note, f.calls())
}

// A proxy setting changes every place hl uses it: URLs, the proxy's files,
// each app's override, and the commands it runs.
func TestSettingsReachEveryPlace(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	lab := f.lab("lab", "name: lab-b\nproxy: {port: 8443, domain: lab.example, bind: 127.0.0.2}\napps:\n  a: {path: '"+a+"'}\n")
	r := f.must("up")
	contains(t, "status", r.out, "127.0.0.2:8443", "http://lab.example:8443/a/")
	contains(t, "override", f.read(filepath.Join(lab, ".hl", "a", "override.yml")), "name: 'hl-lab-b-proxy'", "traefik.docker.network: 'hl-lab-b-proxy'")
	contains(t, "proxy compose", f.read(filepath.Join(lab, ".hl", "proxy", "compose.yml")), "'127.0.0.2:8443:80'", "name: 'hl-lab-b-proxy'", "name: hl-lab-b-proxy")
	contains(t, "traefik.yml", f.read(filepath.Join(lab, ".hl", "proxy", "traefik.yml")), "network: 'hl-lab-b-proxy'")
	if u := f.find("--project-name hl-lab-b-a ", "up --detach"); len(u) != 1 || u[0].Env["HL_URL"] != "http://lab.example:8443/a" {
		t.Errorf("app run: %v", u)
	}
	for _, c := range f.calls() {
		if strings.Contains(c.String(), "hl-proxy") || strings.Contains(c.String(), "--project-name hl-a ") {
			t.Errorf("a default name was used: %s", c)
		}
	}
}

// Folders the app's env points at inside its data folder exist before compose runs, so Docker
// doesn't create them owned by root; paths elsewhere are left alone.
func TestUpCreatesEnvDataFolders(t *testing.T) {
	f := newFake(t)
	blog := f.app("blog", webModel2)
	outside := filepath.Join(f.home, "elsewhere")
	lab := f.lab("lab", "apps:\n  blog:\n    path: '"+blog+"'\n    env:\n"+
		"      BACKUP_LOCATION: ${HL_DATA}/backups\n      MEDIA: ${HL_DATA}/media/../uploads\n      OTHER: '"+outside+"'\n")
	f.must("up", "blog")
	for _, d := range []string{"backups", "uploads"} {
		if _, err := os.Stat(filepath.Join(lab, "data", "blog", d)); err != nil {
			t.Errorf("data/blog/%s not created: %v", d, err)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Errorf("created %s, outside the app's data folder", outside)
	}
}

// An app removed from a projects folder is ignored, so hl sync doesn't add it back; adding it
// again by hand takes it out of ignore.
func TestRemoveIgnoresAndAddUnignores(t *testing.T) {
	f := newFake(t)
	blog := f.app("blog", webModel2)
	other := filepath.Join(f.home, "elsewhere")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	lab := f.lab("lab", "projects: ['"+filepath.Dir(blog)+"']\napps:\n  blog: {path: '"+blog+"'}\n  out: {path: '"+other+"'}\n")
	r := f.must("remove", "blog")
	contains(t, "remove", r.out, "added blog to ignore", "hl set ignore-=blog")
	cfg, _ := config.Load(lab)
	if strings.Join(cfg.Ignore, ",") != "blog" {
		t.Errorf("ignore = %v", cfg.Ignore)
	}
	r = f.must("remove", "out") // not in a projects folder: ignore is left alone
	lacks(t, "remove out", r.out, "ignore")

	r = f.must("add", blog)
	contains(t, "add", r.out, "removed blog from ignore")
	if cfg, _ := config.Load(lab); len(cfg.Ignore) != 0 {
		t.Errorf("ignore after add = %v", cfg.Ignore)
	}
}

// A clash with another homelab's proxy project or network names the fix: a name for this
// homelab (proxy.project and proxy.network are no settings any more).
func TestProxyClashHints(t *testing.T) {
	f := newFake(t)
	f.lab("lab", "apps: {}\n")
	f.on(rule{Match: []string{"ps -a --filter label=com.docker.compose.project=hl-proxy"}, Stdout: "hl-proxy-traefik-1|/elsewhere/.hl/proxy\n"})
	r := f.hl("show", "proxy")
	contains(t, "proxy clash", r.out, "run by another homelab (/elsewhere)", "`hl set name=<name>`")
	lacks(t, "proxy clash", r.out, "set proxy project", "set proxy network")

	g := newFake(t)
	g.lab("lab", "apps: {}\n")
	g.on(rule{Match: []string{"network inspect --format"}, Stdout: "\n"})
	r = g.hl("show", "proxy")
	contains(t, "network clash", r.out, "wasn't created by hl's proxy", "`hl set name=<name>`")
}
