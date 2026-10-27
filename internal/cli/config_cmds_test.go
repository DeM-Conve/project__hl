package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// fakeDocker puts a `docker` on PATH that answers `compose ... config --format json` with
// model (a compose model as JSON) and does nothing for anything else.
func fakeDocker(t *testing.T, model string) {
	t.Helper()
	bin := t.TempDir()
	m := filepath.Join(bin, "model.json")
	if err := os.WriteFile(m, []byte(model), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$*\" in *\"config --format json\"*) cat '" + m + "';; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// homelab makes a homelab folder with hl.yml = body and an app folder ~/code/my-app (with a
// compose file) in a temp home, and points hl at it.
func homelab(t *testing.T, body string) (home, appDir string) {
	t.Helper()
	user := t.TempDir()
	t.Setenv("HOME", user)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(user, ".config"))
	home = filepath.Join(user, "homelab")
	appDir = filepath.Join(user, "code", "my-app")
	for _, d := range []string{home, appDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(appDir, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, config.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HL_HOME", home)
	return home, appDir
}

// run runs hl with args, stdin as its input, and returns what it printed (stdout and stderr
// together) and its error.
func run(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	done := make(chan string)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	c := root()
	c.SetArgs(args)
	c.SetIn(strings.NewReader(stdin))
	runErr := c.Execute()
	_ = w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	return <-done, runErr
}

// onTerminal makes hl believe stdin is a terminal for the rest of the test.
func onTerminal(t *testing.T) {
	old := stdinTerminal
	stdinTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { stdinTerminal = old })
}

func hlYML(t *testing.T, home string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const oneService = `{"services": {"web": {"ports": [{"target": 80, "published": "8080"}]}}}`

func TestAddUpgradesOldFileWithAMessage(t *testing.T) {
	fakeDocker(t, oneService)
	home, appDir := homelab(t, "# my homelab\napps: {}\n")
	out, err := run(t, "", "add", appDir)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "upgraded hl.yml to version 1") {
		t.Errorf("no upgrade message:\n%s", out)
	}
	got := hlYML(t, home)
	if !strings.Contains(got, "version: 1") || !strings.Contains(got, "# my homelab") || !strings.Contains(got, "my-app:") {
		t.Errorf("hl.yml after add:\n%s", got)
	}
	// A current file: no message.
	if out, err := run(t, "", "remove", "my-app"); err != nil || strings.Contains(out, "upgraded") {
		t.Errorf("remove: %v\n%s", err, out)
	}
}

// Every command checks hl.yml and lists every problem with its line; there is no separate check.
func TestEveryCommandChecksHlYml(t *testing.T) {
	home, _ := homelab(t, "version: 1\nproxy:\n  bnd: 0.0.0.0\napps:\n  my-app: {path: ~/code/my-app, port: 0}\n")
	out, err := run(t, "", "show")
	if err == nil {
		t.Fatalf("want the problems:\n%s", out)
	}
	for _, want := range []string{
		filepath.Join(home, "hl.yml") + `:3:3: proxy.bnd: unknown key - did you mean "bind"?`,
		":5:39: apps.my-app.port: 0 is not a port",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestSetAndUnset(t *testing.T) {
	home, _ := homelab(t, "# my homelab\napps:\n  my-app:\n    path: ~/code/my-app  # the folder\n")
	out, err := run(t, "", "set", "my-app", "routing=host", "port=8080", "env.TZ=UTC")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"set apps.my-app.port = 8080", "upgraded hl.yml to version 1", "hl up my-app"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	got := hlYML(t, home)
	for _, want := range []string{"# my homelab", "# the folder", "routing: host", "port: 8080", "TZ: UTC", "version: 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}

	if out, err := run(t, "", "unset", "my-app", "port", "env.TZ"); err != nil || !strings.Contains(out, "unset apps.my-app.port") {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := hlYML(t, home); strings.Contains(got, "port:") || strings.Contains(got, "TZ") || !strings.Contains(got, "# the folder") {
		t.Errorf("after unset:\n%s", got)
	}

	before := hlYML(t, home)
	for _, args := range [][]string{
		{"set", "my-app", "routing=sideways"},
		{"set", "my-app", "port"},
		{"set", "my-app", "port="},
		{"set", "nope", "port=1"},
		{"unset", "my-app", "path"},
	} {
		if out, err := run(t, "", args...); err == nil {
			t.Errorf("%v: expected an error\n%s", args, out)
		}
	}
	if hlYML(t, home) != before {
		t.Error("a refused change was written")
	}
}

func TestSetProxyAndSharedEnv(t *testing.T) {
	home, _ := homelab(t, "version: 1\n# the proxy\nproxy:\n  bind: 127.0.0.1  # loopback\napps:\n  my-app:\n    path: ~/code/my-app\n")
	out, err := run(t, "", "set", "proxy", "port=8082", "subnet=172.31.0.0/16", "bind=0.0.0.0")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"set proxy.port = 8082", "set proxy.subnet = 172.31.0.0/16", "apply it: hl down, then hl up"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	out, err = run(t, "", "set", "env.TZ=Europe/Berlin", "env.LANG=C.UTF-8")
	if err != nil || !strings.Contains(out, "set env.TZ = Europe/Berlin") || !strings.Contains(out, "apply it: hl up (") {
		t.Fatalf("%v\n%s", err, out)
	}
	got := hlYML(t, home)
	for _, want := range []string{"# the proxy", "bind: 0.0.0.0 # loopback", "port: 8082", "subnet: 172.31.0.0/16", "TZ: Europe/Berlin", "LANG: C.UTF-8"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}

	// The port is in every app's HL_URL: the apps need hl up too; the bind isn't.
	if out, err := run(t, "", "unset", "proxy", "port"); err != nil || !strings.Contains(out, "unset proxy.port") || !strings.Contains(out, "apply it: hl up proxy, then hl up (") {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := run(t, "", "set", "proxy", "bind=0.0.0.0"); err != nil || !strings.Contains(out, "apply it: hl up proxy\n") {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := run(t, "", "unset", "env.TZ"); err != nil || !strings.Contains(out, "unset env.TZ") {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := run(t, "", "unset", "env"); err != nil || !strings.Contains(out, "unset env") {
		t.Fatalf("%v\n%s", err, out)
	}
	got = hlYML(t, home)
	if strings.Contains(got, "port:") || strings.Contains(got, "TZ") || strings.Contains(got, "LANG") || !strings.Contains(got, "subnet:") {
		t.Errorf("after unset:\n%s", got)
	}

	before := hlYML(t, home)
	for _, args := range [][]string{
		{"set", "proxy", "port=0"},
		{"set", "proxy", "debug=maybe"},
		{"set", "proxy", "dashboard=true"},
		{"set", "proxy"},
		{"set", "env.TZ="},
		{"set", "port=80"},
		{"set", "my-app"},
		{"unset", "proxy"},
		{"unset", "port"},
	} {
		if out, err := run(t, "", args...); err == nil {
			t.Errorf("%v: expected an error\n%s", args, out)
		}
	}
	if hlYML(t, home) != before {
		t.Error("a refused change was written")
	}

	// Completion: the first argument offers apps, proxy and the shared keys; then the scope's keys.
	out, _ = run(t, "", "__complete", "set", "")
	for _, want := range []string{"my-app\n", "proxy\n", "env.\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("complete set: missing %q:\n%s", want, out)
		}
	}
	out, _ = run(t, "", "__complete", "set", "proxy", "")
	if !strings.Contains(out, "subnet=\n") || strings.Contains(out, "prefix=") {
		t.Errorf("complete set proxy:\n%s", out)
	}
	out, _ = run(t, "", "__complete", "unset", "my-app", "")
	if !strings.Contains(out, "prefix\n") || strings.Contains(out, "subnet") {
		t.Errorf("complete unset my-app:\n%s", out)
	}
	out, _ = run(t, "", "__complete", "set", "env.TZ=UTC", "")
	if !strings.Contains(out, "env.\n") || strings.Contains(out, "port") {
		t.Errorf("complete set env.TZ=UTC:\n%s", out)
	}
}

// compose+= on an app that names no compose file keeps the one compose picks.
func TestSetComposeAddKeepsTheDetectedFile(t *testing.T) {
	home, appDir := homelab(t, "version: 1\napps:\n  my-app:\n    path: ~/code/my-app\n")
	if err := os.WriteFile(filepath.Join(appDir, "compose.dev.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "", "set", "my-app", "compose+=compose.dev.yml")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := hlYML(t, home); !strings.Contains(got, "compose: [compose.yml, compose.dev.yml]") {
		t.Errorf("want the detected file first:\n%s", got)
	}
	if strings.Contains(out, "compose = compose.yml") {
		t.Errorf("the seeded file is not a change of the user's:\n%s", out)
	}
	if out, err := run(t, "", "set", "my-app", "compose-=compose.dev.yml"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := hlYML(t, home); !strings.Contains(got, "compose: [compose.yml]") {
		t.Errorf("after -=:\n%s", got)
	}

	// hl add does the same.
	fakeDocker(t, oneService)
	other := filepath.Join(filepath.Dir(appDir), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"docker-compose.yml", "compose.dev.yml"} {
		if err := os.WriteFile(filepath.Join(other, f), []byte("services: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := run(t, "", "add", other, "compose+=compose.dev.yml"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := hlYML(t, home); !strings.Contains(got, "- docker-compose.yml\n      - compose.dev.yml") {
		t.Errorf("hl add compose+=:\n%s", got)
	}
}

func TestUnsetAppNamedLikeASharedKey(t *testing.T) {
	home, _ := homelab(t, "version: 1\nenv:\n  TZ: UTC\napps:\n  env.tz:\n    path: ~/code/my-app\n    prefix: /x\n")
	if out, err := run(t, "", "unset", "env.tz", "prefix"); err != nil || !strings.Contains(out, "unset apps.env.tz.prefix") {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := hlYML(t, home); !strings.Contains(got, "TZ: UTC") || strings.Contains(got, "prefix") {
		t.Errorf("after unset:\n%s", got)
	}
}

// fakeEditor sets $EDITOR to a script that, on its n-th run, replaces the file with
// versions[n] (the last one repeats), and counts its runs in the returned file.
func fakeEditor(t *testing.T, versions ...string) (runs string) {
	t.Helper()
	dir := t.TempDir()
	runs = filepath.Join(dir, "runs")
	script := "#!/bin/sh\nn=$(cat '" + runs + "' 2>/dev/null || echo 0)\necho $((n+1)) > '" + runs + "'\n"
	for i, v := range versions {
		f := filepath.Join(dir, "v"+string(rune('0'+i)))
		if err := os.WriteFile(f, []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
		script += "[ \"$n\" -ge " + string(rune('0'+i)) + " ] && cp '" + f + "' \"$1\"\n"
	}
	script += "exit 0\n"
	ed := filepath.Join(dir, "my editor")
	if err := os.WriteFile(ed, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "'"+ed+"'")
	return runs
}

func editorRuns(t *testing.T, runs string) string {
	b, _ := os.ReadFile(runs)
	return strings.TrimSpace(string(b))
}

const good = "version: 1\n# kept\napps:\n  my-app: {path: ~/code/my-app}  # mine\n"
const bad = "version: 1\napps:\n  my-app: {path: ~/code/my-app, prot: 80}\n"

func TestEditNeedsATerminal(t *testing.T) {
	home, _ := homelab(t, good)
	runs := fakeEditor(t, bad)
	out, err := run(t, "", "edit")
	if err == nil || !strings.Contains(err.Error(), "needs a terminal") || hintFor(err) == "" {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if editorRuns(t, runs) != "" || hlYML(t, home) != good {
		t.Error("the editor ran without a terminal")
	}
}

func TestEditNoChanges(t *testing.T) {
	home, _ := homelab(t, good)
	onTerminal(t)
	fakeEditor(t)
	if out, err := run(t, "", "edit"); err != nil || !strings.Contains(out, "no changes") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, defaults.EditDraft)); err == nil {
		t.Error("draft left behind")
	}
}

func TestEditSavesValidFile(t *testing.T) {
	home, _ := homelab(t, "apps: {}\n")
	onTerminal(t)
	fakeEditor(t, good)
	if out, err := run(t, "", "edit"); err != nil || !strings.Contains(out, "saved") {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := hlYML(t, home); got != good {
		t.Errorf("hl.yml = %q", got)
	}
}

func TestEditReopensOnError(t *testing.T) {
	home, _ := homelab(t, "apps: {}\n")
	onTerminal(t)
	runs := fakeEditor(t, bad, good)
	out, err := run(t, "\n", "edit")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, `hl.yml:3:33: apps.my-app.prot: unknown key - did you mean "port"?`) || !strings.Contains(out, "open the editor again?") {
		t.Errorf("output:\n%s", out)
	}
	if editorRuns(t, runs) != "2" || hlYML(t, home) != good {
		t.Errorf("runs %s, hl.yml:\n%s", editorRuns(t, runs), hlYML(t, home))
	}
}

func TestEditGivesUpWithoutBreakingHlYml(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
	}{{"answered no", "n\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			home, _ := homelab(t, good)
			onTerminal(t)
			runs := fakeEditor(t, bad)
			out, err := run(t, tc.answer, "edit")
			if err == nil || !strings.Contains(err.Error(), "hl.yml not changed") {
				t.Fatalf("err = %v\n%s", err, out)
			}
			if hlYML(t, home) != good || editorRuns(t, runs) != "1" {
				t.Errorf("hl.yml changed or editor reopened")
			}
			if b, _ := os.ReadFile(filepath.Join(home, defaults.EditDraft)); string(b) != bad {
				t.Errorf("draft = %q, want the edit kept", b)
			}
		})
	}
}

func TestEditWorksOnBrokenFile(t *testing.T) {
	home, _ := homelab(t, "apps: [\n")
	onTerminal(t)
	fakeEditor(t, good)
	if out, err := run(t, "", "edit"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if hlYML(t, home) != good {
		t.Error("not saved")
	}
}

const twoServices = `{"services": {
  "db":  {"ports": [{"target": 5432, "published": "5432"}]},
  "web": {"ports": [{"target": 80, "published": "8080"}, {"target": 443, "published": "8443"}, {"target": 80, "published": "8081"}]}
}}`

func TestAddAsksWhenItHasToGuess(t *testing.T) {
	fakeDocker(t, twoServices)
	home, appDir := homelab(t, "version: 1\napps: {}\n")
	onTerminal(t)
	// Enter takes the usual pick (web); then port 2 of 80, 443.
	out, err := run(t, "\n2\n", "add", appDir)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"Which one should the proxy forward to?", "db  (container port 5432)", "web  (container port 80, 443)", "choice [2]", "Which container port"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if got := hlYML(t, home); !strings.Contains(got, "service: web") || !strings.Contains(got, "port: 443") {
		t.Errorf("hl.yml:\n%s", got)
	}
}

func TestAddPicksByNumberAndFlagsWin(t *testing.T) {
	fakeDocker(t, twoServices)
	home, appDir := homelab(t, "version: 1\napps: {}\n")
	onTerminal(t)
	if out, err := run(t, "1\n", "add", appDir); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := hlYML(t, home); !strings.Contains(got, "service: db") || !strings.Contains(got, "port: 5432") {
		t.Errorf("hl.yml:\n%s", got)
	}
	// Flags: no question at all.
	out, err := run(t, "", "add", appDir, "name=my-app-2", "service=web", "port=80")
	if err != nil || strings.Contains(out, "choose") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestAddWithoutTerminalNamesTheFlag(t *testing.T) {
	fakeDocker(t, twoServices)
	home, appDir := homelab(t, "version: 1\napps: {}\n")
	before := hlYML(t, home)
	// web is a usual web name, so it is picked; its two ports still need --port.
	out, err := run(t, "", "add", appDir)
	if err == nil || !strings.Contains(err.Error(), "service web publishes several ports (80, 443) - name the container port") ||
		!strings.Contains(out, "several services publish ports (db, web); picked web - add service=<name> to route another") {
		t.Errorf("err = %v\n%s", err, out)
	}
	_, err = run(t, "", "add", appDir, "service=web")
	if err == nil || !strings.Contains(err.Error(), "service web publishes several ports (80, 443) - name the container port, e.g. port=80") {
		t.Errorf("err = %v", err)
	}
	_, err = run(t, "", "add", appDir, "service=nope")
	if err == nil || !strings.Contains(err.Error(), `no service "nope"`) {
		t.Errorf("err = %v", err)
	}
	if hlYML(t, home) != before {
		t.Error("hl.yml changed by a failed add")
	}
	// One service with one port needs no question, terminal or not.
	fakeDocker(t, oneService)
	if out, err := run(t, "", "add", appDir); err != nil || !strings.Contains(out, "service web, port 80") {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestShowHomelabSettings(t *testing.T) {
	home, _ := homelab(t, "proxy:\n  port: 8080\nprojects: [~/nowhere]\nignore: [old]\n")
	out, err := run(t, "", "show")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		home, "env ($HL_HOME)", "port", "8080", "http://127.0.0.1:8080",
		"~/nowhere", "not found", "ignore", "old", "always: .*", "scan_depth", strconv.Itoa(defaults.ScanDepth), "default",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show is missing %q:\n%s", want, out)
		}
	}
	if out, err := run(t, "", "show", "--home", home); err != nil || !strings.Contains(out, "flag (--home)") {
		t.Errorf("%v\n%s", err, out)
	}
}

// The schema reserves the same app names as config.
func TestSchemaReservesTheSameNames(t *testing.T) {
	want := config.ReservedAppNames()
	b, err := os.ReadFile("../../schema/hl.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			PropertyNames struct {
				Not struct {
					Enum []string `json:"enum"`
				} `json:"not"`
			} `json:"propertyNames"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties["apps"].PropertyNames.Not.Enum; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("schema reserves %v, want %v", got, want)
	}
}

func TestAddAndCheckRefuseReservedNames(t *testing.T) {
	fakeDocker(t, oneService)
	home, appDir := homelab(t, "version: 1\napps:\n  proxy: {path: ~/code/my-app}\n")
	if out, err := run(t, "", "status"); err == nil || !strings.Contains(err.Error(), "apps.proxy: app name \"proxy\" is reserved") {
		t.Errorf("status: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(home, config.FileName), []byte("version: 1\napps: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, "", "add", appDir, "name=proxy"); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("add --name proxy: %v\n%s", err, out)
	}
}
