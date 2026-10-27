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
  ps*) exit 0 ;;
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

	out, err = runHL(t, "", "init", "--yes", lab, "--projects", code, "--routing", "host", "--bind", "0.0.0.0", "--port", "8081")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proxy.Routing != "host" || cfg.Proxy.Bind != "0.0.0.0" || cfg.Proxy.Port != 8081 {
		t.Errorf("proxy = %+v", cfg.Proxy)
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
		!strings.Contains(out, "skip  kv") || !strings.Contains(out, "--routing none") || !strings.Contains(out, "hl ignore add kv") {
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
	if out, err = runHL(t, "", "init", lab, "--routing", "path"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if readFile(t, filepath.Join(lab, config.FileName)) != before {
		t.Error("re-running init changed hl.yml with nothing new")
	}
	if !strings.Contains(out, "hl.yml kept") || !strings.Contains(out, "--routing is ignored") {
		t.Errorf("output:\n%s", out)
	}
	fakeApp(t, filepath.Join(code, "notes"), webModel)
	fakeApp(t, filepath.Join(code, "later"), webModel)
	if out, err = runHL(t, "", "init", "--yes", lab, "--apps", "notes"); err != nil {
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
		{"init", filepath.Join(home, "b"), "--routing", "sideways"},
		{"init", filepath.Join(home, "c"), "--bind", "everywhere"},
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
	fakeApp(t, filepath.Join(code, "tracker"), webModel)
	fakeApp(t, filepath.Join(code, "cache"), `{"services":{"cache":{"ports":[{"target":6380}]}}}`)
	if err := os.WriteFile(filepath.Join(code, "blog", "fake-vars.json"), []byte(varsJSON("BLOG_BASE_PATH", "BLOG_URL")), 0o644); err != nil {
		t.Fatal(err)
	}
	lab := filepath.Join(home, "my homelab")

	// "n": the plan is shown, nothing is written.
	out, err := runHL(t, "n\n", "init", lab, "--routing", "host")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if exists(filepath.Join(lab, config.FileName)) || exists(filepath.Join(home, ".config", "hl", "home")) || !strings.Contains(out, "nothing written") {
		t.Errorf("declining must write nothing:\n%s", out)
	}

	// One question, after the plan: projects found in the home folder, the busy port moved,
	// every app with its service, port and env, the folder skipped and why.
	out, err = runHL(t, "maybe\ny\n", "init", lab, "--routing", "host", "--apps", "blog,wiki")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"port 80 is in use on 127.0.0.1 - the proxy gets port 8080",
		"homelab    " + lab + " (new hl.yml, data/)",
		"projects   ~/code\n",
		"proxy      127.0.0.1:8080 (this machine only), host routing: apps at http://<app>.localhost:8080/",
		"apps       2 apps to add",
		"+ add   blog", "env BLOG_URL=${HL_URL}",
		"! skip  cache", "not a web app?",
		"left out   tracker (--apps)",
		"answer y or n", "Set it up like this? [Y/n]",
		"added blog", "added wiki",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
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
	if cfg.Proxy.Routing != "host" || cfg.Proxy.Bind != "127.0.0.1" || cfg.Proxy.Port != 8080 {
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
	if out, err = runHL(t, "", "init", "--yes", other); err != nil {
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
	chdir(t, second)
	if out, err = runHL(t, "", "init", "--yes"); err != nil || !strings.Contains(out, "homelab    "+second+" (hl.yml kept)") {
		t.Errorf("%v\n%s", err, out)
	}
	chdir(t, other)
	if out, err = runHL(t, "", "init", ".", "--yes"); err != nil || !exists(filepath.Join(other, config.FileName)) {
		t.Errorf("hl init . sets up the current folder: %v\n%s", err, out)
	}
}
