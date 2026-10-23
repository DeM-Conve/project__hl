package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/interrupt"
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
	contains(t, "hl.yml", f.read(filepath.Join(lab, "hl.yml")), "blog:", "service: web", "port: 80", "# 127.0.0.1 = this machine only")

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
	f.lab("third lab", "proxy: {port: 8082, project: hl-third-proxy, network: hl-third}\napps:\n  blog: {path: '"+blog+"'}\n")
	f.containers(ctr("hl-blog", "web", "running", "Up 1 minute", override))
	f.reset()
	r = f.hl("up", "blog")
	if r.code != 1 {
		t.Fatalf("want a refusal: %s", r)
	}
	contains(t, "clash", r.errOut, "belongs to another homelab ("+lab+")", "hint:", "name:")
	if ups := f.find("--project-name hl-blog", "up"); len(ups) > 0 {
		t.Errorf("the other homelab's app was started: %v", ups)
	}
	r = f.must("down")
	contains(t, "down", r.errOut, "left alone")
	if downs := f.find("--project-name hl-blog", "down"); len(downs) > 0 {
		t.Errorf("the other homelab's app was stopped: %v", downs)
	}
}

func TestAddFlags(t *testing.T) {
	f := newFake(t)
	dir := f.app("My Tool", `{"services":{"ui":{"ports":[{"target":8080}]},"web":{"ports":[{"target":80}]},"worker":{}}}`)
	if err := os.WriteFile(filepath.Join(dir, "compose.prod.yml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	lab := f.lab("lab", "apps: {}\n")

	r := f.must("add", dir, "--name", "tool", "--routing", "host", "--host", "tool.example.com",
		"--service", "ui", "--port", "8081", "--compose", "compose.yml,compose.prod.yml", "--prefix", "/t")
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

	// Several published services: without a terminal to ask on, the flag to pass.
	r = f.hl("add", dir)
	contains(t, "add", r.errOut, "my-tool: several services publish ports (ui, web) - pass --service, e.g. --service web")
	// The folder name becomes a valid app name.
	r = f.must("add", dir, "--service", "web")
	contains(t, "add", r.out, "added my-tool (service web, port 80)", "path routing serves it under /my-tool")

	r = f.must("add", dir, "--name", "db", "--routing", "none", "--compose", "compose.prod.yml")
	contains(t, "add none", r.out, "not behind the proxy")

	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"add", dir, "--name", "tool"}, 1, `app "tool" is already added`},
		{[]string{"add", dir, "--name", "x2"}, 1, "is already added as app"},
		{[]string{"add", dir, "--name", "Bad Name"}, 1, "use lowercase letters"},
		{[]string{"add", dir, "--name", "proxy"}, 1, "reserved"},
		{[]string{"add", dir, "--name", "x3", "--compose", "c.yml", "--service", "nope"}, 1, "compose file"},
		{[]string{"add", dir, "--name", "x4", "--compose", "compose.yml", "--service", "nope"}, 1, `no service "nope"`},
		{[]string{"add", dir, "--name", "x5", "--compose", "compose.yml", "--routing", "sideways"}, 1, "is not path, host or none"},
		{[]string{"add", filepath.Join(f.home, "missing")}, 1, "not found"},
		{[]string{"add"}, 2, "accepts 1 arg"},
		{[]string{"add", dir, "--port", "x"}, 2, "invalid argument"},
	} {
		r := f.hl(c.args...)
		if r.code != c.code || !strings.Contains(r.errOut, c.want) {
			t.Errorf("hl %v: want exit %d and %q, got %s", c.args, c.code, c.want, r)
		}
	}
	two := f.app("two", `{"services":{"a":{"ports":[{"target":1}]},"b":{"ports":[{"target":2}]}}}`)
	r = f.hl("add", two)
	contains(t, "several services", r.errOut, "several services publish ports (a, b) - pass --service")
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
	lab := f.lab("lab", "proxy: {dashboard: true, port: 8080}\napps:\n  a: {path: '"+a+"'}\n  b: {path: '"+b+"'}\n  c: {path: '"+c+"'}\n  d: {path: '"+d+"', routing: none}\n  e: {path: '"+d+"'}\n")
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
	contains(t, "status", r.out, "proxy running  127.0.0.1:8080", "dashboard http://traefik.localhost:8080/",
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
	r = f.hl("ls")
	if r.code != 0 {
		t.Fatalf("status without docker should still work: %s", r)
	}
	contains(t, "status without docker", r.errOut, "can't ask Docker", "start Docker")
	contains(t, "status without docker", r.out, "proxy unknown", "a    ?")
}

func TestUpRestartRebuild(t *testing.T) {
	f := newFake(t)
	a, b := f.app("a", webModel2), f.app("b", apiModel)
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n  b: {path: '"+b+"'}\n")
	proxyFile := filepath.Join(lab, ".hl", "proxy", "compose.yml")

	f.must("restart", "b")
	calls := f.find("docker compose --project-name")
	if len(calls) < 2 || !strings.Contains(calls[0].String(), "hl-proxy") || !strings.Contains(calls[len(calls)-1].String(), "--project-name hl-b") ||
		!strings.HasSuffix(calls[len(calls)-1].String(), " restart") {
		t.Errorf("restart starts the proxy first, then restarts b only: %v", calls)
	}

	// With the proxy running, it is left alone; rebuild builds.
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
	r := f.hl("up")
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

func TestUpWait(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n")
	f.on(rule{Match: []string{"--project-name hl-a", "up --detach --wait --wait-timeout 30"}, Stderr: "container hl-a-db-1 is unhealthy\n", Exit: 1})
	f.on(rule{Match: []string{"--project-name hl-a", "logs"}, Stdout: "db | FATAL: password authentication failed\n"})
	f.containers(
		ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")),
		ctr("hl-a", "web", "running", "Up (healthy)", filepath.Join(lab, ".hl", "a", "override.yml")),
		ctr("hl-a", "db", "running", "Up (unhealthy)", filepath.Join(lab, ".hl", "a", "override.yml")),
	)
	r := f.hl("up", "--wait", "--wait-timeout", "30s")
	if r.code != 1 {
		t.Fatalf("want a failure: %s", r)
	}
	contains(t, "up --wait", r.errOut, "a: not running and healthy: db", "hint: see `hl logs a db`")
	contains(t, "up --wait", r.out, "last 20 log lines of a/db", "password authentication failed")
	if l := f.find("logs --no-color --tail 20 db"); len(l) != 1 {
		t.Errorf("logs of the unhealthy service: %v", f.calls())
	}
}

func TestDown(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	lab := f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n")
	// Nothing runs: down does nothing and succeeds, every time.
	f.must("down")
	f.must("stop")
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

func TestUpdate(t *testing.T) {
	f := newFake(t)
	clean, dirty, plain, diverged := f.app("clean", apiModel), f.app("dirty", apiModel), f.app("plain", apiModel), f.app("diverged", apiModel)
	f.lab("lab", "apps:\n  clean: {path: '"+clean+"'}\n  dirty: {path: '"+dirty+"'}\n  plain: {path: '"+plain+"'}\n  diverged: {path: '"+diverged+"'}\n")
	f.on(rule{Name: "git", Match: []string{dirty, "status --porcelain"}, Stdout: " M main.go\n?? notes.txt\n"})
	f.on(rule{Name: "git", Match: []string{plain, "status"}, Stderr: "fatal: not a git repository (or any of the parent directories): .git\n", Exit: 128})
	f.on(rule{Name: "git", Match: []string{diverged, "pull"}, Stderr: "fatal: Not possible to fast-forward, aborting.\n", Exit: 128})

	r := f.hl("update")
	if r.code != 1 {
		t.Fatalf("a failed pull fails the command: %s", r)
	}
	contains(t, "update", r.out, "skipped: uncommitted changes in 2 file(s)", "skipped: not a git repository",
		"updated: clean", "skipped: dirty (uncommitted changes in 2 file(s) - commit or stash them first); plain (not a git repository)")
	contains(t, "update", r.errOut, "diverged: git pull --ff-only failed", "1 of 4 apps failed: diverged")
	if p := f.find("-C "+dirty, "pull"); len(p) != 0 {
		t.Errorf("pulled over uncommitted work: %v", p)
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
	r = f.hl("update", "clean")
	if r.code != 1 || !strings.Contains(r.errOut, "git not found") {
		t.Errorf("no git: %s", r)
	}
}

func TestLogs(t *testing.T) {
	f := newFake(t)
	a, b := f.app("a", webModel2), f.app("bb", apiModel)
	f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n  bb: {path: '"+b+"'}\n")
	f.on(rule{Match: []string{"--project-name hl-a ", "logs"}, Stdout: "web-1  | GET /\nweb-1  | GET /about\n"})
	f.on(rule{Match: []string{"--project-name hl-bb ", "logs"}, Stdout: "api-1  | ready\n"})

	r := f.must("logs", "a", "web", "-f", "--tail", "5")
	if l := f.find("--project-name hl-a ", "logs"); len(l) != 1 || !strings.HasSuffix(l[0].String(), "logs --tail 5 --follow web") {
		t.Errorf("logs args: %v", l)
	}
	contains(t, "logs a", r.out, "web-1  | GET /about")

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
}

func TestPsShellOpenConfigPath(t *testing.T) {
	f := newFake(t)
	a, db := f.app("a", webModel2), f.app("db", apiModel)
	lab := f.lab("lab", "proxy: {port: 8080}\napps:\n  a: {path: '"+a+"'}\n  db: {path: '"+db+"', routing: none}\n")
	f.containers(
		ctr("hl-proxy", "traefik", "running", "Up 1 hour", filepath.Join(lab, ".hl", "proxy", "compose.yml")),
		ctr("hl-a", "web", "running", "Up 1 hour", filepath.Join(lab, ".hl", "a", "override.yml")),
		ctr("other", "x", "running", "Up", "/elsewhere/compose.yml"),
	)
	r := f.must("ps")
	contains(t, "ps", r.out, "APP", "(proxy)  hl-proxy-traefik-1", "a        hl-a-web-1")
	lacks(t, "ps", r.out, "other")
	f.must("ps", "a")
	if p := f.find("--project-name hl-a", " ps"); len(p) != 1 {
		t.Errorf("ps a: %v", f.calls())
	}

	f.must("shell", "a")
	if s := f.find("exec web sh -c"); len(s) != 1 || s[0].Args[len(s[0].Args)-1] != "command -v bash >/dev/null && exec bash || exec sh" {
		t.Errorf("shell: %v", s)
	}
	f.on(rule{Match: []string{"exec db"}, Exit: 3})
	if r := f.hl("shell", "a", "db"); r.code != 3 || r.errOut != "" {
		t.Errorf("the shell's own exit status: %s", r)
	}
	f.on(rule{Match: []string{"exec api"}, Stderr: "service \"api\" is not running\n", Exit: 1})
	r = f.hl("shell", "a", "api")
	contains(t, "shell not running", r.errOut, "service api is not running", "hl up a")
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

	f.must("config", "a")
	if c := f.find("--project-name hl-a", " config"); len(c) < 2 {
		t.Errorf("config: %v", c)
	}
	if r := f.must("path"); r.out != lab+"\n" {
		t.Errorf("path: %q", r.out)
	}
	if r := f.must("path", "a"); r.out != a+"\n" {
		t.Errorf("path a: %q", r.out)
	}
	if r := f.hl("path", "aa"); r.code != 1 || !strings.Contains(r.errOut, `did you mean "a"`) {
		t.Errorf("close name: %s", r)
	}
}

func TestProxyCommands(t *testing.T) {
	f := newFake(t)
	lab := f.lab("lab", "proxy: {port: 8080, subnet: 172.29.0.0/16}\napps: {}\n")
	f.on(rule{Match: []string{"network ls"}, Stdout: "n1\nn2\n"})
	f.on(rule{Match: []string{"network inspect", "n1"}, Stdout: "bridge\t172.17.0.0/16 \nmine\t172.29.5.0/24 \n"})
	r := f.hl("proxy", "up")
	if r.code != 1 {
		t.Fatalf("subnet clash: %s", r)
	}
	contains(t, "subnet clash", r.errOut, "proxy.subnet 172.29.0.0/16 overlaps Docker network mine (172.29.5.0/24)", "set proxy.subnet: 172.31.0.0/16")

	f.lab("lab", "proxy: {port: 8080}\napps: {}\n")
	portInUse = func(bind string, port int) bool { return port == 8080 }
	f.on(rule{Match: []string{"ps --filter publish=8080"}, Stdout: "some-nginx\n"})
	r = f.hl("proxy", "up")
	if r.code != 1 {
		t.Fatalf("port in use: %s", r)
	}
	contains(t, "port in use", r.errOut, "port 8080 on 127.0.0.1 is already in use by Docker container some-nginx", "set proxy.port", "(8000 is free)")

	portInUse = func(string, int) bool { return false }
	f.reset()
	f.must("proxy", "up")
	if u := f.find("--project-name hl-proxy", "up --detach"); len(u) != 1 {
		t.Errorf("proxy up: %v", f.calls())
	}
	contains(t, "proxy compose", f.read(filepath.Join(lab, ".hl", "proxy", "compose.yml")), "'127.0.0.1:8080:80'")
	lacks(t, "proxy compose", f.read(filepath.Join(lab, ".hl", "proxy", "compose.yml")), "api@internal")

	f.containers(ctr("hl-proxy", "traefik", "running", "Up", filepath.Join(lab, ".hl", "proxy", "compose.yml")))
	f.must("proxy", "logs")
	f.must("proxy", "down")
	if d := f.find("--project-name hl-proxy", "down"); len(d) != 1 {
		t.Errorf("proxy down: %v", f.calls())
	}
	f.must("proxy", "status")
	f.must("proxy", "config")
	f.must("config", "proxy")
	r = f.must("proxy")
	contains(t, "proxy help", r.out, "Available Commands")
	if r := f.hl("proxy", "sideways"); r.code != 2 {
		t.Errorf("unknown proxy command: %s", r)
	}
}

func TestDoctor(t *testing.T) {
	f := newFake(t)
	a := f.app("a", webModel2)
	f.lab("lab", "proxy: {bind: 0.0.0.0, dashboard: true, dashboard_public: true}\napps:\n  a: {path: '"+a+"'}\n  b: {path: /nowhere}\n")
	r := f.hl("doctor")
	if r.code != 1 {
		t.Fatalf("app b is broken: %s", r)
	}
	contains(t, "doctor", r.out, "hl ", "go1.", "docker 27.3.1", "docker daemon 27.3.1 on Fake Linux", "docker compose 2.29.1",
		"homelab ", "(2 apps)", "reachable from your network", "✓ app a", "✗ app b", "proxy port 0.0.0.0:80 is free")
	contains(t, "doctor", r.errOut, "1 problem(s) found", "bug report")

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
	r2 := f.must("--version")
	if r.out != r2.out {
		t.Errorf("version %q vs --version %q", r.out, r2.out)
	}
	if r := f.must("version", "--short"); strings.Count(r.out, " ") != 0 {
		t.Errorf("short: %q", r.out)
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
	f.lab("lab", "apps:\n  a: {path: '"+a+"'}\n  b: {path: '"+b+"'}\n")
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
	lab := f.lab("lab", "name: lab-b\nproxy: {port: 8443, domain: lab.example, network: lab-net, bind: 127.0.0.2}\napps:\n  a: {path: '"+a+"'}\n")
	r := f.must("up")
	contains(t, "status", r.out, "127.0.0.2:8443", "http://lab.example:8443/a/")
	contains(t, "override", f.read(filepath.Join(lab, ".hl", "a", "override.yml")), "name: 'lab-net'", "traefik.docker.network: 'lab-net'")
	contains(t, "proxy compose", f.read(filepath.Join(lab, ".hl", "proxy", "compose.yml")), "'127.0.0.2:8443:80'", "name: 'lab-net'", "name: hl-lab-b-proxy")
	contains(t, "traefik.yml", f.read(filepath.Join(lab, ".hl", "proxy", "traefik.yml")), "network: 'lab-net'")
	if u := f.find("--project-name hl-lab-b-a ", "up --detach"); len(u) != 1 || u[0].Env["HL_URL"] != "http://lab.example:8443/a" {
		t.Errorf("app run: %v", u)
	}
	for _, c := range f.calls() {
		if strings.Contains(c.String(), "hl-proxy") || strings.Contains(c.String(), "--project-name hl-a ") {
			t.Errorf("a default name was used: %s", c)
		}
	}
	r = f.must("config", "defaults")
	contains(t, "config defaults", r.out, "logs.tail", "up.wait_timeout", "docker.project_prefix")
}
