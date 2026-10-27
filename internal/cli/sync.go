package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/scan"
)

func syncCmd() *cobra.Command {
	var yes, dry, noEnv bool
	c := &cobra.Command{
		Use:   "sync",
		Short: "Make hl.yml follow your projects folders: add new apps, remove ones whose folder is gone",
		Long: `Compare the projects folders in hl.yml (projects:) with its apps and show a plan:
  + add     a new subfolder with a compose file (service, port and env detected like hl add)
  - remove  an app whose folder is gone; its data in data/<app> stays
  ~ update  an app whose pinned service or port is no longer in its compose file
Folders matching ignore: in hl.yml, hidden ones, and ones with a ` + defaults.IgnoreFile + ` file are
skipped. Apps outside the projects folders are left alone.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			return runSync(cmd.InOrStdin(), cmd.OutOrStdout(), cfg, yes, dry, noEnv)
		},
	}
	c.Flags().BoolVarP(&yes, "yes", "y", false, "apply the plan without asking")
	c.Flags().BoolVar(&dry, "dry-run", false, "only show the plan")
	c.Flags().BoolVar(&noEnv, "no-env", false, "don't fill in the env of new apps from their compose variables")
	return c
}

func runSync(in io.Reader, out io.Writer, cfg *config.Config, yes, dry, noEnv bool) error {
	if len(cfg.Projects) == 0 {
		return withHint(fmt.Errorf("no projects folders in %s", config.FileName), "add one with hl projects add <folder>, e.g. hl projects add ~/code")
	}
	d, err := scan.Compare(cfg)
	if err != nil {
		return err
	}
	for _, r := range d.MissingRoots {
		fmt.Fprintf(out, "%swarning:%s projects folder %s not found - its apps are left as they are\n", yellow, reset, r)
	}
	plan := scan.Plan(cfg, d, modelFunc(cfg), varsFunc(cfg, noEnv))
	changes := 0
	for _, a := range plan {
		if a.Kind != scan.Skip {
			changes++
		}
	}
	if len(plan) == 0 {
		fmt.Fprintf(out, "%s is in sync with %s\n", config.FileName, strings.Join(cfg.Projects, ", "))
		return nil
	}
	printPlan(out, cfg, plan)
	if changes == 0 || dry {
		return nil
	}
	if !yes {
		if !stdinTerminal(in) {
			return errors.New("not a terminal - pass --yes to apply the plan, or --dry-run to only show it")
		}
		if !newPrompter(in, out).confirm(fmt.Sprintf("apply %d change(s) to %s?", changes, config.FileName), false) {
			fmt.Fprintln(out, "nothing changed")
			return nil
		}
	}
	return applyPlan(out, cfg, plan)
}

// modelFunc resolves an app's compose model the way hl add does.
func modelFunc(cfg *config.Config) scan.ModelFunc {
	return func(name string, a *config.App) (*compose.Model, error) {
		p, err := project(cfg, name, a)
		if err != nil {
			return nil, err
		}
		return p.Model(context.Background())
	}
}

// varsFunc lists an app's compose variables, for the env hl maps; nil with --no-env.
func varsFunc(cfg *config.Config, noEnv bool) scan.VarsFunc {
	if noEnv {
		return nil
	}
	return func(name string, a *config.App) ([]string, error) {
		p, err := project(cfg, name, a)
		if err != nil {
			return nil, err
		}
		return p.Variables(context.Background())
	}
}

// envMapSummary describes the env auto-mapping rules for `hl config defaults`.
func envMapSummary() string {
	parts := []string{
		"<X>" + defaults.EnvBasePathSuffix + "=" + defaults.EnvBasePathValue + " (path routing)",
		"<X>" + defaults.EnvURLSuffix + "=" + defaults.EnvURLValue + " (with <X>" + defaults.EnvBasePathSuffix + ")",
	}
	return strings.Join(append(parts, scan.EnvList(defaults.EnvByName)...), ", ")
}

// mapsBasePath reports whether env feeds ${HL_PREFIX} to some variable.
func mapsBasePath(env map[string]string) bool {
	for _, v := range env {
		if strings.Contains(v, "HL_PREFIX") {
			return true
		}
	}
	return false
}

// printEnv lists the env hl mapped for an app, one variable a line.
func printEnv(w io.Writer, indent string, env map[string]string) {
	for _, kv := range scan.EnvList(env) {
		fmt.Fprintf(w, "%senv %s\n", indent, kv)
	}
}

func printPlan(out io.Writer, cfg *config.Config, plan []scan.Action) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, a := range plan {
		switch a.Kind {
		case scan.Add:
			fmt.Fprintf(w, "  + add\t%s\t%s\t%s\n", a.Name, a.Path, pinned(a.Service, a.Port))
			for _, kv := range scan.EnvList(a.Env) {
				fmt.Fprintf(w, "\t\t  env %s\n", kv)
			}
			if a.Note != "" {
				fmt.Fprintf(w, "\t\t  note: %s\n", a.Note)
			}
		case scan.Remove:
			fmt.Fprintf(w, "  - remove\t%s\t%s\tfolder gone; %s stays\n", a.Name, a.Path, relTo(cfg.Dir, cfg.DataDir(a.Name)))
		case scan.Update:
			fmt.Fprintf(w, "  ~ update\t%s\t%s\t%s -> %s\n", a.Name, a.Path, a.Old, pinned(a.Service, a.Port))
		case scan.Skip:
			fmt.Fprintf(w, "  ! skip\t%s\t%s\t%s\n", a.Name, a.Path, a.Reason)
		}
	}
	_ = w.Flush()
}

func pinned(service string, port int) string {
	if service == "" {
		return "not behind the proxy"
	}
	return fmt.Sprintf("%s:%d", service, port)
}

// applyPlan writes the plan into hl.yml. A removed app is stopped first if
// it still runs; its data folder is never touched.
func applyPlan(out io.Writer, cfg *config.Config, plan []scan.Action) error {
	var added, noBase []string
	for _, a := range plan {
		switch a.Kind {
		case scan.Add:
			if err := config.AddApp(cfg.Dir, a.Name, &config.App{Path: a.Path, Service: a.Service, Port: a.Port, Env: a.Env}); err != nil {
				return err
			}
			added = append(added, a.Name)
			if !mapsBasePath(a.Env) {
				noBase = append(noBase, a.Name)
			}
			fmt.Fprintf(out, "added %s\n", a.Name)
			printEnv(out, "  ", a.Env)
		case scan.Remove:
			name := cfg.ProjectName(a.Name)
			if inv, err := containers(context.Background()); err == nil && inv.owner(cfg, name) == "" && len(inv.of(name)) > 0 {
				// The folder is gone, so stop it by project name alone.
				if err := downByName(context.Background(), name); err != nil {
					fmt.Fprintf(out, "%swarning:%s couldn't stop %s: %v\n", yellow, reset, a.Name, err)
				}
			}
			if err := config.RemoveApp(cfg.Dir, a.Name); err != nil {
				return err
			}
			_ = os.RemoveAll(cfg.StateDir(a.Name))
			fmt.Fprintf(out, "removed %s; its data stays in %s\n", a.Name, cfg.DataDir(a.Name))
		case scan.Update:
			if err := config.PinApp(cfg.Dir, a.Name, a.Service, a.Port); err != nil {
				return err
			}
			fmt.Fprintf(out, "updated %s: %s\n", a.Name, pinned(a.Service, a.Port))
		}
	}
	if len(added) > 0 {
		if len(noBase) > 0 && cfg.Proxy.Routing == config.RoutingPath {
			fmt.Fprintf(out, "%snote:%s path routing serves each app under /<app>/; an app must support that base path.\n"+
				"No base-path setting was found for %s: map ${HL_PREFIX} to its setting\n"+
				"(hl set <app> env.<NAME>='${HL_PREFIX}'), or set routing: host for it.\n", yellow, reset, strings.Join(noBase, ", "))
		}
		fmt.Fprintf(out, "start them: hl up %s\n", strings.Join(added, " "))
	}
	return nil
}

// syncHint tells `hl status` readers when hl sync has something to do: new folders it would
// add (each new folder's compose model is read, as sync does, so one sync skips - not a web
// app, a compose error - isn't counted and the hint doesn't stay forever) and apps whose
// folder is gone. Skipped folders aren't mentioned; hl sync lists them with the reason.
func syncHint(out io.Writer, cfg *config.Config) {
	if len(cfg.Projects) == 0 {
		return
	}
	d, err := scan.Compare(cfg)
	if err != nil || d.Changes() == 0 {
		return
	}
	adds := 0
	if len(d.New) > 0 {
		for _, a := range scan.PlanAdds(cfg, d, modelFunc(cfg), nil) {
			if a.Kind == scan.Add {
				adds++
			}
		}
	}
	var parts []string
	if adds > 0 {
		parts = append(parts, plural(adds, "new app", "new apps")+" in "+strings.Join(cfg.Projects, ", "))
	}
	if n := len(d.Gone); n > 0 {
		parts = append(parts, plural(n, "app's folder is", "apps' folders are")+" gone")
	}
	if len(parts) == 0 {
		return
	}
	fmt.Fprintf(out, "\n%s%s - run hl sync%s\n", yellow, strings.Join(parts, ", "), reset)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
