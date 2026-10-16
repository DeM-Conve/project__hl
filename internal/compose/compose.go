// Package compose runs `docker compose` for one project and reads its
// resolved model, so hl never has to parse compose files itself.
package compose

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultFiles are the names `docker compose` looks for, in its order.
var DefaultFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

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
}

// Command is `docker compose <args>` for the project.
func (p *Project) Command(args ...string) *exec.Cmd {
	a := []string{"compose", "--project-name", p.Name, "--project-directory", p.Dir}
	for _, f := range p.Files {
		a = append(a, "--file", f)
	}
	for _, f := range p.EnvFiles {
		a = append(a, "--env-file", f)
	}
	cmd := exec.Command("docker", append(a, args...)...)
	cmd.Dir = p.Dir
	cmd.Env = append(os.Environ(), p.Env...)
	return cmd
}

// Run runs `docker compose <args>` attached to the terminal.
func (p *Project) Run(args ...string) error {
	cmd := p.Command(args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Model is the part of `docker compose config` hl needs.
type Model struct {
	Services map[string]Service `json:"services"`
	Volumes  map[string]Volume  `json:"volumes"`
}

type Service struct {
	Ports    []Port                     `json:"ports"`
	Networks map[string]json.RawMessage `json:"networks"`
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
func (p *Project) Model() (*Model, error) {
	cmd := p.Command("config", "--format", "json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker compose config (%s): %v\n%s", p.Dir, err, strings.TrimSpace(stderr.String()))
	}
	var m Model
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("reading docker compose config: %w", err)
	}
	return &m, nil
}

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
		return "", fmt.Errorf("no service publishes a port - pass --service and --port")
	}
	for _, want := range []string{"frontend", "web", "app", "nginx", "proxy", "ui"} {
		for _, name := range published {
			if name == want {
				return name, nil
			}
		}
	}
	return "", fmt.Errorf("several services publish ports (%s) - pass --service", strings.Join(published, ", "))
}

// TargetPort is the container port a service publishes first.
func (m *Model) TargetPort(service string) (int, error) {
	s, ok := m.Services[service]
	if !ok {
		return 0, fmt.Errorf("no service %q", service)
	}
	if len(s.Ports) == 0 || s.Ports[0].Target == 0 {
		return 0, fmt.Errorf("service %q publishes no port - pass --port", service)
	}
	return s.Ports[0].Target, nil
}
