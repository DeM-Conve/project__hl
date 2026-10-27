// Package compose runs `docker compose` for one project and reads its
// resolved model, so hl never has to parse compose files itself.
package compose

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/interrupt"
)

// DefaultFiles are the names `docker compose` looks for, in its order.
var DefaultFiles = defaults.ComposeFiles

// FindFile returns the compose file `docker compose` would use in dir.
func FindFile(dir string) (string, error) {
	for _, f := range DefaultFiles {
		p := filepath.Join(dir, f)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no compose file in %s (looked for %s)", dir, strings.Join(DefaultFiles, ", "))
}

// Project is one compose project: an app's own files plus hl's override.
type Project struct {
	Name     string   // compose project name
	Dir      string   // project directory: relative paths in the files resolve here
	Files    []string // compose files, absolute, later ones override earlier ones
	EnvFiles []string // read for interpolation, later ones win; the shell env wins over all
	Env      []string // KEY=VALUE added to the environment

	// Where Run attaches the command; nil means the process's own.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// Args are the arguments of `docker` for `docker compose <args>` on the project.
func (p *Project) Args(args ...string) []string {
	a := []string{"compose", "--project-name", p.Name, "--project-directory", p.Dir}
	for _, f := range p.Files {
		a = append(a, "--file", f)
	}
	for _, f := range p.EnvFiles {
		a = append(a, "--env-file", f)
	}
	return append(a, args...)
}

// Command is `docker compose <args>` for the project. Arguments are passed
// as they are, never through a shell.
func (p *Project) Command(args ...string) *exec.Cmd {
	cmd := exec.Command("docker", p.Args(args...)...)
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(), p.Env...)
	return cmd
}

// Run runs `docker compose <args>` attached to the terminal (or the
// project's Stdin/Stdout/Stderr). Ctrl-C is handled as package interrupt describes.
func (p *Project) Run(ctx context.Context, args ...string) error {
	cmd := p.Command(args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = or[io.Reader](p.Stdin, os.Stdin), or[io.Writer](p.Stdout, os.Stdout), or[io.Writer](p.Stderr, os.Stderr)
	return Exec(ctx, cmd)
}

func or[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// Error is a docker command that failed: which one, its exit code and the
// end of what it wrote to stderr (so hl can explain the failure).
type Error struct {
	Cmd    string // e.g. "docker compose up"
	Code   int    // exit code, -1 when it didn't start or was killed
	Stderr string // the last part of its error output
	Err    error  // the underlying error
}

func (e *Error) Error() string {
	if errors.Is(e.Err, exec.ErrNotFound) {
		name, _, _ := strings.Cut(e.Cmd, " ")
		return name + " not found on PATH"
	}
	if e.Code >= 0 {
		return fmt.Sprintf("%s failed (exit status %d)", e.Cmd, e.Code)
	}
	return fmt.Sprintf("%s failed: %v", e.Cmd, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Exec runs cmd, keeping the tail of its stderr for the error. A failure
// is an *Error; an interruption is the interrupt package's error.
func Exec(ctx context.Context, cmd *exec.Cmd) error {
	tail := &tailBuffer{max: defaults.StderrTail}
	if cmd.Stderr != nil {
		cmd.Stderr = io.MultiWriter(cmd.Stderr, tail)
	} else {
		cmd.Stderr = tail
	}
	err := interrupt.Run(ctx, cmd)
	if err == nil || errors.Is(err, interrupt.ErrInterrupted) {
		return err
	}
	return wrap(cmd, err, tail.String())
}

func wrap(cmd *exec.Cmd, err error, stderr string) error {
	e := &Error{Cmd: describe(cmd.Args), Code: -1, Stderr: strings.TrimSpace(stderr), Err: err}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		e.Code = ee.ExitCode()
	}
	return e
}

// describe names a docker command without its project flags: "docker compose up".
func describe(args []string) string {
	if len(args) == 0 {
		return ""
	}
	out := []string{filepath.Base(args[0])}
	skip := false
	for _, a := range args[1:] {
		switch {
		case skip:
			skip = false
		case a == "--project-name" || a == "--project-directory" || a == "--file" || a == "--env-file" || a == "-C" ||
			a == "--format" || a == "--filter" || a == "--tail" || a == "--wait-timeout":
			skip = true
		case strings.HasPrefix(a, "-"):
		default:
			out = append(out, a)
			if len(out) == 3 { // docker compose <command>
				return strings.Join(out, " ")
			}
		}
	}
	return strings.Join(out, " ")
}

// Output runs `docker compose <args>` and returns its stdout.
func (p *Project) Output(ctx context.Context, args ...string) ([]byte, error) {
	cmd := p.Command(args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := Exec(ctx, cmd)
	return out.Bytes(), err
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.max:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }

// Model is the part of `docker compose config` hl needs.
type Model struct {
	Services map[string]Service `json:"services"`
	Volumes  map[string]Volume  `json:"volumes"`
}

type Service struct {
	Ports    []Port                     `json:"ports"`
	Networks map[string]json.RawMessage `json:"networks"`
	// DependsOn: compose's resolved model always has the long (map) form.
	DependsOn map[string]json.RawMessage `json:"depends_on"`
	// Build is the resolved build section (context, dockerfile, args, ...), empty without one.
	Build json.RawMessage `json:"build"`
}

// BuildHashes is, for each service with a build section, a hash of that section as compose
// resolved it (variables filled in), so a change of any build setting changes the hash.
func (m *Model) BuildHashes() map[string]string {
	out := map[string]string{}
	for name, s := range m.Services {
		if len(s.Build) == 0 || string(s.Build) == "null" {
			continue
		}
		// Decoded and encoded again: map keys come out sorted, so the hash doesn't depend on
		// the order compose printed them in.
		b := []byte(s.Build)
		var v any
		if json.Unmarshal(s.Build, &v) == nil {
			if c, err := json.Marshal(v); err == nil {
				b = c
			}
		}
		h := sha256.Sum256(b)
		out[name] = hex.EncodeToString(h[:])[:16]
	}
	return out
}

// WithDeps is services and every service they depend on (depends_on, followed through),
// sorted; names the model doesn't have are kept as they are.
func (m *Model) WithDeps(services ...string) []string {
	seen := map[string]bool{}
	var visit func(string)
	visit = func(s string) {
		if seen[s] {
			return
		}
		seen[s] = true
		for d := range m.Services[s].DependsOn {
			visit(d)
		}
	}
	for _, s := range services {
		visit(s)
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

type Port struct {
	Target    int `json:"target"`
	Published any `json:"published"`
}

type Volume struct {
	External   bool              `json:"external"`
	Driver     string            `json:"driver"`
	DriverOpts map[string]string `json:"driver_opts"`
}

// Model resolves the project's compose files the way `docker compose up` would.
func (p *Project) Model(ctx context.Context) (*Model, error) {
	out, err := p.Output(ctx, "config", "--format", "json")
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Stderr != "" {
			return nil, fmt.Errorf("reading the compose files in %s: %w\n%s", p.Dir, err, indent(e.Stderr))
		}
		return nil, fmt.Errorf("reading the compose files in %s: %w", p.Dir, err)
	}
	var m Model
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("reading docker compose config: %w", err)
	}
	return &m, nil
}

// Variables are the names of the variables the project's compose files use (`docker compose
// config --variables`, compose v2.24+), sorted.
func (p *Project) Variables(ctx context.Context) ([]string, error) {
	out, err := p.Output(ctx, "config", "--variables", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("listing the variables of the compose files in %s: %w", p.Dir, err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("reading docker compose config --variables: %w", err)
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func indent(s string) string { return "  " + strings.ReplaceAll(s, "\n", "\n  ") }

// Exposed picks the service the proxy should forward to: the only one that
// publishes a port, or among several a conventional web name.
func (m *Model) Exposed() (string, error) {
	var published []string
	for name, s := range m.Services {
		if len(s.Ports) > 0 {
			published = append(published, name)
		}
	}
	switch len(published) {
	case 1:
		return published[0], nil
	case 0:
		return "", fmt.Errorf("no service publishes a port - name the one to route: service=<name> port=<container port>")
	}
	for _, want := range defaults.ExposedServices {
		for _, name := range published {
			if name == want {
				return name, nil
			}
		}
	}
	sort.Strings(published)
	return "", fmt.Errorf("several services publish ports (%s) - name the one to route: service=<name>", strings.Join(published, ", "))
}

// TargetPort is the container port a service publishes first.
func (m *Model) TargetPort(service string) (int, error) {
	s, ok := m.Services[service]
	if !ok {
		return 0, fmt.Errorf("no service %q in the compose files", service)
	}
	if len(s.Ports) == 0 || s.Ports[0].Target == 0 {
		return 0, fmt.Errorf("service %q publishes no port - name its container port: port=<port>", service)
	}
	return s.Ports[0].Target, nil
}
