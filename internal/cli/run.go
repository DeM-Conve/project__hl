package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/interrupt"
)

// Commands that run apps.

// waitFlags are --wait and --wait-timeout, for commands that start apps.
type waitFlags struct {
	wait    bool
	timeout time.Duration
}

func (w *waitFlags) add(c *cobra.Command) {
	c.Flags().BoolVar(&w.wait, "wait", false, "wait until each app's service behind the proxy, and the services it depends on, run and are healthy; show the logs of any that aren't")
	c.Flags().DurationVar(&w.timeout, "wait-timeout", defaults.WaitTimeout, "how long --wait waits for each app")
}

// on reports whether hl waits at all.
func (w *waitFlags) on() bool { return w != nil && w.wait }

func (w *waitFlags) args(args ...string) []string {
	return append(args, "--wait", "--wait-timeout", strconv.Itoa(int(w.timeout.Seconds())))
}

// services are the services --wait waits for: the routed one and what it depends on, or nil
// for all of them (an app that isn't behind the proxy).
func (w *waitFlags) services(pa *preparedApp) []string {
	if pa.Service == "" {
		return nil
	}
	return pa.Model.WithDeps(pa.Service)
}

// failures collects the apps a multi-app command couldn't handle, so one
// broken app doesn't stop the others.
type failures struct {
	names []string
	last  error
	total int
}

func (f *failures) add(name string, err error) {
	f.names = append(f.names, name)
	f.last = err
	if f.total > 1 {
		printErr(err)
	}
}

func (f *failures) err() error {
	switch {
	case len(f.names) == 0:
		return nil
	case f.total == 1:
		return f.last
	}
	return fmt.Errorf("%d of %d apps failed: %s", len(f.names), f.total, strings.Join(f.names, ", "))
}

// eachApp runs a compose command for every target app, the proxy first when
// the apps need it. Another homelab's projects are never touched.
func eachApp(ctx context.Context, cfg *config.Config, names []string, needProxy bool, w *waitFlags, args ...string) error {
	inv, err := containers(ctx)
	if err != nil {
		return err
	}
	if needProxy {
		if err := ensureProxy(ctx, cfg, inv); err != nil {
			return err
		}
	}
	f := &failures{total: len(names)}
	for _, n := range names {
		if err := runApp(ctx, cfg, inv, n, w, args...); err != nil {
			if errors.Is(err, interrupt.ErrInterrupted) {
				return err
			}
			f.add(n, err)
		}
	}
	return f.err()
}

func runApp(ctx context.Context, cfg *config.Config, inv *inventory, name string, w *waitFlags, args ...string) error {
	if err := inv.mine(cfg, cfg.ProjectName(name), name); err != nil {
		return err
	}
	pa, err := prepare(ctx, cfg, name)
	if err != nil {
		return err
	}
	p := pa.Project
	step(name)
	// --wait: compose starts every service, then waits only for the ones that matter, so a
	// sidecar with a slow healthcheck doesn't fail an app that already serves.
	var waitFor []string
	if w.on() {
		if waitFor = w.services(pa); waitFor == nil {
			args = w.args(args...)
		}
	}
	if err := p.Run(ctx, args...); err != nil {
		if errors.Is(err, interrupt.ErrInterrupted) {
			return err
		}
		if w.on() && waitFor == nil {
			return unhealthy(ctx, p, name, err, nil)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	if waitFor != nil {
		say("%swaiting for %s%s", dim, strings.Join(waitFor, ", "), reset)
		if err := p.Run(ctx, append(w.args("up", "--detach"), waitFor...)...); err != nil {
			if errors.Is(err, interrupt.ErrInterrupted) {
				return err
			}
			return unhealthy(ctx, p, name, err, waitFor)
		}
	}
	return nil
}

// unhealthy explains an app that didn't come up under --wait: which of the services waited
// for (nil: all) aren't running or healthy, and their last log lines.
func unhealthy(ctx context.Context, p *compose.Project, name string, cause error, waited []string) error {
	inv, err := containers(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", name, cause)
	}
	var bad []string
	for _, c := range inv.of(p.Name) {
		if waited != nil && !slices.Contains(waited, c.service) {
			continue
		}
		if h := health(c); c.state != "running" || h == "unhealthy" || h == "starting" {
			bad = append(bad, c.service)
		}
	}
	if len(bad) == 0 {
		return fmt.Errorf("%s: %w", name, cause)
	}
	sort.Strings(bad)
	bad = uniq(bad)
	for _, s := range bad {
		say("%slast %d log lines of %s/%s:%s", dim, defaults.FailLogTail, name, s, reset)
		_ = p.Run(ctx, "logs", "--no-color", "--tail", strconv.Itoa(defaults.FailLogTail), s)
	}
	return withHint(fmt.Errorf("%s: not running and healthy: %s", name, strings.Join(bad, ", ")),
		"see `hl logs %s %s`; a slow app may need a longer --wait-timeout", name, bad[0])
}

func uniq(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// splitProxy takes the proxy out of a command's arguments: up, down, restart, rebuild, logs and
// show take it like an app (it is a reserved app name, so no app has it).
func splitProxy(args []string) (withProxy bool, apps []string) {
	for _, a := range args {
		if a == proxyArg {
			withProxy = true
		} else {
			apps = append(apps, a)
		}
	}
	return withProxy, apps
}

// appsCmd is a command over [app...] (all by default) that also takes the proxy: withProxy
// says it was named, names are the apps (none when only the proxy was named).
func appsCmd(use, short string, run func(ctx context.Context, cfg *config.Config, withProxy bool, names []string) error) *cobra.Command {
	return &cobra.Command{
		Use:               use + " [app...|proxy]",
		Short:             short,
		ValidArgsFunction: completeTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			withProxy, apps := splitProxy(args)
			var names []string
			if !withProxy || len(apps) > 0 {
				if names, err = targets(cfg, apps); err != nil {
					return err
				}
			}
			return run(cmd.Context(), cfg, withProxy, names)
		},
	}
}

// proxyThenApps runs proxyRun when the proxy was named, then apps when an app was named too,
// or none was.
func proxyThenApps(withProxy bool, names []string, proxyRun, apps func() error) error {
	if withProxy {
		if err := proxyRun(); err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
	}
	return apps()
}

func upCmd() *cobra.Command {
	var w waitFlags
	c := appsCmd("up", "Start apps (all by default), the proxy first; builds images that don't exist yet",
		func(ctx context.Context, cfg *config.Config, withProxy bool, names []string) error {
			err := proxyThenApps(withProxy, names,
				func() error { return upProxy(ctx, cfg) },
				func() error { return eachApp(ctx, cfg, names, true, &w, "up", "--detach") })
			return statusAfter(ctx, cfg, err)
		})
	c.Aliases = []string{"start"}
	w.add(c)
	return c
}

func rebuildCmd() *cobra.Command {
	var w waitFlags
	c := appsCmd("rebuild", "Rebuild apps' images from their folders' current code, then restart them",
		func(ctx context.Context, cfg *config.Config, withProxy bool, names []string) error {
			err := proxyThenApps(withProxy, names,
				func() error { return upProxy(ctx, cfg, "--force-recreate") },
				func() error { return eachApp(ctx, cfg, names, true, &w, "up", "--detach", "--build") })
			return statusAfter(ctx, cfg, err)
		})
	w.add(c)
	return c
}

func restartCmd() *cobra.Command {
	return appsCmd("restart", "Restart apps' containers without rebuilding (the proxy first, like up)",
		func(ctx context.Context, cfg *config.Config, withProxy bool, names []string) error {
			return proxyThenApps(withProxy, names,
				func() error { return restartProxy(ctx, cfg) },
				func() error { return eachApp(ctx, cfg, names, true, nil, "restart") })
		})
}

// proxyRunning takes the inventory and returns the proxy's project, refusing a proxy another
// homelab runs; running says whether it runs now.
func proxyRunning(ctx context.Context, cfg *config.Config) (p *compose.Project, running bool, err error) {
	inv, err := containers(ctx)
	if err != nil {
		return nil, false, err
	}
	if err := inv.mine(cfg, cfg.ProxyProject(), proxyArg); err != nil {
		return nil, false, err
	}
	if p, err = proxy(cfg); err != nil {
		return nil, false, err
	}
	return p, inv.running(p.Name), nil
}

// upProxy starts the proxy, or applies a changed config to a running one (compose recreates it
// only when its files changed, or always with --force-recreate in args).
func upProxy(ctx context.Context, cfg *config.Config, args ...string) error {
	p, running, err := proxyRunning(ctx, cfg)
	if err != nil {
		return err
	}
	if !running {
		return startProxy(ctx, cfg, p, args...)
	}
	step(proxyArg)
	if err := p.Run(ctx, append([]string{"up", "--detach"}, args...)...); err != nil {
		return fmt.Errorf("starting the proxy: %w", err)
	}
	return nil
}

// restartProxy restarts the proxy's container, or starts it when it isn't running.
func restartProxy(ctx context.Context, cfg *config.Config) error {
	p, running, err := proxyRunning(ctx, cfg)
	if err != nil {
		return err
	}
	if !running {
		return startProxy(ctx, cfg, p)
	}
	step(proxyArg)
	if err := p.Run(ctx, "restart"); err != nil {
		return fmt.Errorf("restarting the proxy: %w", err)
	}
	return nil
}

// statusAfter prints the status table after a command that started apps.
func statusAfter(ctx context.Context, cfg *config.Config, err error) error {
	if errors.Is(err, interrupt.ErrInterrupted) {
		return err
	}
	fmt.Fprintln(stdout)
	if serr := printStatus(ctx, cfg); err == nil {
		return serr
	}
	return err
}

func downCmd() *cobra.Command {
	c := &cobra.Command{
		Use:               "down [app...|proxy]",
		Aliases:           []string{"stop"},
		Short:             "Stop and remove apps' containers; data stays. With no app, also stops the proxy",
		ValidArgsFunction: completeTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := load()
			if err != nil {
				return err
			}
			withProxy, apps := splitProxy(args)
			names := cfg.Names()
			if len(args) > 0 {
				names = nil
				if len(apps) > 0 {
					if names, err = targets(cfg, apps); err != nil {
						return err
					}
				}
			}
			inv, err := containers(ctx)
			if err != nil {
				return err
			}
			f := &failures{total: len(names)}
			for _, n := range names {
				if err := downApp(ctx, cfg, inv, n); err != nil {
					if errors.Is(err, interrupt.ErrInterrupted) {
						return err
					}
					f.add(n, err)
				}
			}
			if len(args) > 0 && !withProxy {
				return f.err()
			}
			if err := downProxy(ctx, cfg, inv); err != nil {
				if f.err() != nil {
					printErr(err)
					return f.err()
				}
				return err
			}
			return f.err()
		},
	}
	return c
}

// downApp stops one app; one that is already down is left alone, so down is
// safe to repeat.
func downApp(ctx context.Context, cfg *config.Config, inv *inventory, name string) error {
	pn := cfg.ProjectName(name)
	if other := inv.owner(cfg, pn); other != "" {
		warn("%s: left alone - Docker project %s belongs to the homelab in %s", name, pn, other)
		return nil
	}
	if len(inv.of(pn)) == 0 {
		return nil
	}
	step(name)
	p, err := prepared(ctx, cfg, name)
	if err != nil {
		warn("%v - stopping it by its project name", err)
		err = downByName(ctx, pn)
	} else {
		err = p.Run(ctx, "down")
	}
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func downProxy(ctx context.Context, cfg *config.Config, inv *inventory) error {
	if other := inv.owner(cfg, cfg.ProxyProject()); other != "" {
		warn("proxy left alone - Docker project %s belongs to the homelab in %s", cfg.ProxyProject(), other)
		return nil
	}
	if len(inv.of(cfg.ProxyProject())) == 0 {
		return nil
	}
	p, err := proxy(cfg)
	if err != nil {
		return err
	}
	step("proxy")
	if err := p.Run(ctx, "down"); err != nil {
		return fmt.Errorf("stopping the proxy: %w", err)
	}
	return nil
}

func updateCmd() *cobra.Command {
	var w waitFlags
	c := appsCmd("update", "git pull apps' folders (fast-forward only, skips uncommitted changes), then rebuild",
		func(ctx context.Context, cfg *config.Config, withProxy bool, names []string) error {
			if withProxy {
				return withHint(errors.New("the proxy has no folder to pull"), "`hl up proxy` applies its settings")
			}
			if _, err := exec.LookPath("git"); err != nil {
				return withHint(errors.New("git not found on PATH"), "install git: update pulls each app's folder with it")
			}
			inv, err := containers(ctx)
			if err != nil {
				return err
			}
			if err := ensureProxy(ctx, cfg, inv); err != nil {
				return err
			}
			var updated, skipped []string
			f := &failures{total: len(names)}
			for _, n := range names {
				dir := cfg.AppDir(cfg.Apps[n])
				step(n + " " + dim + "(" + dir + ")" + reset)
				if why := dirty(ctx, dir); why != "" {
					say("%sskipped:%s %s", yellow, reset, why)
					skipped = append(skipped, n+" ("+why+")")
					continue
				}
				pull := exec.Command("git", "-C", dir, "pull", "--ff-only")
				pull.Stdout, pull.Stderr = stdout, stderr
				if err := compose.Exec(ctx, pull); err != nil {
					if errors.Is(err, interrupt.ErrInterrupted) {
						return err
					}
					f.add(n, withHint(fmt.Errorf("%s: git pull --ff-only failed; the folder is left as it was", n),
						"the branch may have diverged or have no upstream - sort it out in %s, then `hl update %s`", dir, n))
					continue
				}
				if err := runApp(ctx, cfg, inv, n, &w, "up", "--detach", "--build"); err != nil {
					if errors.Is(err, interrupt.ErrInterrupted) {
						return err
					}
					f.add(n, err)
					continue
				}
				updated = append(updated, n)
			}
			fmt.Fprintln(stdout)
			if len(updated) > 0 {
				say("updated: %s", strings.Join(updated, ", "))
			}
			if len(skipped) > 0 {
				say("%sskipped:%s %s", yellow, reset, strings.Join(skipped, "; "))
			}
			return statusAfter(ctx, cfg, f.err())
		})
	w.add(c)
	return c
}

// dirty is why update must not pull in dir, or "" when it may.
func dirty(ctx context.Context, dir string) string {
	out, err := run2(ctx, "git", "-C", dir, "status", "--porcelain")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not a git repository") {
			return "not a git repository"
		}
		return "git status failed: " + err.Error()
	}
	if n := len(strings.Split(strings.TrimSpace(out), "\n")); strings.TrimSpace(out) != "" {
		return fmt.Sprintf("uncommitted changes in %d file(s) - commit or stash them first", n)
	}
	return ""
}

func logsCmd() *cobra.Command {
	var follow bool
	var tail int
	c := &cobra.Command{
		Use:   "logs [app|proxy] [service...]",
		Short: "Show an app's or the proxy's logs; with no app, every app's, each line prefixed by its app",
		Args: func(cmd *cobra.Command, args []string) error {
			for _, s := range args[min(1, len(args)):] {
				if err := config.ValidService(s); err != nil {
					return err
				}
			}
			return nil
		},
		ValidArgsFunction: completeLogs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := load()
			if err != nil {
				return err
			}
			a := []string{"logs", "--tail", strconv.Itoa(tail)}
			if follow {
				a = append(a, "--follow")
			}
			if len(args) > 0 && args[0] == proxyArg {
				p, err := proxy(cfg)
				if err != nil {
					return err
				}
				return p.Run(ctx, append(a, args[1:]...)...)
			}
			if len(args) == 0 {
				names, err := targets(cfg, nil)
				if err != nil {
					return err
				}
				return logsAll(ctx, cfg, names, a)
			}
			if _, err := targets(cfg, args[:1]); err != nil {
				return err
			}
			p, err := prepared(ctx, cfg, args[0])
			if err != nil {
				return err
			}
			if err := p.Run(ctx, append(a, args[1:]...)...); err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			return nil
		},
	}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "keep following")
	c.Flags().IntVarP(&tail, "tail", "n", defaults.LogTail, "lines to show from the end of each log")
	return c
}

// logsAll runs `docker compose logs` for every app at once, each line
// prefixed by the app's name.
func logsAll(ctx context.Context, cfg *config.Config, names []string, args []string) error {
	width := 0
	for _, n := range names {
		width = max(width, len(n))
	}
	type proc struct {
		name string
		cmd  *exec.Cmd
		w    *io.PipeWriter
	}
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		procs []proc
		f     = &failures{total: len(names)}
	)
	for _, n := range names {
		p, err := prepared(ctx, cfg, n)
		if err != nil {
			f.add(n, err)
			continue
		}
		cmd := p.Command(append(args, "--no-color")...)
		r, w := io.Pipe()
		cmd.Stdout, cmd.Stderr = w, w
		if err := cmd.Start(); err != nil {
			_ = w.Close()
			f.add(n, &compose.Error{Cmd: "docker compose logs", Code: -1, Err: err})
			continue
		}
		prefix := fmt.Sprintf("%s%-*s |%s ", cyan, width, n, reset)
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			sc.Buffer(make([]byte, 64<<10), 1<<20)
			for sc.Scan() {
				mu.Lock()
				fmt.Fprintln(stdout, prefix+sc.Text())
				mu.Unlock()
			}
			_, _ = io.Copy(io.Discard, r)
		}()
		procs = append(procs, proc{n, cmd, w})
	}
	var stopped error
	for _, pr := range procs {
		err := interrupt.Wait(ctx, pr.cmd)
		_ = pr.w.Close()
		switch {
		case errors.Is(err, interrupt.ErrInterrupted):
			stopped = err
		case err != nil:
			code := -1
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			}
			f.add(pr.name, fmt.Errorf("%s: %w", pr.name, &compose.Error{Cmd: "docker compose logs", Code: code, Err: err}))
		}
	}
	wg.Wait()
	if stopped != nil {
		return stopped
	}
	return f.err()
}

func shellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell <app> [service]",
		Short: "Open a shell in an app's container (default: the service behind the proxy)",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.RangeArgs(1, 2)(cmd, args); err != nil {
				return err
			}
			if len(args) == 2 {
				return config.ValidService(args[1])
			}
			return nil
		},
		ValidArgsFunction: completeAppServices,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := load()
			if err != nil {
				return err
			}
			if _, err := targets(cfg, args[:1]); err != nil {
				return err
			}
			a := cfg.Apps[args[0]]
			svc := a.Service
			if len(args) == 2 {
				svc = args[1]
			}
			if svc == "" && cfg.Routing(a) == config.RoutingNone {
				return &usageError{err: errNoService, cmd: "hl shell"}
			}
			p, err := prepared(ctx, cfg, args[0])
			if err != nil {
				return err
			}
			if svc == "" {
				// The service behind the proxy, as prepared picked it.
				m, err := p.Model(ctx)
				if err != nil {
					return err
				}
				if svc, err = m.Exposed(); err != nil {
					return &usageError{err: errNoService, cmd: "hl shell"}
				}
			}
			err = p.Run(ctx, "exec", svc, "sh", "-c", defaults.Shell)
			var ce *compose.Error
			if errors.As(err, &ce) && ce.Code > 0 {
				if strings.Contains(ce.Stderr, "is not running") || strings.Contains(ce.Stderr, "no container") {
					return withHint(fmt.Errorf("%s: service %s is not running", args[0], svc), "start it with `hl up %s`", args[0])
				}
				// The shell's own exit status, as `docker exec` does.
				return &exitStatus{code: ce.Code}
			}
			return err
		},
	}
}
