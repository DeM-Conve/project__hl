// Package cli is the hl command line.
package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/version"
)

var homeFlag string

// Execute runs the command line and exits non-zero on failure.
func Execute() {
	if err := root().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%serror:%s %v\n", red, reset, err)
		os.Exit(1)
	}
}

func root() *cobra.Command {
	c := &cobra.Command{
		Use:   "hl",
		Short: "Run your self-hosted apps behind one proxy, each from its own folder, unmodified",
		Long: `hl runs apps that each come with their own docker compose file behind one
Traefik proxy, from a homelab folder that holds the config (hl.yml) and every
app's data (data/<app>/).

  hl init ~/homelab           create the homelab folder
  hl add ~/code/my-app        add an app (its folder, with a compose file)
  hl up                       start everything; hl status shows the URLs`,
		Version:       version.Get().Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	c.SetVersionTemplate(version.Get().String() + "\n")
	c.PersistentFlags().StringVar(&homeFlag, "home", "", "homelab folder (default: $HL_HOME, the nearest folder with an hl.yml, or the one from `hl init`)")
	c.AddCommand(initCmd(), addCmd(), removeCmd(), statusCmd(),
		upCmd(), downCmd(), restartCmd(), rebuildCmd(), updateCmd(),
		logsCmd(), psCmd(), openCmd(), shellCmd(), configCmd(), pathCmd(),
		proxyCmd(), doctorCmd(), versionCmd())
	c.AddCommand(syncCmd())
	return c
}

func load() (*config.Config, error) {
	home, err := config.Home(homeFlag)
	if err != nil {
		return nil, err
	}
	return config.Load(home)
}

// targets are the apps named on the command line, or all of them.
func targets(cfg *config.Config, args []string) ([]string, error) {
	if len(args) == 0 {
		if len(cfg.Apps) == 0 {
			return nil, errors.New("no apps yet - add one with `hl add <folder>`")
		}
		return cfg.Names(), nil
	}
	for _, a := range args {
		if _, err := cfg.App(a); err != nil {
			return nil, err
		}
	}
	return args, nil
}

// completeApps completes app names from hl.yml, leaving out the ones already typed.
func completeApps(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	cfg, err := load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return without(cfg.Names(), args), cobra.ShellCompDirectiveNoFileComp
}

// completeApp completes one app name: the command's only argument.
func completeApp(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeApps(cmd, args, toComplete)
}

// completeAppServices completes an app name, then that app's services.
func completeAppServices(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeApps(cmd, args, toComplete)
	}
	cfg, err := load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	a, err := cfg.App(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	p, err := project(cfg, args[0], a)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	m, err := p.Model()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	services := make([]string, 0, len(m.Services))
	for s := range m.Services {
		services = append(services, s)
	}
	sort.Strings(services)
	return without(services, args[1:]), cobra.ShellCompDirectiveNoFileComp
}

// without is names minus the ones in typed.
func without(names, typed []string) []string {
	seen := map[string]bool{}
	for _, t := range typed {
		seen[t] = true
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !seen[n] {
			out = append(out, n)
		}
	}
	return out
}

func addCmd() *cobra.Command {
	var a config.App
	var name string
	c := &cobra.Command{
		Use:   "add <folder>",
		Short: "Add an app: a folder with its own compose file",
		Long: `Add an app to hl.yml. hl finds the service that publishes a port and serves it
through the proxy instead; the app's files are not changed. Named volumes are
kept in <homelab>/data/<app>/<volume>.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			dir, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			if name == "" {
				name = strings.ToLower(filepath.Base(dir))
			}
			if err := config.ValidName(name); err != nil {
				return fmt.Errorf("%w - pass --name", err)
			}
			if _, ok := cfg.Apps[name]; ok {
				return fmt.Errorf("app %q is already added", name)
			}
			a.Path = relTo(cfg.Dir, dir)
			// Check it resolves, and pin the service and port hl picked.
			cfg.Apps[name] = &a
			if err := cfg.Validate(); err != nil {
				return err
			}
			p, err := project(cfg, name, &a)
			if err != nil {
				return err
			}
			m, err := p.Model()
			if err != nil {
				return err
			}
			if cfg.Routing(&a) != config.RoutingNone {
				if a.Service == "" {
					if a.Service, err = m.Exposed(); err != nil {
						return err
					}
				}
				if a.Port == 0 {
					if a.Port, err = m.TargetPort(a.Service); err != nil {
						return err
					}
				}
			}
			if err := upgradeConfig(cfg.Dir); err != nil {
				return err
			}
			if err := config.AddApp(cfg.Dir, name, &a); err != nil {
				return err
			}
			say("added %s%s%s (service %s, port %d)", bold, name, reset, a.Service, a.Port)
			if u := cfg.URL(name, &a); u != "" {
				say("url: %s/", u)
			}
			if cfg.Routing(&a) == config.RoutingPath {
				say("%snote:%s path routing serves it under %s, with the prefix stripped. The app must build its\n"+
					"links for that prefix: map ${HL_PREFIX} to its base-path setting in hl.yml (apps.%s.env),\n"+
					"or use --routing host.", yellow, reset, cfg.Prefix(name, &a), name)
			}
			say("start it: hl up %s", name)
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&name, "name", "", "app name (default: the folder's name)")
	f.StringVar(&a.Routing, "routing", "", "path, host or none (default: proxy.routing)")
	f.StringVar(&a.Prefix, "prefix", "", "URL prefix for path routing (default: /<name>)")
	f.StringVar(&a.Host, "host", "", "host name for host routing (default: <name>.<domain>)")
	f.StringVar(&a.Service, "service", "", "service the proxy forwards to (default: the one that publishes a port)")
	f.IntVar(&a.Port, "port", 0, "that service's port in its container (default: the one it publishes)")
	f.StringSliceVar(&a.Compose, "compose", nil, "compose files in the folder (default: the one docker compose picks)")
	return c
}

func removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "remove <app>",
		Short:             "Stop an app and remove it from hl.yml; its data stays in data/<app>",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeApp,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			name := args[0]
			if _, err := cfg.App(name); err != nil {
				return err
			}
			if p, err := prepared(cfg, name); err == nil {
				step(name)
				if err := p.Run("down"); err != nil {
					return err
				}
			}
			if err := upgradeConfig(cfg.Dir); err != nil {
				return err
			}
			if err := config.RemoveApp(cfg.Dir, name); err != nil {
				return err
			}
			_ = os.RemoveAll(cfg.StateDir(name))
			if _, err := os.Stat(cfg.DataDir(name)); err == nil {
				say("removed %s; its data is still in %s", name, cfg.DataDir(name))
			} else {
				say("removed %s", name)
			}
			return nil
		},
	}
}

// eachApp runs a compose command for every target app, proxy first when the
// apps need it.
func eachApp(use, short string, needProxy bool, compose func(name string) []string, after func(*config.Config) error) *cobra.Command {
	return &cobra.Command{
		Use:               use + " [app...]",
		Short:             short,
		ValidArgsFunction: completeApps,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			names, err := targets(cfg, args)
			if err != nil {
				return err
			}
			if needProxy {
				if err := ensureProxy(cfg); err != nil {
					return err
				}
			}
			for _, n := range names {
				p, err := prepared(cfg, n)
				if err != nil {
					return err
				}
				step(n)
				if err := p.Run(compose(n)...); err != nil {
					return fmt.Errorf("%s: %w", n, err)
				}
			}
			if after != nil {
				return after(cfg)
			}
			return nil
		},
	}
}

func upCmd() *cobra.Command {
	c := eachApp("up", "Start apps (all by default); builds images that don't exist yet", true,
		func(string) []string { return []string{"up", "--detach"} }, printStatus)
	c.Aliases = []string{"start"}
	return c
}

func rebuildCmd() *cobra.Command {
	return eachApp("rebuild", "Rebuild apps' images from their folders' current code, then restart them", true,
		func(string) []string { return []string{"up", "--detach", "--build"} }, printStatus)
}

func restartCmd() *cobra.Command {
	return eachApp("restart", "Restart apps' containers without rebuilding", false,
		func(string) []string { return []string{"restart"} }, nil)
}

func downCmd() *cobra.Command {
	c := eachApp("down", "Stop and remove apps' containers; data stays. With no app, also stops the proxy", false,
		func(string) []string { return []string{"down"} }, nil)
	c.Aliases = []string{"stop"}
	run := c.RunE
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if err := run(cmd, args); err != nil {
			return err
		}
		if len(args) > 0 {
			return nil
		}
		cfg, err := load()
		if err != nil {
			return err
		}
		p, err := proxy(cfg)
		if err != nil {
			return err
		}
		step("proxy")
		return p.Run("down")
	}
	return c
}

func updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "update [app...]",
		Short:             "git pull apps' folders (fast-forward only, skips uncommitted changes), then rebuild",
		ValidArgsFunction: completeApps,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			names, err := targets(cfg, args)
			if err != nil {
				return err
			}
			if err := ensureProxy(cfg); err != nil {
				return err
			}
			for _, n := range names {
				dir := cfg.AppDir(cfg.Apps[n])
				step(n + " " + dim + "(" + dir + ")" + reset)
				if out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output(); err != nil {
					say("%sskipped:%s not a git repository", yellow, reset)
					continue
				} else if len(strings.TrimSpace(string(out))) > 0 {
					say("%sskipped:%s uncommitted changes - commit or stash them first", yellow, reset)
					continue
				}
				pull := exec.Command("git", "-C", dir, "pull", "--ff-only")
				pull.Stdout, pull.Stderr = os.Stdout, os.Stderr
				if err := pull.Run(); err != nil {
					say("%spull failed%s - leaving %s as it is", red, reset, n)
					continue
				}
				p, err := prepared(cfg, n)
				if err != nil {
					return err
				}
				if err := p.Run("up", "--detach", "--build"); err != nil {
					return fmt.Errorf("%s: %w", n, err)
				}
			}
			return printStatus(cfg)
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"ls"},
		Short:   "Apps, their state and URLs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			if err := printStatus(cfg); err != nil {
				return err
			}
			syncHint(cmd.OutOrStdout(), cfg)
			return nil
		},
	}
}

func printStatus(cfg *config.Config) error {
	state := red + "stopped" + reset
	if running(cfg.Proxy.Project) {
		state = green + "running" + reset
	}
	say("%sproxy%s %s  %s%s:%d%s  %shome %s%s\n", bold, reset, state, dim, cfg.Proxy.Bind, cfg.Proxy.Port, reset, dim, cfg.Dir, reset)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "APP\tSTATE\tURL\tFOLDER\n")
	for _, n := range cfg.Names() {
		a := cfg.Apps[n]
		u := cfg.URL(n, a)
		if u != "" {
			u += "/"
		} else {
			u = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", n, appState(config.ProjectName(n)), u, cfg.AppDir(a))
	}
	return w.Flush()
}

// appState summarises a project's containers; tabwriter counts bytes, so no colour here.
func appState(project string) string {
	out, err := exec.Command("docker", "ps", "-a", "--filter", "label=com.docker.compose.project="+project, "--format", "{{.State}}|{{.Status}}").Output()
	if err != nil {
		return "?"
	}
	lines := strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(out)), " ", "_"))
	if len(lines) == 0 {
		return "down"
	}
	up, unhealthy, starting := 0, 0, 0
	for _, l := range lines {
		if strings.HasPrefix(l, "running") {
			up++
		}
		if strings.Contains(l, "unhealthy") || strings.HasPrefix(l, "restarting") {
			unhealthy++
		} else if strings.Contains(l, "health:_starting") {
			starting++
		}
	}
	switch {
	case unhealthy > 0:
		return fmt.Sprintf("unhealthy %d/%d", up, len(lines))
	case up < len(lines):
		return fmt.Sprintf("partial %d/%d", up, len(lines))
	case starting > 0:
		return fmt.Sprintf("starting %d/%d", up, len(lines))
	}
	return fmt.Sprintf("up %d/%d", up, len(lines))
}

func logsCmd() *cobra.Command {
	var follow bool
	c := &cobra.Command{
		Use:               "logs <app> [service...]",
		Short:             "Show an app's logs",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeAppServices,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := appProject(args[0])
			if err != nil {
				return err
			}
			a := []string{"logs", "--tail", "200"}
			if follow {
				a = append(a, "--follow")
			}
			return p.Run(append(a, args[1:]...)...)
		},
	}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "keep following")
	return c
}

func psCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "ps [app]",
		Short:             "List containers (of one app, or of the whole homelab)",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeApp,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				p, err := appProject(args[0])
				if err != nil {
					return err
				}
				return p.Run("ps")
			}
			ps := exec.Command("docker", "ps", "-a", "--filter", "label=com.docker.compose.project",
				"--format", "table {{.Label \"com.docker.compose.project\"}}\t{{.Names}}\t{{.Status}}")
			out, err := ps.Output()
			if err != nil {
				return err
			}
			for i, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
				if i == 0 || strings.HasPrefix(l, "hl-") {
					fmt.Println(l)
				}
			}
			return nil
		},
	}
}

func openCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "open <app>",
		Short:             "Print an app's URL and open it in the browser",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeApp,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			a, err := cfg.App(args[0])
			if err != nil {
				return err
			}
			u := cfg.URL(args[0], a)
			if u == "" {
				return fmt.Errorf("%s is not behind the proxy (routing: none)", args[0])
			}
			u += "/"
			fmt.Println(u)
			opener := "xdg-open"
			if runtime.GOOS == "darwin" {
				opener = "open"
			}
			// Headless or over ssh: the printed URL is enough.
			if _, err := exec.LookPath(opener); err == nil && (runtime.GOOS == "darwin" || os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "") {
				_ = exec.Command(opener, u).Start()
			}
			return nil
		},
	}
}

func shellCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "shell <app> [service]",
		Short:             "Open a shell in an app's container (default: the service behind the proxy)",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completeAppServices,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			p, err := prepared(cfg, args[0])
			if err != nil {
				return err
			}
			svc := cfg.Apps[args[0]].Service
			if len(args) == 2 {
				svc = args[1]
			}
			if svc == "" {
				return errors.New("which service? hl shell <app> <service>")
			}
			return p.Run("exec", svc, "sh", "-c", "command -v bash >/dev/null && exec bash || exec sh")
		},
	}
}

func configCmd() *cobra.Command {
	c := &cobra.Command{
		Use:               "config <app>|proxy",
		Short:             "Show an app's compose config as hl runs it (its own files + hl's override), or the proxy's Traefik config",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeApp,
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == proxyArg {
				return printProxyConfig()
			}
			p, err := appProject(args[0])
			if err != nil {
				return err
			}
			return p.Run("config")
		},
	}
	c.AddCommand(configCheckCmd())
	return c
}

func pathCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "path [app]",
		Short:             "Print an app's folder, or with no app the homelab folder  (cd \"$(hl path my-app)\")",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeApp,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				fmt.Println(cfg.Dir)
				return nil
			}
			a, err := cfg.App(args[0])
			if err != nil {
				return err
			}
			fmt.Println(cfg.AppDir(a))
			return nil
		},
	}
}

func proxyCmd() *cobra.Command {
	c := &cobra.Command{Use: "proxy", Short: "The Traefik proxy on its own"}
	sub := func(use, short string, compose ...string) *cobra.Command {
		return &cobra.Command{
			Use: use, Short: short, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := load()
				if err != nil {
					return err
				}
				p, err := proxy(cfg)
				if err != nil {
					return err
				}
				return p.Run(compose...)
			},
		}
	}
	c.AddCommand(
		sub("up", "Start the proxy", "up", "--detach"),
		sub("down", "Stop the proxy", "down"),
		sub("logs", "Show the proxy's logs", "logs", "--tail", "200"),
		proxyStatusCmd(), proxyConfigCmd(),
	)
	return c
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check docker, the homelab folder and every app",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			bad := 0
			check := func(ok bool, what, fix string) {
				if ok {
					say("%s✓%s %s", green, reset, what)
					return
				}
				bad++
				say("%s✗%s %s%s", red, reset, what, map[bool]string{true: "", false: " - " + fix}[fix == ""])
			}
			_, err := exec.LookPath("docker")
			check(err == nil, "docker is installed", "install Docker Engine")
			check(exec.Command("docker", "info").Run() == nil, "the docker daemon is reachable", "start docker, or add yourself to the docker group")
			v, err := exec.Command("docker", "compose", "version", "--short").Output()
			check(err == nil && composeAtLeast(strings.TrimSpace(string(v)), 2, 24),
				"docker compose v2.24+ ("+strings.TrimSpace(string(v))+")", "update the docker compose plugin")
			cfg, err := load()
			check(err == nil, "homelab config loads", fmt.Sprint(err))
			if err != nil {
				return fmt.Errorf("%d problem(s)", bad)
			}
			doctorProxy(cfg, check)
			for _, n := range cfg.Names() {
				_, err := prepared(cfg, n)
				check(err == nil, "app "+n, fmt.Sprint(err))
			}
			if bad > 0 {
				return fmt.Errorf("%d problem(s)", bad)
			}
			return nil
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print hl's version, commit and build date",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.Get())
		},
	}
}

func appProject(name string) (*compose.Project, error) {
	cfg, err := load()
	if err != nil {
		return nil, err
	}
	return prepared(cfg, name)
}

// composeAtLeast compares a compose version like "2.29.1" or "5.1.4".
func composeAtLeast(v string, major, minor int) bool {
	var ma, mi int
	if _, err := fmt.Sscanf(strings.TrimPrefix(v, "v"), "%d.%d", &ma, &mi); err != nil {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}

// relTo is target relative to base when it is inside base, else absolute.
func relTo(base, target string) string {
	if r, err := filepath.Rel(base, target); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return target
}
