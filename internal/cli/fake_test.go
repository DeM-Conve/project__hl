package cli

// A fake `docker` (and `git`) for the CLI tests: the test binary itself,
// linked under those names into a folder that is the whole PATH. Each call
// is recorded with its arguments and HL_* environment; what it prints and
// its exit code come from rules the test sets, with sensible defaults.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	switch name := filepath.Base(os.Args[0]); name {
	case "docker", "git", "lsof", "xdg-open":
		os.Exit(fakeMain(name, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// call is one recorded run of a fake command.
type call struct {
	Name string            `json:"name"`
	Args []string          `json:"args"`
	Dir  string            `json:"dir"`
	Env  map[string]string `json:"env,omitempty"`
	Note string            `json:"note,omitempty"` // "started", "signal: terminated"
}

func (c call) String() string { return c.Name + " " + strings.Join(c.Args, " ") }

// rule answers the calls whose arguments contain every string in Match.
type rule struct {
	Name   string   `json:"name,omitempty"` // default docker
	Match  []string `json:"match"`
	Stdout string   `json:"stdout,omitempty"`
	Stderr string   `json:"stderr,omitempty"`
	Exit   int      `json:"exit,omitempty"`
	Wait   bool     `json:"wait,omitempty"` // run until signalled
}

const fakeDirEnv = "HL_TEST_FAKE_DIR"

func record(dir string, c call) {
	b, _ := json.Marshal(c)
	f, err := os.OpenFile(filepath.Join(dir, "calls.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func fakeMain(name string, args []string) int {
	dir := os.Getenv(fakeDirEnv)
	wd, _ := os.Getwd()
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "HL_") && k != fakeDirEnv {
			env[k] = v
		}
	}
	c := call{Name: name, Args: args, Dir: wd, Env: env}
	record(dir, c)
	joined := strings.Join(args, " ")
	var rules []rule
	if b, err := os.ReadFile(filepath.Join(dir, "rules.json")); err == nil {
		_ = json.Unmarshal(b, &rules)
	}
	for _, r := range rules {
		if r.Name == "" {
			r.Name = "docker"
		}
		if r.Name != name {
			continue
		}
		ok := true
		for _, m := range r.Match {
			if !strings.Contains(joined, m) {
				ok = false
			}
		}
		if !ok {
			continue
		}
		fmt.Fprint(os.Stdout, r.Stdout)
		fmt.Fprint(os.Stderr, r.Stderr)
		if r.Wait {
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
			record(dir, call{Name: name, Args: args, Note: "started"})
			select {
			case s := <-sigs:
				record(dir, call{Name: name, Args: args, Note: "signal: " + s.String()})
				return 130
			case <-time.After(20 * time.Second):
				return 99
			}
		}
		return r.Exit
	}
	if name != "docker" {
		return 0
	}
	switch {
	case strings.Contains(joined, "config --format json"):
		// The app folder's model.json is what compose would resolve.
		for i, a := range args {
			if a == "--project-directory" && i+1 < len(args) {
				if b, err := os.ReadFile(filepath.Join(args[i+1], "model.json")); err == nil {
					os.Stdout.Write(b)
					return 0
				}
			}
		}
		fmt.Fprintln(os.Stderr, "no model.json for this fake app")
		return 1
	case strings.HasPrefix(joined, "network inspect"):
		fmt.Fprintln(os.Stderr, "Error response from daemon: network not found")
		return 1
	case joined == "compose version --short":
		fmt.Println("2.29.1")
	case strings.HasPrefix(joined, "version --format"):
		fmt.Println("27.3.1")
	case strings.HasPrefix(joined, "info"):
		fmt.Println("27.3.1 on Fake Linux (linux/x86_64)")
	}
	return 0
}

// fake is a test's world: a fresh user's home (with spaces in its path), no
// hl state, and only the fake commands on PATH.
type fake struct {
	t     *testing.T
	dir   string // the fake's records and rules
	bin   string
	home  string
	rules []rule
}

func newFake(t *testing.T) *fake {
	t.Helper()
	root := t.TempDir()
	f := &fake{t: t, dir: filepath.Join(root, "fake"), bin: filepath.Join(root, "bin"), home: filepath.Join(root, "home of a user")}
	for _, d := range []string{f.dir, f.bin, f.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"docker", "git"} {
		if err := os.Symlink(exe, filepath.Join(f.bin, n)); err != nil {
			t.Skip("needs symlinks:", err)
		}
	}
	t.Setenv("PATH", f.bin)
	t.Setenv("HOME", f.home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(f.home, ".config"))
	t.Setenv("HL_HOME", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv(fakeDirEnv, f.dir)
	oldGoos, oldPort, oldNets, oldTerm := goos, portInUse, hostSubnets, stdinTerminal
	t.Cleanup(func() {
		goos, portInUse, hostSubnets, stdinTerminal = oldGoos, oldPort, oldNets, oldTerm
		homeFlag = ""
		stdin, stdout, stderr = os.Stdin, os.Stdout, os.Stderr
	})
	portInUse = func(string, int) bool { return false }
	hostSubnets = func() []*net.IPNet { return nil }
	stdinTerminal = func(io.Reader) bool { return false }
	return f
}

// on adds a rule; later rules come after earlier ones, so add specific ones first.
func (f *fake) on(r rule) {
	f.rules = append(f.rules, r)
	b, _ := json.Marshal(f.rules)
	if err := os.WriteFile(filepath.Join(f.dir, "rules.json"), b, 0o644); err != nil {
		f.t.Fatal(err)
	}
}

// containers makes `docker ps --all` (hl's inventory) list these lines.
func (f *fake) containers(lines ...string) {
	f.on(rule{Match: []string{"ps --all --filter label=com.docker.compose.project --format"}, Stdout: strings.Join(lines, "\n") + "\n"})
}

// ctr is one inventory line.
func ctr(project, service, state, status, configFiles string) string {
	return strings.Join([]string{project, service, state, status, configFiles, project + "-" + service + "-1"}, "\t")
}

// calls are the fake commands run so far; reset forgets them.
func (f *fake) calls() []call {
	b, err := os.ReadFile(filepath.Join(f.dir, "calls.jsonl"))
	if err != nil {
		return nil
	}
	var out []call
	for _, l := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		var c call
		if json.Unmarshal(l, &c) == nil {
			out = append(out, c)
		}
	}
	return out
}

func (f *fake) reset() { _ = os.Remove(filepath.Join(f.dir, "calls.jsonl")) }

// find is the calls whose arguments contain every string in match.
func (f *fake) find(match ...string) []call {
	var out []call
	for _, c := range f.calls() {
		if c.Note != "" {
			continue
		}
		s := c.String()
		ok := true
		for _, m := range match {
			if !strings.Contains(s, m) {
				ok = false
			}
		}
		if ok {
			out = append(out, c)
		}
	}
	return out
}

// result is one hl run.
type result struct {
	code        int
	out, errOut string
}

func (r result) String() string {
	return fmt.Sprintf("exit %d\n--- stdout\n%s--- stderr\n%s", r.code, r.out, r.errOut)
}

// hl runs the command line as main does, with ctx (default: background).
func (f *fake) hl(args ...string) result { return f.hlCtx(context.Background(), args...) }

func (f *fake) hlCtx(ctx context.Context, args ...string) result {
	f.t.Helper()
	var out, errOut bytes.Buffer
	stdin, stdout, stderr = strings.NewReader(""), &out, &errOut
	code := runCLI(ctx, args)
	return result{code, out.String(), errOut.String()}
}

// must runs hl and fails the test unless it exits 0.
func (f *fake) must(args ...string) result {
	f.t.Helper()
	r := f.hl(args...)
	if r.code != 0 {
		f.t.Fatalf("hl %s: %s", strings.Join(args, " "), r)
	}
	return r
}

// app makes a project folder with a compose file and the model the fake
// docker resolves for it.
func (f *fake) app(name, model string) string {
	f.t.Helper()
	dir := filepath.Join(f.home, "my code", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	for file, body := range map[string]string{"compose.yml": "services: {}\n", "model.json": model} {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
	return dir
}

// lab writes a homelab folder with this hl.yml and makes it HL_HOME.
func (f *fake) lab(name, hlyml string) string {
	f.t.Helper()
	dir := filepath.Join(f.home, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hl.yml"), []byte(hlyml), 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.t.Setenv("HL_HOME", dir)
	return dir
}

func (f *fake) read(path string) string {
	f.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

func contains(t *testing.T, what, s string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("%s is missing %q:\n%s", what, w, s)
		}
	}
}

func lacks(t *testing.T, what, s string, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(s, w) {
			t.Errorf("%s should not contain %q:\n%s", what, w, s)
		}
	}
}

const (
	webModel2 = `{"services":{"web":{"ports":[{"target":80,"published":"8080"}]},"db":{}},"volumes":{"pgdata":{}}}`
	apiModel  = `{"services":{"api":{"ports":[{"target":3000,"published":"3000"}]}}}`
)
