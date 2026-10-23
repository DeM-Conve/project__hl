package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"ls"},
		Short:   "Apps, their state, health and URLs",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			if err := printStatus(cmd.Context(), cfg); err != nil {
				return err
			}
			syncHint(stdout, cfg)
			return nil
		},
	}
}

func table() *tabwriter.Writer { return tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0) }

func printStatus(ctx context.Context, cfg *config.Config) error {
	inv, err := containers(ctx)
	if err != nil {
		// Still show what hl.yml says; the states are unknown.
		warn("can't ask Docker for the apps' state: %v", err)
		if h := hintFor(err); h != "" {
			fmt.Fprintf(stderr, "%shint:%s %s\n", yellow, reset, h)
		}
	}
	proxyState := red + "stopped" + reset
	switch {
	case inv == nil:
		proxyState = "unknown"
	case inv.owner(cfg, cfg.ProxyProject()) != "":
		proxyState = yellow + "used by the homelab in " + inv.owner(cfg, cfg.ProxyProject()) + reset
	case inv.running(cfg.ProxyProject()):
		proxyState = green + "running" + reset
	}
	say("%sproxy%s %s  %s%s:%d%s  %shome %s%s", bold, reset, proxyState, dim, cfg.Proxy.Bind, cfg.Proxy.Port, reset, dim, cfg.Dir, reset)
	if u := cfg.DashboardURL(); u != "" {
		say("%sdashboard %s%s", dim, u, reset)
	}
	fmt.Fprintln(stdout)
	if len(cfg.Apps) == 0 {
		say("no apps yet - add one with `hl add <folder>`")
		return nil
	}
	w := table()
	fmt.Fprintf(w, "APP\tSTATE\tHEALTH\tURL\tFOLDER\n")
	for _, n := range cfg.Names() {
		a := cfg.Apps[n]
		u := cfg.URL(n, a)
		if u != "" {
			u += "/"
		} else {
			u = "-"
		}
		state, health := "?", "?"
		if inv != nil {
			if other := inv.owner(cfg, cfg.ProjectName(n)); other != "" {
				state, health = "other homelab", "-"
			} else {
				state, health = summarize(inv.of(cfg.ProjectName(n)))
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", n, state, health, u, cfg.AppDir(a))
	}
	return w.Flush()
}

// health is a container's healthcheck state from its docker ps status:
// healthy, unhealthy, starting, or "" without a healthcheck.
func health(c container) string {
	s := c.status
	switch {
	case strings.Contains(s, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(s, "(health: starting)"):
		return "starting"
	case strings.Contains(s, "(healthy)"):
		return "healthy"
	}
	return ""
}

// summarize is an app's STATE (up n/m, partial n/m, down) and HEALTH
// (healthy, starting, unhealthy: <services>, or - without healthchecks).
// tabwriter counts bytes, so no colour here.
func summarize(cs []container) (state, healthText string) {
	if len(cs) == 0 {
		return "down", "-"
	}
	up := 0
	var bad, starting []string
	checked, healthy := 0, 0
	for _, c := range cs {
		if c.state == "running" {
			up++
		}
		switch h := health(c); {
		case c.state == "restarting":
			bad = append(bad, c.service+" restarting")
		case c.state != "running":
			bad = append(bad, c.service+" "+c.state)
		case h == "unhealthy":
			checked++
			bad = append(bad, c.service)
		case h == "starting":
			checked++
			starting = append(starting, c.service)
		case h == "healthy":
			checked++
			healthy++
		}
	}
	state = fmt.Sprintf("up %d/%d", up, len(cs))
	if up < len(cs) {
		state = fmt.Sprintf("partial %d/%d", up, len(cs))
	}
	sort.Strings(bad)
	sort.Strings(starting)
	switch {
	case len(bad) > 0:
		return state, "unhealthy: " + strings.Join(uniq(bad), ", ")
	case len(starting) > 0:
		return state, "starting: " + strings.Join(uniq(starting), ", ")
	case checked > 0 && healthy == checked:
		return state, "healthy"
	}
	return state, "-"
}

func sortedServices(m map[string]compose.Service) []string {
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
