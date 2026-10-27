package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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
	up := len(args) > 0 && args[0] == "up"
	if up && !slices.Contains(args, "--build") {
		// An image keeps the build args it was built with; rebuild the ones whose changed.
		if stale := staleBuilds(cfg, name, pa.Model); len(stale) > 0 {
			say("%sbuild settings changed - rebuilding %s%s", dim, strings.Join(stale, ", "), reset)
			if err := p.Run(ctx, append([]string{"build"}, stale...)...); err != nil {
				if errors.Is(err, interrupt.ErrInterrupted) {
					return err
				}
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
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
	if up {
		if err := recordBuilds(cfg, name, pa.Model); err != nil {
			warn("%s: %v", name, err)
		}
	}
	if waitFor != nil {
		deadline := time.Now().Add(w.timeout)
		say("%swaiting for %s%s", dim, strings.Join(waitFor, ", "), reset)
		if err := p.Run(ctx, append(w.args("up", "--detach"), waitFor...)...); err != nil {
			if errors.Is(err, interrupt.ErrInterrupted) {
				return err
			}
			return unhealthy(ctx, p, name, err, waitFor)
		}
		// Healthy containers aren't enough: the proxy picks the route up a moment later.
		return waitRoute(ctx, cfg, name, deadline)
	}
	return nil
}

// waitRoute asks the proxy for the app's URL until something other than the proxy's own 404
// answers (any answer of the app counts, its own 404 too), or fails at the deadline.
func waitRoute(ctx context.Context, cfg *config.Config, name string, deadline time.Time) error {
	u, err := url.Parse(cfg.URL(name, cfg.Apps[name]) + "/")
	if err != nil || u.Host == "" {
		return nil
	}
	// Where the proxy really listens (it keeps its address until `hl up proxy`).
	host, port := cfg.Proxy.Bind, cfg.Proxy.Port
	if h, p, ok := publishedAddr(ctx, cfg.ProxyProject()); ok {
		host, port = h, p
	}
	if unspecified(host) {
		host = "localhost"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	say("%swaiting for the route %s%s", dim, u, reset)
	for !routeReady(ctx, addr, u.Host, u.Path) {
		if time.Now().After(deadline) {
			return withHint(fmt.Errorf("%s: the proxy has no route to %s yet", name, u),
				"see `hl logs proxy` (`hl set proxy debug=true`, then `hl up proxy`, logs each request); a slow app may need a longer --wait-timeout")
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(defaults.RouteWaitInterval):
		}
	}
	return nil
}

// routeReady is probeRoute; tests replace it.
var routeReady = probeRoute

// probeRoute sends one request for path with this Host header to the proxy at addr and reports
// whether the app answered: any response but the proxy's own 404 for an unknown route.
func probeRoute(ctx context.Context, addr, host, path string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+path, nil)
	if err != nil {
		return false
	}
	req.Host = host
	client := &http.Client{
		Timeout:       defaults.RouteProbeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		return true
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return strings.TrimSpace(string(b)) != defaults.ProxyNotFound
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
// says it was named, names are the apps (none when only the proxy was named, all of them when
// nothing was).
func appsCmd(use, short string, run func(ctx context.Context, cfg *config.Config, withProxy bool, names []string, all bool) error) *cobra.Command {
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
			return run(cmd.Context(), cfg, withProxy, names, len(args) == 0)
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
	c := appsCmd("up", "Start apps (all by default), the proxy first; with no app, also applies the proxy's changed settings",
		func(ctx context.Context, cfg *config.Config, withProxy bool, names []string, all bool) error {
			// A bare hl up brings everything to what hl.yml says, the proxy included (compose
			// recreates it only when its files changed); hl up <app> only starts a missing proxy,
			// so one app doesn't take every other app's routes down for a moment.
			if all {
				withProxy = true
			}
			err := proxyThenApps(withProxy, names,
				func() error { return upProxy(ctx, cfg) },
				func() error { return eachApp(ctx, cfg, names, !withProxy, &w, "up", "--detach") })
			return statusAfter(ctx, cfg, err)
		})
	w.add(c)
	return c
}

func rebuildCmd() *cobra.Command {
	var w waitFlags
	var pull bool
	c := appsCmd("rebuild", "Rebuild apps' images from their folders' current code, then restart them; --pull git pulls first",
		func(ctx context.Context, cfg *config.Config, withProxy bool, names []string, _ bool) error {
			proxyArgs := []string{"--force-recreate"}
			if pull {
				proxyArgs = append(proxyArgs, "--pull", "always")
			}
			err := proxyThenApps(withProxy, names,
				func() error { return upProxy(ctx, cfg, proxyArgs...) },
				func() error {
					if pull {
						return pullAndRebuild(ctx, cfg, names, &w)
					}
					return eachApp(ctx, cfg, names, true, &w, "up", "--detach", "--build")
				})
			return statusAfter(ctx, cfg, err)
		})
	w.add(c)
	c.Flags().BoolVar(&pull, "pull", false, "first git pull each app's folder (fast-forward only; a folder that isn't a git repo or has uncommitted changes is rebuilt without a pull); for the proxy, pull its image")
	return c
}

func restartCmd() *cobra.Command {
	return appsCmd("restart", "Restart apps' containers without rebuilding (the proxy first, like up)",
		func(ctx context.Context, cfg *config.Config, withProxy bool, names []string, _ bool) error {
			return proxyThenApps(withProxy, names,
				func() error { return restartProxy(ctx, cfg) },
				func() error {
					if names = runningApps(ctx, cfg, names); len(names) == 0 {
						return nil
					}
					return eachApp(ctx, cfg, names, true, nil, "restart")
				})
		})
}

// runningApps leaves out the apps that have no containers at all (compose restart would do
// nothing for them) and says so for each; without Docker it keeps them all and lets compose
// say what is wrong.
func runningApps(ctx context.Context, cfg *config.Config, names []string) []string {
	inv, err := containers(ctx)
	if err != nil {
		return names
	}
	var out []string
	for _, n := range names {
		if len(inv.of(cfg.ProjectName(n))) == 0 {
			say("%s isn't running - start it with: hl up %s", n, n)
			continue
		}
		out = append(out, n)
	}
	return out
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
	// Compose replaces a running proxy whose files changed; if the new address is taken, the
	// old proxy is gone and the new one can't start. So check a new address first.
	if moved(ctx, cfg, p.Name) {
		if err := checkPort(ctx, cfg); err != nil {
			return fmt.Errorf("%w - the running proxy is left as it is", err)
		}
	}
	step(proxyArg)
	if err := p.Run(ctx, append([]string{"up", "--detach"}, args...)...); err != nil {
		return fmt.Errorf("starting the proxy: %w", err)
	}
	return nil
}

// moved reports whether the running proxy publishes another port than cfg's (or another
// address, where the old one doesn't hold the new one too), so that the new address must be
// checked before compose replaces it. False when Docker can't tell.
func moved(ctx context.Context, cfg *config.Config, project string) bool {
	host, port, ok := publishedAddr(ctx, project)
	return ok && addrDiffers(cfg, host, port)
}

// publishedAddr is the host address and port the running proxy publishes, as Docker reports
// them; ok is false when Docker can't tell or nothing is published.
func publishedAddr(ctx context.Context, project string) (host string, port int, ok bool) {
	out, err := docker(ctx, "ps", "--filter", "label="+composeProjectLabel+"="+project, "--format", "{{.Ports}}")
	if err != nil {
		return "", 0, false
	}
	suffix := fmt.Sprintf("->%d/tcp", defaults.ProxyContainerPort)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		for _, pub := range strings.Split(line, ",") {
			hostPort, ok := strings.CutSuffix(strings.TrimSpace(pub), suffix)
			if !ok {
				continue
			}
			h, p, err := net.SplitHostPort(hostPort)
			if err != nil {
				continue
			}
			if n, err := strconv.Atoi(p); err == nil {
				return h, n, true
			}
		}
	}
	return "", 0, false
}

// addrDiffers reports whether a proxy published on host:port isn't where cfg says it listens.
func addrDiffers(cfg *config.Config, host string, port int) bool {
	if port != cfg.Proxy.Port {
		return true
	}
	// Same port: a wildcard address on either side is the proxy's own.
	return host != cfg.Proxy.Bind && !unspecified(host) && !unspecified(cfg.Proxy.Bind)
}

func unspecified(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsUnspecified()
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

// statusAfter prints the status table after a command that started apps. An error is printed
// first, so it isn't lost above the table.
func statusAfter(ctx context.Context, cfg *config.Config, err error) error {
	if errors.Is(err, interrupt.ErrInterrupted) {
		return err
	}
	if err != nil {
		printErr(err)
	}
	fmt.Fprintln(stdout)
	if serr := printStatus(ctx, cfg); err == nil {
		return serr
	}
	return &exitStatus{code: exitFail}
}

func downCmd() *cobra.Command {
	c := &cobra.Command{
		Use:               "down [app...|proxy]",
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

// pullAndRebuild is rebuild --pull for apps: git pull --ff-only in each app's folder, then
// rebuild it. A folder that isn't a git repo or has uncommitted changes isn't pulled but is
// still rebuilt from the code it has; the summary says which and why.
func pullAndRebuild(ctx context.Context, cfg *config.Config, names []string, w *waitFlags) error {
	if _, err := exec.LookPath("git"); err != nil {
		return withHint(errors.New("git not found on PATH"), "install git: --pull pulls each app's folder with it, or rebuild without --pull")
	}
	inv, err := containers(ctx)
	if err != nil {
		return err
	}
	if err := ensureProxy(ctx, cfg, inv); err != nil {
		return err
	}
	var pulled, local []string
	f := &failures{total: len(names)}
	for _, n := range names {
		dir := cfg.AppDir(cfg.Apps[n])
		step(n + " " + dim + "(" + dir + ")" + reset)
		why := dirty(ctx, dir)
		if why == "" {
			pull := exec.Command("git", "-C", dir, "pull", "--ff-only")
			pull.Stdout, pull.Stderr = stdout, stderr
			if err := compose.Exec(ctx, pull); err != nil {
				if errors.Is(err, interrupt.ErrInterrupted) {
					return err
				}
				f.add(n, withHint(fmt.Errorf("%s: git pull --ff-only failed; the folder is left as it was", n),
					"the branch may have diverged or have no upstream - sort it out in %s, then `hl rebuild %s --pull`", dir, n))
				continue
			}
		}
		// A folder hl may not pull is still rebuilt from the code it has.
		if err := runApp(ctx, cfg, inv, n, w, "up", "--detach", "--build"); err != nil {
			if errors.Is(err, interrupt.ErrInterrupted) {
				return err
			}
			f.add(n, err)
			continue
		}
		if why == "" {
			pulled = append(pulled, n)
		} else {
			local = append(local, n+" ("+why+")")
		}
	}
	fmt.Fprintln(stdout)
	if len(pulled) > 0 {
		say("pulled and rebuilt: %s", strings.Join(pulled, ", "))
	}
	if len(local) > 0 {
		say("%spull skipped%s, rebuilt from the code in the folder: %s", yellow, reset, strings.Join(local, "; "))
	}
	return f.err()
}

// dirty is why rebuild --pull must not pull in dir, or "" when it may.
func dirty(ctx context.Context, dir string) string {
	out, err := run2(ctx, "git", "-C", dir, "status", "--porcelain")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not a git repository") {
			return "not a git repository"
		}
		return "git status failed: " + err.Error()
	}
	if n := len(strings.Split(strings.TrimSpace(out), "\n")); strings.TrimSpace(out) != "" {
		return "uncommitted changes in " + plural(n, "file", "files")
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
				if notRunning(ctx, cfg.ProxyProject(), proxyArg) {
					return nil
				}
				p, err := proxy(cfg)
				if err != nil {
					return err
				}
				out := &countWriter{w: stdout}
				p.Stdout = out
				if err := p.Run(ctx, append(a, args[1:]...)...); err != nil {
					return err
				}
				if out.n == 0 && !follow && !cfg.Proxy.Debug {
					// Traefik logs only errors unless debug is on.
					say("%sno proxy logs: Traefik logs only errors - for its INFO log and one line per request: hl set proxy debug=true, then hl up proxy%s", dim, reset)
				}
				return nil
			}
			if len(args) == 0 {
				names, err := targets(cfg, nil)
				if err != nil {
					return err
				}
				// Only the apps that have containers: the others have no logs.
				if inv, err := containers(ctx); err == nil {
					names = slices.DeleteFunc(names, func(n string) bool { return len(inv.of(cfg.ProjectName(n))) == 0 })
					if len(names) == 0 {
						say("no app is running, so there are no logs - start them with: hl up")
						return nil
					}
				}
				return logsAll(ctx, cfg, names, a)
			}
			if _, err := targets(cfg, args[:1]); err != nil {
				return err
			}
			if notRunning(ctx, cfg.ProjectName(args[0]), args[0]) {
				return nil
			}
			pa, err := prepare(ctx, cfg, args[0])
			if err != nil {
				return err
			}
			for _, s := range args[1:] {
				if _, ok := pa.Model.Services[s]; !ok {
					return &usageError{err: withHint(fmt.Errorf("%s has no service %q", args[0], s),
						"its services: %s", strings.Join(sortedServices(pa.Model.Services), ", ")), cmd: "hl logs"}
				}
			}
			p := pa.Project
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

// serviceRunning reports whether one of the containers runs service.
func serviceRunning(cs []container, service string) bool {
	for _, c := range cs {
		if c.service == service && c.state == "running" {
			return true
		}
	}
	return false
}

// countWriter counts what passes through it.
type countWriter struct {
	w io.Writer
	n int
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}

// notRunning says so and reports true when project has no containers at all (compose would
// print nothing); without Docker it reports false and lets compose say what is wrong.
func notRunning(ctx context.Context, project, name string) bool {
	inv, err := containers(ctx)
	if err != nil || len(inv.of(project)) > 0 {
		return false
	}
	say("%s isn't running, so it has no logs - start it with: hl up %s", name, name)
	return true
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
			stopped := withHint(fmt.Errorf("%s: service %s isn't running", args[0], svc), "start it with `hl up %s`", args[0])
			// Ask Docker first, so a stopped service gets this one message rather than
			// docker's own error line followed by it.
			if inv, err := containers(ctx); err == nil && !serviceRunning(inv.of(p.Name), svc) {
				return stopped
			}
			err = p.Run(ctx, "exec", svc, "sh", "-c", defaults.Shell)
			var ce *compose.Error
			if errors.As(err, &ce) && ce.Code > 0 {
				if strings.Contains(ce.Stderr, "is not running") || strings.Contains(ce.Stderr, "no container") {
					return stopped
				}
				// The shell's own exit status, as `docker exec` does.
				return &exitStatus{code: ce.Code}
			}
			return err
		},
	}
}
