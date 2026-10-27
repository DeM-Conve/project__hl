package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
			"point it at the folder: `hl set %s path=<folder>`, or `hl remove %s` if the app is gone", name, name)
	}
	var files []string
	if len(a.Compose) == 0 {
		f, err := compose.FindFile(dir)
		if err != nil {
			return nil, withHint(fmt.Errorf("%s: %w", name, err),
				"add a compose file to the folder, or name yours: `hl set %s compose=<file>[,<file>...]`", name)
		}
		files = []string{f}
	} else {
		for _, f := range a.Compose {
			f = filepath.Join(dir, f)
			if _, err := os.Stat(f); err != nil {
				return nil, withHint(fmt.Errorf("%s: compose file %s not found", name, f),
					"fix the list: `hl set %s compose=<file>[,<file>...]` (relative to the app's folder), or `hl unset %s compose` for the default", name, name)
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
				"create it, or fix the list: `hl set %s env_files=<file>[,<file>...]` (relative to the homelab folder) or `hl unset %s env_files`", name, name)
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
	pa, err := prepare(ctx, cfg, name)
	if err != nil {
		return nil, err
	}
	return pa.Project, nil
}

// preparedApp is a prepared project with what hl read to prepare it.
type preparedApp struct {
	*compose.Project
	Model *compose.Model
	// Service is the one behind the proxy, "" with routing none.
	Service string
}

// prepare is prepared, keeping the model and the routed service.
func prepare(ctx context.Context, cfg *config.Config, name string) (*preparedApp, error) {
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
				return nil, withHint(fmt.Errorf("%s: %w", name, err), "pick it: `hl set %s service=<service>`", name)
			}
		}
		if g.Port == 0 {
			if g.Port, err = m.TargetPort(g.Service); err != nil {
				return nil, withHint(fmt.Errorf("%s: %w", name, err), "say which: `hl set %s port=<port inside the container>`", name)
			}
		}
	}
	override, dirs, err := gen.Override(g, m)
	if err != nil {
		return nil, withHint(err, "pick one of the app's services: `hl set %s service=<service>` (`hl show %s --compose` lists them)", name, name)
	}
	dirs = append(dirs, envDataDirs(p.Env, cfg.DataDir(name))...)
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
	routed := ""
	if g.Routing != config.RoutingNone {
		routed = g.Service
	}
	return &preparedApp{Project: p, Model: m, Service: routed}, nil
}

// envDataDirs are the folders inside the app's data folder that its env points at (say
// BACKUP_LOCATION=${HL_DATA}/backups). hl creates them as the user, as it does for volumes;
// left to Docker, a missing bind-mount source is created owned by root, and an app that runs
// as the user can't write to it.
func envDataDirs(env []string, dataDir string) []string {
	var dirs []string
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "HL_") || !filepath.IsAbs(v) {
			continue
		}
		v = filepath.Clean(v)
		if v == dataDir || strings.HasPrefix(v, dataDir+string(filepath.Separator)) {
			dirs = append(dirs, v)
		}
	}
	return dirs
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

// startProxy checks the port and the network, then starts the proxy; args go to compose up.
func startProxy(ctx context.Context, cfg *config.Config, p *compose.Project, args ...string) error {
	if err := checkPort(ctx, cfg); err != nil {
		return err
	}
	if err := checkNetwork(ctx, cfg); err != nil {
		return err
	}
	step("proxy")
	if err := p.Run(ctx, append([]string{"up", "--detach"}, args...)...); err != nil {
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
	defer func() { _ = os.RemoveAll(dir) }()
	p := &compose.Project{Name: project, Dir: dir, Stdin: stdin, Stdout: stdout, Stderr: stderr}
	return p.Run(ctx, "down")
}
