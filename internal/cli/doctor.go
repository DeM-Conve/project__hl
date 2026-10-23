package cli

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/version"
)

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check docker, the homelab folder and every app (paste the output into bug reports)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return doctor(cmd.Context())
		},
	}
}

type checks struct{ bad, warned int }

func (c *checks) ok(what string) { say("%s✓%s %s", green, reset, what) }

func (c *checks) fail(what, fix string) {
	c.bad++
	say("%s✗%s %s", red, reset, what)
	if fix != "" {
		say("    %s%s%s", dim, fix, reset)
	}
}

func (c *checks) warn(what, fix string) {
	c.warned++
	say("%s!%s %s", yellow, reset, what)
	if fix != "" {
		say("    %s%s%s", dim, fix, reset)
	}
}

func (c *checks) check(err error, what string) bool {
	if err == nil {
		c.ok(what)
		return true
	}
	c.fail(what+": "+firstLine(err.Error()), hintFor(err))
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func doctor(ctx context.Context) error {
	c := &checks{}
	say("%s", version.Get().Full())

	// Docker.
	path, err := exec.LookPath("docker")
	if err != nil {
		c.fail("docker is not installed (not on PATH)", "install Docker: https://docs.docker.com/engine/install/")
		return c.result()
	}
	client, _ := docker(ctx, "version", "--format", "{{.Client.Version}}")
	c.ok(fmt.Sprintf("docker %s (%s)", strings.TrimSpace(client), path))
	server, err := docker(ctx, "info", "--format", "{{.ServerVersion}} on {{.OperatingSystem}} ({{.OSType}}/{{.Architecture}})")
	daemon := c.check(err, "docker daemon: "+strings.TrimSpace(server))
	v, err := docker(ctx, "compose", "version", "--short")
	v = strings.TrimSpace(v)
	switch {
	case err != nil:
		c.fail("docker compose plugin is missing", "install the compose plugin: https://docs.docker.com/compose/install/")
	case !composeAtLeast(v, defaults.ComposeMinMajor, defaults.ComposeMinMinor):
		c.fail(fmt.Sprintf("docker compose %s is too old", v), fmt.Sprintf("hl needs v%d.%d or newer - update the compose plugin", defaults.ComposeMinMajor, defaults.ComposeMinMinor))
	default:
		c.ok("docker compose " + v)
	}

	// The homelab.
	home, err := config.Home(homeFlag)
	if err != nil {
		c.fail("no homelab found", "create one with `hl init <folder>`")
		return c.result()
	}
	cfg, err := config.Load(home)
	if err != nil {
		c.fail("homelab "+home, "")
		for _, l := range strings.Split(err.Error(), "\n") {
			say("    %s", l)
		}
		return c.result()
	}
	c.ok(fmt.Sprintf("homelab %s (%d apps)", cfg.Dir, len(cfg.Apps)))

	// The proxy.
	p := cfg.Proxy
	if ip := net.ParseIP(p.Bind); ip != nil && !ip.IsLoopback() {
		c.warn(fmt.Sprintf("the proxy listens on %s: apps are reachable from your network, without a password", p.Bind),
			"set proxy.bind: "+defaults.Bind+" if that isn't what you want")
	}
	if p.Dashboard {
		if p.DashboardPublic {
			c.warn("the Traefik dashboard is on and reachable from your network, without a password", "")
		} else {
			c.ok("Traefik dashboard at " + cfg.DashboardURL())
		}
	}
	doctorProxy(cfg, func(ok bool, what, fix string) {
		if ok {
			c.ok(what)
		} else {
			c.fail(what, fix)
		}
	})
	if daemon {
		inv, err := containers(ctx)
		if c.check(err, "listing containers") {
			switch {
			case inv.owner(cfg, cfg.ProxyProject()) != "":
				// Reported by doctorProxy above.
			case inv.running(cfg.ProxyProject()):
				c.ok(fmt.Sprintf("proxy running on %s:%d", p.Bind, p.Port))
			default:
				if c.check(checkPort(ctx, cfg), fmt.Sprintf("proxy port %s:%d is free", p.Bind, p.Port)) {
					c.check(checkNetwork(ctx, cfg), "proxy network "+p.Network)
				}
			}
			for _, n := range cfg.Names() {
				if other := inv.owner(cfg, cfg.ProjectName(n)); other != "" {
					c.fail(fmt.Sprintf("app %s: Docker project %s belongs to the homelab in %s", n, cfg.ProjectName(n), other),
						"give this homelab a `name:` in hl.yml")
				}
			}
		}
	}

	// The apps: each must resolve (docker compose config) and get its override.
	for _, n := range cfg.Names() {
		_, err := prepared(ctx, cfg, n)
		c.check(err, "app "+n)
	}
	return c.result()
}

func (c *checks) result() error {
	fmt.Fprintln(stdout)
	if c.bad > 0 {
		return withHint(fmt.Errorf("%d problem(s) found", c.bad),
			"fix them and run `hl doctor` again; for a bug report, paste this output (check it for private paths first)")
	}
	say("all good%s", map[bool]string{true: fmt.Sprintf(" (%d warning(s))", c.warned), false: ""}[c.warned > 0])
	return nil
}

// composeAtLeast compares a compose version like "2.29.1" or "5.1.4".
func composeAtLeast(v string, major, minor int) bool {
	var ma, mi int
	if _, err := fmt.Sscanf(strings.TrimPrefix(v, "v"), "%d.%d", &ma, &mi); err != nil {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}

func versionCmd() *cobra.Command {
	var short bool
	c := &cobra.Command{
		Use:   "version",
		Short: "Print hl's version, commit, build date and platform (for bug reports)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if short {
				fmt.Fprintln(stdout, version.Get().Version)
				return nil
			}
			fmt.Fprintln(stdout, version.Get().Full())
			return nil
		},
	}
	c.Flags().BoolVar(&short, "short", false, "print only the version")
	return c
}
