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
	lab := filepath.Join(home, "homelab")

	out, err := runHL(t, "", "init", lab, "--projects", code, "--routing", "host", "--bind", "0.0.0.0", "--port", "8081")
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
	if !strings.Contains(out, "skip two") || !strings.Contains(out, "hl up blog wiki") {
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
	if !strings.Contains(out, "already exists") || !strings.Contains(out, "--routing is ignored") {
		t.Errorf("output:\n%s", out)
	}
	fakeApp(t, filepath.Join(code, "notes"), webModel)
	fakeApp(t, filepath.Join(code, "later"), webModel)
	if out, err = runHL(t, "", "init", lab, "--apps", "notes"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	cfg, _ = config.Load(lab)
	if strings.Join(cfg.Names(), ",") != "blog,notes,wiki" {
		t.Errorf("apps = %v\n%s", cfg.Names(), out)
	}
	if !strings.Contains(readFile(t, filepath.Join(lab, config.FileName)), "# 127.0.0.1 = this machine only") {
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
	lab := filepath.Join(home, "my homelab")

	answers := strings.Join([]string{
		lab,      // homelab folder
		"",       // projects: the suggested ~/code
		"2",      // host routing
		"",       // this machine only
		"",       // port 80 is busy: take the suggested one
		"9, 1 3", // a bad pick is asked again
		"1 3 3",  // blog and wiki (sorted: blog, tracker, wiki)
	}, "\n") + "\n"
	out, err := runHL(t, answers, "init")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"[~/code]", "port 80 is in use", "type numbers from 1 to 3", "Found 3 apps"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	cfg, err := config.Load(lab)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if cfg.Proxy.Routing != "host" || cfg.Proxy.Bind != "127.0.0.1" || cfg.Proxy.Port != 8080 {
		t.Errorf("proxy = %+v", cfg.Proxy)
	}
	if strings.Join(cfg.Names(), ",") != "blog,wiki" {
		t.Errorf("apps = %v\n%s", cfg.Names(), out)
	}
}

func TestInitWithoutDocker(t *testing.T) {
	home := initSyncEnv(t)
	t.Setenv("PATH", filepath.Join(home, "empty"))
	code := filepath.Join(home, "code")
	fakeApp(t, filepath.Join(code, "blog"), webModel)
	out, err := runHL(t, "", "init", filepath.Join(home, "lab"), "--projects", code)
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
	for _, want := range []string{"+ add", "new", "- remove", "old", "data/old stays", "~ update", "web:80 -> web:3000"} {
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
	if _, err := os.Stat(filepath.Join(lab, "data", "old", "db")); err != nil {
		t.Errorf("sync must keep a removed app's data: %v", err)
	}
	if out, err = runHL(t, "", "sync", "--home", lab, "--yes"); err != nil || !strings.Contains(out, "in sync") {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestSyncWithoutProjects(t *testing.T) {
	home := initSyncEnv(t)
	lab := filepath.Join(home, "lab")
	if out, err := runHL(t, "", "init", lab); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := runHL(t, "", "sync", "--home", lab); err == nil || !strings.Contains(err.Error(), "projects") {
		t.Errorf("got %v", err)
	}
}
