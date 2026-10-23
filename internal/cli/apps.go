package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// Commands that manage the homelab folder and hl.yml.

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
			if st, err := os.Stat(dir); err != nil || !st.IsDir() {
				return withHint(fmt.Errorf("folder %s not found", dir), "pass the app's folder: the one with its compose file")
			}
			if name == "" {
				name = config.NameFor(filepath.Base(dir))
			}
			if err := config.ValidName(name); err != nil {
				return withHint(err, "pass a name with --name")
			}
			if _, ok := cfg.Apps[name]; ok {
				return withHint(fmt.Errorf("app %q is already added", name), "pick another name with --name, or `hl remove %s` first", name)
			}
			for _, n := range cfg.Names() {
				if o := cfg.Apps[n]; cfg.AppDir(o) == dir && sameCompose(o.Compose, a.Compose) && (a.Service == "" || a.Service == o.Service) {
					return withHint(fmt.Errorf("%s is already added as app %q", dir, n), "to add it again, pass --name with --service (another service) or --compose (other files)")
				}
			}
			a.Path = relTo(cfg.Dir, dir)
			// Check it resolves, and pin the service and port hl picked.
			cfg.Apps[name] = &a
			if err := cfg.Validate(); err != nil {
				return withHint(err, "fix the flags (see `hl add --help`)")
			}
			p, err := project(cfg, name, &a)
			if err != nil {
				return err
			}
			m, err := p.Model(cmd.Context())
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if cfg.Routing(&a) != config.RoutingNone {
				if a.Service != "" {
					if _, ok := m.Services[a.Service]; !ok {
						return withHint(fmt.Errorf("%s: no service %q in its compose files", name, a.Service),
							"services: %s", strings.Join(sortedServices(m.Services), ", "))
					}
				}
				if err := pickServicePort(m, &a, stdin); err != nil {
					return withHint(fmt.Errorf("%s: %w", name, err), "services: %s", strings.Join(sortedServices(m.Services), ", "))
				}
			}
			if err := upgradeConfig(cfg.Dir); err != nil {
				return err
			}
			if err := config.AddApp(cfg.Dir, name, &a); err != nil {
				return err
			}
			if cfg.Routing(&a) == config.RoutingNone {
				say("added %s%s%s (not behind the proxy)", bold, name, reset)
			} else {
				say("added %s%s%s (service %s, port %d)", bold, name, reset, a.Service, a.Port)
				say("url: %s/", cfg.URL(name, &a))
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

func sameCompose(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

func removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "remove <app>",
		Short:             "Stop an app and remove it from hl.yml; its data stays in data/<app>",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeApp,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := load()
			if err != nil {
				return err
			}
			name := args[0]
			if _, err := cfg.App(name); err != nil {
				return withHint(err, "`hl status` lists the apps")
			}
			inv, err := containers(ctx)
			if err != nil {
				return err
			}
			if err := inv.mine(cfg, cfg.ProjectName(name), name); err != nil {
				return err
			}
			if len(inv.of(cfg.ProjectName(name))) > 0 {
				step(name)
				if p, perr := prepared(ctx, cfg, name); perr == nil {
					err = p.Run(ctx, "down")
				} else {
					warn("%v - stopping it by its project name", perr)
					err = downByName(ctx, cfg.ProjectName(name))
				}
				if err != nil {
					return withHint(fmt.Errorf("stopping %s: %w", name, err), "fix that and run `hl remove %s` again; hl.yml is unchanged", name)
				}
			}
			if err := upgradeConfig(cfg.Dir); err != nil {
				return err
			}
			if err := config.RemoveApp(cfg.Dir, name); err != nil {
				return err
			}
			_ = os.RemoveAll(cfg.StateDir(name))
			if exists(cfg.DataDir(name)) {
				say("removed %s; its data is still in %s", name, cfg.DataDir(name))
			} else {
				say("removed %s", name)
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
				return withHint(err, "`hl status` lists the apps")
			}
			u := cfg.URL(args[0], a)
			if u == "" {
				return withHint(fmt.Errorf("%s is not behind the proxy (routing: none)", args[0]), "`hl ps %s` shows its own ports", args[0])
			}
			u += "/"
			fmt.Fprintln(stdout, u)
			opener := defaults.OpenerLinux
			if goos == "darwin" {
				opener = defaults.OpenerMac
			}
			// Headless or over ssh: the printed URL is enough.
			if _, err := exec.LookPath(opener); err == nil && (goos == "darwin" || os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "") {
				o := exec.Command(opener, u)
				if err := o.Start(); err == nil {
					go func() { _ = o.Wait() }()
				}
			}
			return nil
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
			cfg, err := load()
			if err != nil {
				return err
			}
			p, err := prepared(cmd.Context(), cfg, args[0])
			if err != nil {
				return err
			}
			return p.Run(cmd.Context(), "config")
		},
	}
	c.AddCommand(configCheckCmd(), configDefaultsCmd())
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
				fmt.Fprintln(stdout, cfg.Dir)
				return nil
			}
			a, err := cfg.App(args[0])
			if err != nil {
				return withHint(err, "`hl status` lists the apps")
			}
			fmt.Fprintln(stdout, cfg.AppDir(a))
			return nil
		},
	}
}

// relTo is target relative to base when it is inside base, else absolute.
func relTo(base, target string) string {
	r, err := filepath.Rel(base, target)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return target
	}
	return r
}

var errNoService = errors.New("which service? pass it: hl shell <app> <service>")
