package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/config"
)

// initSyncDocker answers the docker calls init and sync make: `info`,
// `compose version`, `ps` (nothing runs) and `compose ... config --format
// json`, which prints the test's fake-model.json from the project folder.
const initSyncDocker = `#!/bin/sh
echo "$*" >> "$HL_TEST_DOCKER_LOG"
dir=""
prev=""
for a in "$@"; do
  [ "$prev" = "--project-directory" ] && dir="$a"
  prev="$a"
done
case "$*" in
  info*) exit 0 ;;
  "compose version --short") echo 2.29.1; exit 0 ;;
  ps*) [ -f "$HL_TEST_DOCKER_PS" ] && cat "$HL_TEST_DOCKER_PS"; exit 0 ;;
  "network inspect"*)
    # Networks listed (one name a line) in $HL_TEST_DOCKER_NETS exist; others don't.
    for a in "$@"; do last="$a"; done
    if [ -f "$HL_TEST_DOCKER_NETS" ] && grep -qx "$last" "$HL_TEST_DOCKER_NETS"; then echo "$last"; exit 0; fi
    echo "Error response from daemon: network $last not found" >&2; exit 1 ;;
  *"config --variables --format json"*)
    if [ -f "$dir/fake-vars.json" ]; then cat "$dir/fake-vars.json"; else echo "{}"; fi
    exit 0 ;;
  *"config --format json"*)
    if [ -f "$dir/fake-model.json" ]; then cat "$dir/fake-model.json"; exit 0; fi
    echo "no fake model in $dir" >&2; exit 1 ;;
esac
exit 0
`

// initSyncEnv is a fresh user: a home folder with spaces, no hl state, and a
// fake docker first on PATH. It returns the home folder.
func initSyncEnv(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home of a user")
	bin := filepath.Join(t.TempDir(), "bin")
	for _, d := range []string{home, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(initSyncDocker), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("HL_HOME", "")
	t.Setenv("HL_TEST_DOCKER_LOG", filepath.Join(bin, "log"))
	t.Setenv("HL_TEST_DOCKER_PS", filepath.Join(bin, "ps"))
	t.Setenv("HL_TEST_DOCKER_NETS", filepath.Join(bin, "nets"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldTerm, oldPort := stdinTerminal, portInUse
	t.Cleanup(func() { stdinTerminal, portInUse, homeFlag = oldTerm, oldPort, "" })
	stdinTerminal = func(io.Reader) bool { return false }
	portInUse = func(string, int) bool { return false }
	return home
}

// fakeApp makes a project folder with a compose file and the model the fake
// docker returns for it.
func fakeApp(t *testing.T, dir, model string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, body := range map[string]string{"compose.yaml": "services: {}\n", "fake-model.json": model} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const webModel = `{"services":{"web":{"ports":[{"target":80,"published":"8080"}]},"db":{}}}`

func runHL(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	c := root()
	var out bytes.Buffer
	c.SetArgs(args)
	c.SetIn(strings.NewReader(stdin))
	c.SetOut(&out)
	c.SetErr(&out)
	err := c.Execute()
	return out.String(), err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInitNonInteractive(t *testing.T) {
	home := initSyncEnv(t)
	code := filepath.Join(home, "my code")
	fakeApp(t, filepath.Join(code, "blog"), webModel)
	fakeApp(t, filepath.Join(code, "Wiki"), `{"services":{"app":{"ports":[{"target":3000}]}}}`)
	fakeApp(t, filepath.Join(code, "two"), `{"services":{"a":{"ports":[{"target":1}]},"b":{"ports":[{"target":2}]}}}`)
	fakeApp(t, filepath.Join(code, "kv"), `{"services":{"kv":{"ports":[{"target":6380}]}}}`)
	lab := filepath.Join(home, "homelab")

	// Without a terminal, init shows the plan but writes only with --yes (like hl sync).
	out, err := runHL(t, "n\n", "init", lab, "--projects", code)
	if err == nil || !strings.Contains(err.Error(), "not a terminal") || !strings.Contains(out, "+ add") {
		t.Errorf("init without a terminal or --yes must refuse after the plan: %v\n%s", err, out)
	}
	if exists(lab) || exists(filepath.Join(home, ".config", "hl", "home")) {
		t.Error("init without --yes wrote something")
	}

	out, err = runHL(t, "", "init", "--yes", lab, "--projects", code)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proxy.Routing != "path" || cfg.Proxy.Bind != "127.0.0.1" || cfg.Proxy.Port != 80 {
		t.Errorf("proxy = %+v, want the defaults", cfg.Proxy)
	}
	if strings.Join(cfg.Projects, ",") != "~/my code" {
		t.Errorf("projects = %q, want them written with ~", cfg.Projects)
	}
	if strings.Join(cfg.Names(), ",") != "blog,wiki" {
		t.Errorf("apps = %v\n%s", cfg.Names(), out)
	}
	if a := cfg.Apps["wiki"]; a.Path != "~/my code/Wiki" || a.Service != "app" || a.Port != 3000 {
		t.Errorf("wiki = %+v", a)
	}
	if !strings.Contains(out, "skip  two") || !strings.Contains(out, "hl up blog wiki") ||
		!strings.Contains(out, "skip  kv") || !strings.Contains(out, "routing=none") || !strings.Contains(out, "hl set ignore+=kv") {
		t.Errorf("output:\n%s", out)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(home, ".config", "hl", "home"))); got != lab {
		t.Errorf("default home = %q", got)
	}
	if _, err := os.Stat(filepath.Join(lab, "data")); err != nil {
		t.Error(err)
	}

	// Re-running: hl.yml is kept, only new apps are offered.
	before := readFile(t, filepath.Join(lab, config.FileName))
	if out, err = runHL(t, "", "init", lab); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if readFile(t, filepath.Join(lab, config.FileName)) != before {
		t.Error("re-running init changed hl.yml with nothing new")
	}
	if !strings.Contains(out, "hl.yml kept") {
		t.Errorf("output:\n%s", out)
	}
	fakeApp(t, filepath.Join(code, "notes"), webModel)
	if out, err = runHL(t, "", "init", "--yes", lab); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg, _ = config.Load(lab)
	if strings.Join(cfg.Names(), ",") != "blog,notes,wiki" {
		t.Errorf("apps = %v\n%s", cfg.Names(), out)
	}
	if !strings.Contains(readFile(t, filepath.Join(lab, config.FileName)), "# Folders with your projects") {
		t.Error("hl.yml lost its comments")
	}
}

func TestInitErrors(t *testing.T) {
	home := initSyncEnv(t)
	for _, args := range [][]string{
		{"init", filepath.Join(home, "a"), "--projects", filepath.Join(home, "nope")},
		// The proxy is set with hl set proxy after init; init has no flags for it.
		{"init", filepath.Join(home, "b"), "--routing", "host"},
		{"init", filepath.Join(home, "c"), "--bind", "0.0.0.0"},
		{"init", filepath.Join(home, "d"), "--port", "8081"},
		{"init", filepath.Join(home, "e"), "--apps", "blog"},
		// Mapped env is undone per variable: hl set <app> env.X= or hl unset <app> env.
		{"init", filepath.Join(home, "f"), "--no-env"},
		{"sync", "--home", filepath.Join(home, "g"), "--no-env"},
	} {
		if out, err := runHL(t, "", args...); err == nil {
			t.Errorf("%v: expected an error\n%s", args, out)
		}
	}
}

func TestInitInteractive(t *testing.T) {
	home := initSyncEnv(t)
	stdinTerminal = func(io.Reader) bool { return true }
	portInUse = func(_ string, port int) bool { return port == 80 }
	code := filepath.Join(home, "code")
	fakeApp(t, filepath.Join(code, "blog"), webModel)
	fakeApp(t, filepath.Join(code, "wiki"), webModel)
	fakeApp(t, filepath.Join(code, "cache"), `{"services":{"cache":{"ports":[{"target":6380}]}}}`)
	if err := os.WriteFile(filepath.Join(code, "blog", "fake-vars.json"), []byte(varsJSON("BLOG_BASE_PATH", "BLOG_URL")), 0o644); err != nil {
		t.Fatal(err)
	}
	lab := filepath.Join(home, "my homelab")

	// "n": the plan is shown, nothing is written.
	out, err := runHL(t, "n\n", "init", lab)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if exists(filepath.Join(lab, config.FileName)) || exists(filepath.Join(home, ".config", "hl", "home")) || !strings.Contains(out, "nothing written") {
		t.Errorf("declining must write nothing:\n%s", out)
	}

	// One question, after the plan: projects found in the home folder, the busy port moved,
	// every app with its service, port and env, the folder skipped and why.
	out, err = runHL(t, "maybe\ny\n", "init", lab)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"homelab    " + lab + " (new hl.yml, data/)",
		"projects   ~/code\n",
		"proxy      127.0.0.1:8080 (this machine only; port 80 is in use), path routing: apps at http://localhost:8080/<app>/",
		"apps       2 apps to add",
		"+ add   blog", "env BLOG_URL=${HL_URL}",
		"! skip  cache", "not a web app?",
		"answer y or n", "Set it up like this? [Y/n]",
		"added blog", "added wiki",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "env BLOG_URL") != 1 {
		t.Errorf("an app's env is shown once, in the plan:\n%s", out)
	}
	if strings.Count(out, "Set it up like this?") != 2 {
		t.Errorf("init should ask one question (asked again after a bad answer):\n%s", out)
	}
	if strings.Index(out, "+ add") > strings.Index(out, "Set it up like this?") {
		t.Errorf("the plan comes before the question:\n%s", out)
	}
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if cfg.Proxy.Routing != "path" || cfg.Proxy.Bind != "127.0.0.1" || cfg.Proxy.Port != 8080 {
		t.Errorf("proxy = %+v", cfg.Proxy)
	}
	if strings.Join(cfg.Names(), ",") != "blog,wiki" || cfg.Apps["blog"].Env["BLOG_URL"] != "${HL_URL}" {
		t.Errorf("apps = %v, blog = %+v\n%s", cfg.Names(), cfg.Apps["blog"], out)
	}
	// The new hl.yml is short: what differs from the defaults, and the apps.
	text := readFile(t, filepath.Join(lab, config.FileName))
	for _, unwanted := range []string{"bind:", "domain:", "dashboard", "# env:"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("hl.yml should not have %q:\n%s", unwanted, text)
		}
	}

	fakeApp(t, filepath.Join(code, "tracker"), webModel)
	// Again: only the new app is offered, nothing else asked about.
	out, err = runHL(t, "y\n", "init", lab)
	if err != nil || !strings.Contains(out, "hl.yml kept") || !strings.Contains(out, "+ add   tracker") || !strings.Contains(out, "added tracker") {
		t.Errorf("%v\n%s", err, out)
	}
	out, err = runHL(t, "", "init", lab)
	if err != nil || strings.Contains(out, "Set it up") || !strings.Contains(out, "next: hl up") {
		t.Errorf("nothing new: no question expected\n%v\n%s", err, out)
	}
}

func TestInitWithoutDocker(t *testing.T) {
	home := initSyncEnv(t)
	t.Setenv("PATH", filepath.Join(home, "empty"))
	code := filepath.Join(home, "code")
	fakeApp(t, filepath.Join(code, "blog"), webModel)
	out, err := runHL(t, "", "init", "--yes", filepath.Join(home, "lab"), "--projects", code)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "install Docker") || !strings.Contains(out, "run hl sync") {
		t.Errorf("output:\n%s", out)
	}
}

func TestSync(t *testing.T) {
	home := initSyncEnv(t)
	code := filepath.Join(home, "my code")
	lab := filepath.Join(home, "lab")
	fakeApp(t, filepath.Join(code, "blog"), webModel)
	fakeApp(t, filepath.Join(code, "old"), webModel)
	fakeApp(t, filepath.Join(code, "moved"), webModel)
	if out, err := runHL(t, "", "init", lab, "--projects", code, "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(lab, "data", "old", "db"), 0o755); err != nil {
		t.Fatal(err)
	}

	// No changes yet.
	out, err := runHL(t, "", "sync", "--home", lab)
	if err != nil || !strings.Contains(out, "in sync") {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, _ := runHL(t, "", "status", "--home", lab); strings.Contains(out, "hl sync") {
		t.Errorf("no hint expected:\n%s", out)
	}

	// A new app, a deleted one, one whose port changed, one opted out.
	fakeApp(t, filepath.Join(code, "new"), webModel)
	if err := os.WriteFile(filepath.Join(code, "new", "fake-vars.json"), []byte(varsJSON("NEW_BASE_PATH", "DATA_LOCATION", "TZ")), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeApp(t, filepath.Join(code, "lib"), webModel)
	if err := os.WriteFile(filepath.Join(code, "lib", ".hlignore"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(code, "old")); err != nil {
		t.Fatal(err)
	}
	fakeApp(t, filepath.Join(code, "moved"), `{"services":{"web":{"ports":[{"target":3000}]}}}`)
	before := readFile(t, filepath.Join(lab, config.FileName))

	out, _ = runHL(t, "", "status", "--home", lab)
	if !strings.Contains(out, "1 new app in ~/my code, 1 app's folder is gone - run hl sync") {
		t.Errorf("status hint missing:\n%s", out)
	}
	out, err = runHL(t, "", "sync", "--home", lab, "--dry-run")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"+ add", "new", "web:80\n", "env NEW_BASE_PATH=${HL_PREFIX}\n", "env DATA_LOCATION=${HL_DATA}\n",
		"- remove", "old", "data/old stays", "~ update", "web:80 -> web:3000"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "lib") {
		t.Errorf("lib has a .hlignore:\n%s", out)
	}
	if readFile(t, filepath.Join(lab, config.FileName)) != before {
		t.Error("--dry-run changed hl.yml")
	}
	if _, err := runHL(t, "", "sync", "--home", lab); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("sync without a terminal must ask for --yes, got %v", err)
	}

	// Interactive: "no" keeps hl.yml, "yes" applies.
	stdinTerminal = func(io.Reader) bool { return true }
	if out, err = runHL(t, "n\n", "sync", "--home", lab); err != nil || !strings.Contains(out, "nothing changed") {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err = runHL(t, "y\n", "sync", "--home", lab); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Names(), ",") != "blog,moved,new" || cfg.Apps["moved"].Port != 3000 {
		t.Errorf("apps = %v, moved = %+v\n%s", cfg.Names(), cfg.Apps["moved"], out)
	}
	if e := cfg.Apps["new"].Env; e["NEW_BASE_PATH"] != "${HL_PREFIX}" || e["DATA_LOCATION"] != "${HL_DATA}" || len(e) != 2 {
		t.Errorf("new's env = %v\n%s", e, out)
	}
	if !strings.Contains(out, "env NEW_BASE_PATH=${HL_PREFIX}") {
		t.Errorf("sync should say what it mapped:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(lab, "data", "old", "db")); err != nil {
		t.Errorf("sync must keep a removed app's data: %v", err)
	}
	if out, err = runHL(t, "", "sync", "--home", lab, "--yes"); err != nil || !strings.Contains(out, "in sync") {
		t.Errorf("%v\n%s", err, out)
	}

	// A folder sync would only skip (not a web app) is no reason to run it.
	fakeApp(t, filepath.Join(code, "cache"), `{"services":{"cache":{"ports":[{"target":6380}]}}}`)
	if out, _ = runHL(t, "", "status", "--home", lab); strings.Contains(out, "hl sync") {
		t.Errorf("status should not send you to hl sync for a folder it skips:\n%s", out)
	}
	fakeApp(t, filepath.Join(code, "wiki"), webModel)
	if out, _ = runHL(t, "", "status", "--home", lab); !strings.Contains(out, "1 new app in ~/my code - run hl sync") {
		t.Errorf("status hint missing:\n%s", out)
	}
}

// A renamed app folder is a move: the app keeps its name, settings and data.
func TestSyncMovedFolder(t *testing.T) {
	home := initSyncEnv(t)
	code := filepath.Join(home, "my code")
	lab := filepath.Join(home, "lab")
	vols := `{"services":{"web":{"ports":[{"target":80}]}},"volumes":{"db":{}}}`
	fakeApp(t, filepath.Join(code, "blog"), vols)
	if out, err := runHL(t, "", "init", lab, "--projects", code, "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := runHL(t, "", "set", "--home", lab, "blog", "env.TZ=UTC"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(lab, "data", "blog", "db"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(code, "blog"), filepath.Join(code, "my-blog")); err != nil {
		t.Fatal(err)
	}
	out, err := runHL(t, "", "sync", "--home", lab, "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	contains(t, "sync", out, "~ moved  blog  ~/my code/blog -> ~/my code/my-blog", "moved blog to ~/my code/my-blog", "apply it: hl up blog")
	lacks(t, "sync", out, "+ add", "- remove")
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Names(), ",") != "blog" || cfg.Apps["blog"].Path != "~/my code/my-blog" || cfg.Apps["blog"].Env["TZ"] != "UTC" {
		t.Errorf("after the move: %v %+v", cfg.Names(), cfg.Apps["blog"])
	}

	// Without the app's data volume in the new folder, it is no move: remove and add.
	if err := os.Rename(filepath.Join(code, "my-blog"), filepath.Join(code, "other")); err != nil {
		t.Fatal(err)
	}
	fakeApp(t, filepath.Join(code, "other"), webModel)
	if out, err = runHL(t, "", "sync", "--home", lab, "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	contains(t, "sync", out, "+ add", "- remove")
	lacks(t, "sync", out, "~ moved")
}

func TestStatusOtherHomelabsProxy(t *testing.T) {
	home := initSyncEnv(t)
	lab, other := filepath.Join(home, "lab"), filepath.Join(home, "other lab")
	if out, err := runHL(t, "", "init", lab, "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	ps := strings.Join([]string{"hl-proxy", "traefik", "running", "Up 1 hour",
		filepath.Join(other, ".hl", "proxy", "compose.yml"), "hl-proxy-traefik-1"}, "\t") + "\n"
	if err := os.WriteFile(os.Getenv("HL_TEST_DOCKER_PS"), []byte(ps), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runHL(t, "", "status", "--home", lab)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "proxy: run by another homelab ("+other+") - see hl doctor\nhome ~/lab") || strings.Contains(out, "127.0.0.1:80") {
		t.Errorf("status header:\n%s", out)
	}
}

func TestStripPrefix(t *testing.T) {
	home := initSyncEnv(t)
	code := filepath.Join(home, "code")
	lab := filepath.Join(home, "lab")
	fakeApp(t, filepath.Join(code, "project__blog"), webModel)
	fakeApp(t, filepath.Join(code, "plain"), webModel)
	out, err := runHL(t, "", "init", "--yes", lab, "--projects", code, "--strip-prefix", "project__")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Names(), ",") != "blog,plain" || strings.Join(cfg.StripPrefix, ",") != "project__" {
		t.Errorf("apps = %v, strip_prefix = %q\n%s", cfg.Names(), cfg.StripPrefix, out)
	}
	if a := cfg.Apps["blog"]; a.Path != filepath.Join("~", "code", "project__blog") {
		t.Errorf("blog = %+v", a)
	}

	// sync and add name new folders the same way.
	fakeApp(t, filepath.Join(code, "project__wiki"), webModel)
	if out, err = runHL(t, "", "sync", "--home", lab, "--yes"); err != nil || !strings.Contains(out, "added wiki") {
		t.Errorf("%v\n%s", err, out)
	}
	notes := filepath.Join(home, "elsewhere", "project__notes")
	fakeApp(t, notes, webModel)
	if out, err = runHL(t, "", "add", notes, "--home", lab); err != nil || !strings.Contains(out, "added notes") {
		t.Errorf("%v\n%s", err, out)
	}

	// On an existing homelab --strip-prefix is saved only when none is set.
	other := filepath.Join(home, "other")
	if out, err = runHL(t, "", "init", "--yes", other, "--strip-prefix", "none"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err = runHL(t, "", "init", "--yes", other, "--strip-prefix", "x-,y-"); err != nil || !strings.Contains(out, "strip_prefix saved") {
		t.Errorf("%v\n%s", err, out)
	}
	if out, err = runHL(t, "", "init", other, "--strip-prefix", "z-"); err != nil || !strings.Contains(out, "--strip-prefix is ignored") {
		t.Errorf("%v\n%s", err, out)
	}
	if cfg, _ := config.Load(other); strings.Join(cfg.StripPrefix, ",") != "x-,y-" {
		t.Errorf("strip_prefix = %q", cfg.StripPrefix)
	}
}

func TestInitSharedPrefix(t *testing.T) {
	home := initSyncEnv(t)
	code := filepath.Join(home, "code")
	for _, f := range []string{"project__blog", "project__wiki", "plain"} {
		fakeApp(t, filepath.Join(code, f), webModel)
	}
	lab := filepath.Join(home, "lab")
	out, err := runHL(t, "", "init", lab, "--projects", code, "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "app names  without project__ - the folders share it (--strip-prefix none keeps whole names)") {
		t.Errorf("the plan should say it strips the shared prefix:\n%s", out)
	}
	if !strings.Contains(out, "hint: if links break under /<app>/ (blog, plain, wiki): hl set <app> env.<BASE_PATH_VAR>='${HL_PREFIX}' (or routing=host)\n") {
		t.Errorf("the base-path hint: one line, as hl add has it:\n%s", out)
	}
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Names(), ",") != "blog,plain,wiki" || strings.Join(cfg.StripPrefix, ",") != "project__" {
		t.Errorf("apps = %v, strip_prefix = %q\n%s", cfg.Names(), cfg.StripPrefix, out)
	}

	// --strip-prefix none keeps the whole folder names.
	whole := filepath.Join(home, "whole")
	if out, err = runHL(t, "", "init", whole, "--projects", code, "--strip-prefix", "none", "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg, _ = config.Load(whole)
	if strings.Join(cfg.Names(), ",") != "plain,project__blog,project__wiki" || len(cfg.StripPrefix) != 0 || strings.Contains(out, "app names") {
		t.Errorf("apps = %v, strip_prefix = %q\n%s", cfg.Names(), cfg.StripPrefix, out)
	}
}

func TestShortPath(t *testing.T) {
	cfg := &config.Config{Projects: []string{"/srv/long/path/code", "~/src"}}
	for in, want := range map[string]string{
		"/srv/long/path/code/blog":   "code/blog",
		"/srv/long/path/code/a/b":    "code/a/b",
		"~/src/wiki":                 "~/src/wiki",
		"/elsewhere/app":             "/elsewhere/app",
		"/srv/long/path/codex/other": "/srv/long/path/codex/other",
	} {
		if got := shortPath(cfg, in); got != want {
			t.Errorf("shortPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSyncWithoutProjects(t *testing.T) {
	home := initSyncEnv(t)
	lab := filepath.Join(home, "lab")
	if out, err := runHL(t, "", "init", "--yes", lab); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := runHL(t, "", "sync", "--home", lab); err == nil || !strings.Contains(err.Error(), "projects") {
		t.Errorf("got %v", err)
	}
}

func TestInitClash(t *testing.T) {
	home := initSyncEnv(t)
	// Another homelab runs the default proxy on port 80; 8080 is taken too.
	other := filepath.Join(home, "other lab")
	ps := strings.Join([]string{"hl-proxy", "traefik", "running", "Up 1 hour",
		filepath.Join(other, ".hl", "proxy", "compose.yml"), "hl-proxy-traefik-1"}, "\t") + "\n"
	if err := os.WriteFile(os.Getenv("HL_TEST_DOCKER_PS"), []byte(ps), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("HL_TEST_DOCKER_NETS"), []byte("hl-proxy\nhl-taken-proxy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	portInUse = func(_ string, port int) bool { return port == 80 || port == 8080 }

	lab := filepath.Join(home, "My Lab")
	out, err := runHL(t, "", "init", lab, "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"name       my-lab - the homelab in " + other + " runs hl-proxy, so this one's Docker names are hl-my-lab-<app> and hl-my-lab-proxy",
		"proxy      127.0.0.1:8000 (this machine only; port 80 is in use)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "warning") {
		t.Errorf("init settled the clashes, nothing to warn about:\n%s", out)
	}
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "my-lab" || cfg.Proxy.Port != 8000 || cfg.ProxyProject() != "hl-my-lab-proxy" || cfg.Proxy.Network != "hl-my-lab-proxy" {
		t.Errorf("name %q, proxy %+v", cfg.Name, cfg.Proxy)
	}
	if text := readFile(t, filepath.Join(lab, config.FileName)); !strings.Contains(text, "# Another homelab on this Docker") {
		t.Errorf("hl.yml should say why it has a name:\n%s", text)
	}

	// The folder's name is taken too: the next free one.
	out, err = runHL(t, "", "init", filepath.Join(home, "taken"), "--yes")
	if err != nil || !strings.Contains(out, "name       taken-2 - the homelab in "+other+" runs hl-proxy") {
		t.Errorf("%v\n%s", err, out)
	}

	// Only the network is left (its proxy stopped): taken all the same.
	if err := os.WriteFile(os.Getenv("HL_TEST_DOCKER_PS"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runHL(t, "", "init", filepath.Join(home, "net"), "--yes")
	if err != nil || !strings.Contains(out, "name       net - Docker network hl-proxy exists already") {
		t.Errorf("%v\n%s", err, out)
	}

	// No clash: no name.
	if err := os.WriteFile(os.Getenv("HL_TEST_DOCKER_NETS"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	free := filepath.Join(home, "free")
	if out, err = runHL(t, "", "init", free, "--yes"); err != nil || strings.Contains(out, "name  ") {
		t.Errorf("%v\n%s", err, out)
	}
	if cfg, _ := config.Load(free); cfg.Name != "" {
		t.Errorf("name = %q", cfg.Name)
	}
}

func TestInitOtherHome(t *testing.T) {
	home := initSyncEnv(t)
	a, b, c := filepath.Join(home, "a"), filepath.Join(home, "b"), filepath.Join(home, "c")
	if out, err := runHL(t, "", "init", a, "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	// $HL_HOME points at a: hl would keep using it.
	t.Setenv("HL_HOME", a)
	out, err := runHL(t, "", "init", b, "--yes")
	if err != nil || !strings.Contains(out, "$HL_HOME is "+a+", so hl commands use that homelab, not "+b+" - unset HL_HOME, or set it to "+b) {
		t.Errorf("%v\n%s", err, out)
	}
	t.Setenv("HL_HOME", "")

	// Inside homelab a, setting up c elsewhere.
	chdir(t, a)
	out, err = runHL(t, "", "init", c, "--yes")
	if err != nil || !strings.Contains(out, "the current folder is inside the homelab "+a) {
		t.Errorf("%v\n%s", err, out)
	}

	// Nothing to say when hl will use the new homelab.
	chdir(t, home)
	if out, err = runHL(t, "", "init", filepath.Join(home, "d"), "--yes"); err != nil || strings.Contains(out, "warning") {
		t.Errorf("%v\n%s", err, out)
	}
}

// chdir changes the working folder for the rest of the test.
func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func TestInitDefaultFolder(t *testing.T) {
	home := initSyncEnv(t)
	chdir(t, home)

	// No folder, no homelab yet: ~/homelab, never the current folder.
	out, err := runHL(t, "", "init", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	lab := filepath.Join(home, "homelab")
	if !exists(filepath.Join(lab, config.FileName)) {
		t.Errorf("init without a folder should set up ~/homelab:\n%s", out)
	}
	for _, f := range []string{config.FileName, "data", ".gitignore"} {
		if exists(filepath.Join(home, f)) {
			t.Errorf("init wrote %s into the current folder", f)
		}
	}

	// Again, from anywhere: the homelab hl uses, not a new one.
	other := filepath.Join(home, "elsewhere")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, other)
	if out, err = runHL(t, "", "init", "--yes"); err != nil || !strings.Contains(out, "hl.yml kept") || !strings.Contains(out, lab) {
		t.Errorf("%v\n%s", err, out)
	}
	if exists(filepath.Join(other, config.FileName)) {
		t.Error("init wrote hl.yml into the current folder")
	}

	// Inside a homelab folder: that one. Named explicitly: the current folder.
	second := filepath.Join(home, "second")
	if out, err = runHL(t, "", "init", second, "--yes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// A second homelab takes over as the default: the plan says so.
	if !strings.Contains(out, "default    this homelab becomes the default, instead of "+lab) {
		t.Errorf("the plan should say the default changes:\n%s", out)
	}
	chdir(t, second)
	if out, err = runHL(t, "", "init", "--yes"); err != nil || !strings.Contains(out, "homelab    existing homelab "+second+" (hl.yml kept)") {
		t.Errorf("%v\n%s", err, out)
	}
	chdir(t, other)
	if out, err = runHL(t, "", "init", ".", "--yes"); err != nil || !exists(filepath.Join(other, config.FileName)) {
		t.Errorf("hl init . sets up the current folder: %v\n%s", err, out)
	}
}

// On an existing hl.yml, init changes only what its plan lists: the usual projects folders
// are named, not written, unless --projects asks for them.
func TestInitExistingHomelabIsHonest(t *testing.T) {
	home := initSyncEnv(t)
	fakeApp(t, filepath.Join(home, "code", "blog"), webModel)
	lab := filepath.Join(home, "lab")
	if err := os.MkdirAll(lab, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "version: 1\n\n# my apps\napps: {}\n"
	if err := os.WriteFile(filepath.Join(lab, config.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HL_HOME", lab)
	out, err := runHL(t, "", "init", "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	contains(t, "plan", out, "homelab    existing homelab "+lab+" (hl.yml kept)",
		"projects   none - to look for apps in ~/code: hl init --projects ~/code")
	if got := readFile(t, filepath.Join(lab, config.FileName)); got != body {
		t.Errorf("hl.yml changed although the plan kept it:\n%s", got)
	}
	if exists(filepath.Join(lab, ".gitignore")) {
		t.Error("init wrote a .gitignore into an existing homelab")
	}

	out, err = runHL(t, "", "init", "--yes", "--projects", "~/code")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	contains(t, "plan", out, "existing homelab "+lab+" (hl.yml updated as below)", "projects   + ~/code (hl.yml updated)",
		"+ add", "blog", "projects folders saved")
	if cfg, err := config.Load(lab); err != nil || strings.Join(cfg.Projects, ",") != "~/code" || cfg.Apps["blog"] == nil {
		t.Errorf("after --projects: %v %+v", err, cfg)
	}
}
