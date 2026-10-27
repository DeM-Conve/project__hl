package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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
	checkLatest(ctx, c, version.Get().Version)

	// Docker.
	path, err := exec.LookPath("docker")
	if err != nil {
		c.fail("docker is not installed (not on PATH)", "install Docker: https://docs.docker.com/engine/install/")
		return c.result()
	}
	client, _ := docker(ctx, "version", "--format", "{{.Client.Version}}")
	c.ok(fmt.Sprintf("docker %s (%s)", strings.TrimSpace(client), path))
	server, err := docker(ctx, "info", "--format", "{{.ServerVersion}} on {{.OperatingSystem}} ({{.OSType}}/{{.Architecture}})")
	daemon := err == nil
	if daemon {
		c.ok("docker daemon " + strings.TrimSpace(server))
	} else {
		c.check(err, "docker daemon")
	}
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
			"if that isn't what you want: `hl set proxy bind="+defaults.Bind+"`")
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
						"give this homelab a name of its own ("+nameHowTo+")")
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

// latestReleaseURL is where checkLatest asks for the latest release; tests replace it.
var latestReleaseURL = defaults.LatestReleaseURL

// checkLatest warns when this hl is older than the latest release. A development build is
// not compared; a `go install` of a commit is told how to install a release. Offline, or when
// GitHub doesn't answer in time, it says nothing: doctor checks this machine, not the network.
func checkLatest(ctx context.Context, c *checks, current string) {
	switch version.Kind(current) {
	case version.KindDev:
		return
	case version.KindPseudo:
		c.warn("hl "+current+" is a build of a commit (go install), not a release",
			"install a release: "+defaults.InstallCommand+"  (or download one from "+defaults.ReleasesPage+")")
		return
	}
	latest := latestRelease(ctx)
	switch {
	case latest == "":
	case version.Newer(latest, current):
		c.warn("hl "+current+" is out of date: "+latest+" is the latest release",
			"update: "+defaults.InstallCommand+"  (what changed: "+defaults.ReleasesPage+")")
	default:
		c.ok("hl " + current + " is the latest release")
	}
}

// latestRelease is the tag of the latest GitHub release, or "" when it can't be had quickly.
func latestRelease(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, defaults.UpdateCheckTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil || version.Kind(r.Tag) != version.KindRelease {
		return ""
	}
	return r.Tag
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
	return &cobra.Command{
		Use:   "version",
		Short: "Print hl's version, commit, build date and platform (for bug reports)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(stdout, version.Get().Full())
			return nil
		},
	}
}
