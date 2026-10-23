package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/fsutil"
	"github.com/DeM-Conve/project__hl/internal/gen"
)

// project is the app's own compose project, before hl's override: its
// files, its .env, then the homelab's env files and settings.
func project(cfg *config.Config, name string, a *config.App) (*compose.Project, error) {
	dir := cfg.AppDir(a)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, withHint(fmt.Errorf("%s: app folder %s not found", name, dir),
			"fix apps.%s.path in %s, or `hl remove %s` if the app is gone", name, cfg.File(), name)
	}
	var files []string
	if len(a.Compose) == 0 {
		f, err := compose.FindFile(dir)
		if err != nil {
			return nil, withHint(fmt.Errorf("%s: %w", name, err),
				"add a compose file to the folder, or list yours in apps.%s.compose in %s", name, cfg.File())
		}
		files = []string{f}
	} else {
		for _, f := range a.Compose {
			f = filepath.Join(dir, f)
			if _, err := os.Stat(f); err != nil {
				return nil, withHint(fmt.Errorf("%s: compose file %s not found", name, f),
					"fix apps.%s.compose in %s (file names are relative to the app's folder)", name, cfg.File())
			}
			files = append(files, f)
		}
	}
	// Passing --env-file stops compose reading the app's .env by itself, so it goes first.
	var envFiles []string
	if f := filepath.Join(dir, defaults.AppEnvFile); exists(f) {
		envFiles = append(envFiles, f)
	}
	for _, f := range cfg.EnvFiles(a) {
		if !exists(f) {
			return nil, withHint(fmt.Errorf("%s: env file %s not found", name, f),
				"create it, or fix apps.%s.env_files in %s (relative to the homelab folder)", name, cfg.File())
		}
		envFiles = append(envFiles, f)
	}
	return &compose.Project{
		Name:     cfg.ProjectName(name),
		Dir:      dir,
		Files:    files,
		EnvFiles: envFiles,
		Env:      cfg.AppEnv(name, a),
		Stdin:    stdin,
		Stdout:   stdout,
		Stderr:   stderr,
	}, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// prepared is the app's project with hl's override written and layered on,
// and the data folders its volumes use created.
func prepared(ctx context.Context, cfg *config.Config, name string) (*compose.Project, error) {
	a, err := cfg.App(name)
	if err != nil {
		return nil, err
	}
	p, err := project(cfg, name, a)
	if err != nil {
		return nil, err
	}
	m, err := p.Model(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
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
				return nil, withHint(fmt.Errorf("%s: %w", name, err), "set apps.%s.service in %s", name, cfg.File())
			}
		}
		if g.Port == 0 {
			if g.Port, err = m.TargetPort(g.Service); err != nil {
				return nil, withHint(fmt.Errorf("%s: %w", name, err), "set apps.%s.port in %s", name, cfg.File())
			}
		}
	}
	override, dirs, err := gen.Override(g, m)
	if err != nil {
		return nil, withHint(err, "set apps.%s.service in %s to one of the app's services", name, cfg.File())
	}
	for _, d := range append(dirs, cfg.StateDir(name)) {
		if err := os.MkdirAll(d, defaults.DataDirMode); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	f := cfg.OverrideFile(name)
	if err := writeGenerated(f, override); err != nil {
		return nil, err
	}
	p.Files = append(p.Files, f)
	return p, nil
}

// writeGenerated writes a generated file atomically, and only when it changed.
func writeGenerated(path, content string) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, []byte(content)) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), defaults.DataDirMode); err != nil {
		return err
	}
	return fsutil.WriteFile(path, []byte(content), defaults.FileMode)
}

// proxy is the Traefik project, its files regenerated from hl.yml
// (proxyProject); it refuses a proxy or network another homelab runs.
func proxy(cfg *config.Config) (*compose.Project, error) {
	p, err := proxyProject(cfg)
	if err != nil {
		return nil, err
	}
	p.Stdin, p.Stdout, p.Stderr = stdin, stdout, stderr
	return p, nil
}

// ensureProxy starts the proxy first, so apps can join its network.
func ensureProxy(ctx context.Context, cfg *config.Config, inv *inventory) error {
	p, err := proxy(cfg)
	if err != nil {
		return err
	}
	if inv.running(p.Name) {
		return nil
	}
	return startProxy(ctx, cfg, p)
}

// startProxy checks the port and the network, then starts the proxy.
func startProxy(ctx context.Context, cfg *config.Config, p *compose.Project) error {
	if err := checkPort(ctx, cfg); err != nil {
		return err
	}
	if err := checkNetwork(ctx, cfg); err != nil {
		return err
	}
	step("proxy")
	if err := p.Run(ctx, "up", "--detach"); err != nil {
		return fmt.Errorf("starting the proxy: %w", err)
	}
	return nil
}

// downByName stops a project hl can't prepare any more (its folder or compose
// file is gone), from what Docker knows about it.
func downByName(ctx context.Context, project string) error {
	dir, err := os.MkdirTemp("", "hl-down-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	p := &compose.Project{Name: project, Dir: dir, Stdin: stdin, Stdout: stdout, Stderr: stderr}
	return p.Run(ctx, "down")
}
