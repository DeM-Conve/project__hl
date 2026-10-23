package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/gen"
)

// project is the app's own compose project, before hl's override: its
// files, its .env, then the homelab's env files and settings.
func project(cfg *config.Config, name string, a *config.App) (*compose.Project, error) {
	dir := cfg.AppDir(a)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%s: app folder %s not found", name, dir)
	}
	var files []string
	if len(a.Compose) == 0 {
		f, err := compose.FindFile(dir)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		files = []string{f}
	} else {
		for _, f := range a.Compose {
			files = append(files, filepath.Join(dir, f))
		}
	}
	// Passing --env-file stops compose reading the app's .env by itself, so it goes first.
	var envFiles []string
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		envFiles = append(envFiles, filepath.Join(dir, ".env"))
	}
	for _, f := range cfg.EnvFiles(a) {
		if _, err := os.Stat(f); err != nil {
			return nil, fmt.Errorf("%s: env file %s not found", name, f)
		}
		envFiles = append(envFiles, f)
	}
	return &compose.Project{
		Name:     config.ProjectName(name),
		Dir:      dir,
		Files:    files,
		EnvFiles: envFiles,
		Env:      cfg.AppEnv(name, a),
	}, nil
}

// prepared is the app's project with hl's override written and layered on,
// and the data folders its volumes use created.
func prepared(cfg *config.Config, name string) (*compose.Project, error) {
	a, err := cfg.App(name)
	if err != nil {
		return nil, err
	}
	p, err := project(cfg, name, a)
	if err != nil {
		return nil, err
	}
	m, err := p.Model()
	if err != nil {
		return nil, err
	}
	g := gen.App{
		Name:    name,
		Routing: cfg.Routing(a),
		Prefix:  cfg.Prefix(name, a),
		Host:    cfg.Host(name, a),
		Service: a.Service,
		Port:    a.Port,
		Network: cfg.Proxy.Network,
		DataDir: cfg.DataDir(name),
	}
	if g.Routing != config.RoutingNone {
		if g.Service == "" {
			if g.Service, err = m.Exposed(); err != nil {
				return nil, fmt.Errorf("%s: %w (or set apps.%s.service in hl.yml)", name, err, name)
			}
		}
		if g.Port == 0 {
			if g.Port, err = m.TargetPort(g.Service); err != nil {
				return nil, fmt.Errorf("%s: %w (or set apps.%s.port in hl.yml)", name, err, name)
			}
		}
	}
	override, dirs, err := gen.Override(g, m)
	if err != nil {
		return nil, err
	}
	for _, d := range append(dirs, cfg.StateDir(name)) {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	f := filepath.Join(cfg.StateDir(name), "override.yml")
	if err := os.WriteFile(f, []byte(override), 0o644); err != nil {
		return nil, err
	}
	p.Files = append(p.Files, f)
	return p, nil
}

// proxy is the Traefik project, its files regenerated from hl.yml (proxyFiles).
func proxy(cfg *config.Config) (*compose.Project, error) {
	return proxyProject(cfg)
}

// running reports whether any container of the compose project is up.
func running(projectName string) bool {
	out, err := exec.Command("docker", "ps", "-q", "--filter", "label=com.docker.compose.project="+projectName).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// ensureProxy starts the proxy first, so apps can join its network.
func ensureProxy(cfg *config.Config) error {
	p, err := proxy(cfg)
	if err != nil {
		return err
	}
	if running(p.Name) {
		return nil
	}
	step("proxy")
	return p.Run("up", "--detach")
}
