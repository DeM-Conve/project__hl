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
	oldIn := input
	input = strings.NewReader(stdin)
	done := make(chan string)
	go func() {
		var b bytes.Buffer
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	c := root()
	c.SetArgs(args)
	runErr := c.Execute()
	w.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	input = oldIn
	return <-done, runErr
}

// onTerminal makes hl believe stdin is a terminal for the rest of the test.
func onTerminal(t *testing.T) {
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = old })
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

func TestConfigCheck(t *testing.T) {
	home, _ := homelab(t, "version: 1\nproxy:\n  bnd: 0.0.0.0\napps:\n  my-app: {path: ~/code/my-app, port: 0}\n")
	out, err := run(t, "", "config", "check")
	if err == nil || !strings.Contains(err.Error(), "2 problems") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, want := range []string{
		filepath.Join(home, "hl.yml") + `:3:3: proxy.bnd: unknown key - did you mean "bind"?`,
		":5:39: apps.my-app.port: 0 is not a port",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	homelab(t, "apps:\n  my-app: {path: ~/code/my-app}\n")
	out, err = run(t, "", "config", "check")
	if err != nil || !strings.Contains(out, "is fine") || !strings.Contains(out, "no `version:` line") {
		t.Errorf("err = %v\n%s", err, out)
	}
	// Still works when hl.yml doesn't even parse.
	homelab(t, "apps: [\n")
	if out, err := run(t, "", "config", "check"); err == nil || !strings.Contains(out, "hl.yml:1:1: not valid YAML") {
		t.Errorf("err = %v\n%s", err, out)
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
