package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/scan"
)

// Commands that manage the homelab folder and hl.yml.

func addCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <folder> [key=value...]",
		Short: "Add an app: a folder with its own compose file",
		Long: `Add an app to hl.yml. hl finds the service that publishes a port and serves it
through the proxy instead; the app's files are not changed. Named volumes are
kept in <homelab>/data/<app>/<volume>.

Settings after the folder are the ones hl set <app> takes, plus name (default: the
folder's name, less a strip_prefix): ` + strings.Join(addKeys(), ", ") + `.
service and port pick what the proxy forwards to when hl can't tell on its own.

hl also fills in apps.<app>.env from the variables the compose file uses:
<X>_BASE_PATH gets ${HL_PREFIX} (path routing), <X>_URL ${HL_URL} (when
<X>_BASE_PATH exists too), DATA_LOCATION ${HL_DATA} and BACKUP_LOCATION
${HL_DATA}/backups. env.NAME=value sets one more (or another value) and wins over
the mapped ones; env.NAME= with no value leaves that variable out.`,
		Example: `  hl add ~/code/blog
  hl add ~/code/wiki routing=host env.TZ=Europe/Berlin 'env.WIKI_TITLE=My wiki'
  hl add ~/code/tool name=tools service=web port=8080
  hl add ~/code/db routing=none`,
		Args: cobra.MinimumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return nil, cobra.ShellCompDirectiveFilterDirs
			}
			var out []string
			for _, k := range addKeys() {
				if !strings.HasSuffix(k, ".") {
					k += "="
				}
				out = append(out, k)
			}
			return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
		},
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
			var a config.App
			name, drop, err := addSettings(&a, dir, args[1:])
			if err != nil {
				return err
			}
			if name == "" {
				name = config.NameFor(config.Stripped(filepath.Base(dir), cfg.StripPrefix))
			}
			if err := config.ValidName(name); err != nil {
				return withHint(err, "give it another: hl add %s name=<name>", args[0])
			}
			if _, ok := cfg.Apps[name]; ok {
				return withHint(fmt.Errorf("app %q is already added", name), "pick another name with name=<other>, or `hl remove %s` first", name)
			}
			for _, n := range cfg.Names() {
				if o := cfg.Apps[n]; cfg.AppDir(o) == dir && sameCompose(o.Compose, a.Compose) && (a.Service == "" || a.Service == o.Service) {
					return withHint(fmt.Errorf("%s is already added as app %q", dir, n), "to add it again, give it name=<other> with service=<another service> or compose=<other files>")
				}
			}
			a.Path = relTo(cfg.Dir, dir)
			// Check it resolves, and pin the service and port hl picked.
			cfg.Apps[name] = &a
			if err := cfg.Validate(); err != nil {
				return withHint(err, "fix the settings (see `hl add --help`)")
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
					return fmt.Errorf("%s: %w", name, err)
				}
			}
			if vars, err := p.Variables(cmd.Context()); err != nil {
				warn("env not mapped: %v", err)
			} else if mapped := scan.AutoEnv(vars, cfg.Routing(&a)); mapped != nil {
				for k, v := range a.Env { // env.NAME=value wins
					mapped[k] = v
				}
				a.Env = mapped
			}
			for _, k := range drop {
				delete(a.Env, k)
			}
			if len(a.Env) == 0 {
				a.Env = nil
			}
			if err := upgradeConfig(cfg.Dir); err != nil {
				return err
			}
			if err := config.AddApp(cfg.Dir, name, &a); err != nil {
				return err
			}
			// Added by hand after hl remove (or hl set ignore+=): hl sync may see it again.
			for _, e := range cfg.Ignore {
				if ok, _ := filepath.Match(e, filepath.Base(dir)); ok && (e == filepath.Base(dir) || e == scan.IgnoreEntry(filepath.Base(dir))) {
					if err := config.Edit(cfg.Dir, config.HomelabScope, []config.Change{{Key: "ignore", Value: e, Op: config.OpRemove}}); err != nil {
						warn("%s stays in ignore: %v", e, err)
					} else {
						say("removed %s from ignore", e)
					}
				}
			}
			if cfg.Routing(&a) == config.RoutingNone {
				say("added %s%s%s (not behind the proxy)", bold, name, reset)
			} else {
				say("added %s%s%s (service %s, port %d)", bold, name, reset, a.Service, a.Port)
				say("url: %s/", cfg.URL(name, &a))
			}
			printEnv(stdout, "", a.Env)
			if cfg.Routing(&a) != config.RoutingNone && !scan.LooksWeb(a.Service, a.Port) {
				// The heuristic hl init and hl sync skip such folders by.
				warn("not a web app? (service %s, port %d) - run it without the proxy: hl set %s routing=none", a.Service, a.Port, name)
			} else if cfg.Routing(&a) == config.RoutingPath && !mapsBasePath(a.Env) {
				say("%shint:%s if its links break under %s: hl set %s env.<BASE_PATH_VAR>='${HL_PREFIX}' (or routing=host)",
					yellow, reset, cfg.Prefix(name, &a), name)
			}
			say("start it: hl up %s", name)
			return nil
		},
	}
}

// addKeys are the settings hl add takes: name, then hl set <app>'s keys but path (the folder).
func addKeys() []string {
	keys := []string{"name"}
	for _, k := range config.AppKeys() {
		if k != "path" {
			keys = append(keys, k)
		}
	}
	return keys
}

// addSettings applies hl add's key=value arguments to a. name is the app's name if one was
// given; drop are the env variables given with no value (env.NAME=), left out of the mapping.
func addSettings(a *config.App, dir string, kvs []string) (name string, drop []string, err error) {
	var rest []string
	for _, kv := range kvs {
		k, v, ok := strings.Cut(kv, "=")
		switch {
		case !ok || k == "":
			return "", nil, &usageError{err: fmt.Errorf("%q: use key=value, e.g. port=8080", kv), cmd: "hl add"}
		case v == "" && !strings.HasPrefix(k, "env."):
			return "", nil, &usageError{err: fmt.Errorf("%q: no value", kv), cmd: "hl add"}
		case k == "name":
			name = v
		case k == "path":
			return "", nil, &usageError{err: errors.New("path is the folder, the first argument"), cmd: "hl add"}
		case strings.HasPrefix(k, "env.") && v == "":
			drop = append(drop, strings.TrimPrefix(k, "env."))
		default:
			rest = append(rest, kv)
		}
	}
	changes, err := parseSettings("<app>", rest)
	if err != nil {
		return "", nil, &usageError{err: err, cmd: "hl add"}
	}
	if changes, err = seedCompose(dir, nil, changes); err != nil {
		return "", nil, err
	}
	if err := config.ApplyApp(a, changes); err != nil {
		return "", nil, withHint(err, "`hl add --help` lists the settings")
	}
	return name, drop, nil
}

// seedCompose makes compose+=/compose-= start from the file compose picks on its own when the
// app names none (have is empty): that file is put in the list first, so adding an override
// doesn't drop it.
func seedCompose(dir string, have []string, changes []config.Change) ([]config.Change, error) {
	if len(have) > 0 {
		return changes, nil
	}
	for _, c := range changes {
		if c.Key != "compose" || c.Op == "" {
			continue
		}
		f, err := compose.FindFile(dir)
		if err != nil {
			return nil, withHint(err, "name every file: compose=<file>[,<file>...]")
		}
		return append([]config.Change{{Key: "compose", Value: filepath.Base(f)}}, changes...), nil
	}
	return changes, nil
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
			// In a projects folder, hl sync would add it straight back: ignore it.
			if dir := cfg.AppDir(cfg.Apps[name]); exists(dir) && scan.InProjects(cfg, dir) && !scan.Ignored(cfg, filepath.Base(dir)) {
				entry := scan.IgnoreEntry(filepath.Base(dir))
				if err := config.Edit(cfg.Dir, config.HomelabScope, []config.Change{{Key: "ignore", Value: entry, Op: config.OpAdd}}); err != nil {
					warn("hl sync may add it again: %v", err)
				} else {
					say("added %s to ignore, so hl sync doesn't add it again (undo: hl set ignore-=%s)", entry, entry)
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
				return withHint(err, "`hl status` lists the apps")
			}
			u := cfg.URL(args[0], a)
			if u == "" {
				return withHint(fmt.Errorf("%s is not behind the proxy (routing: none)", args[0]), "its compose file publishes its own ports: `hl show %s --compose`", args[0])
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

// relTo is target relative to base when it is inside base, else absolute.
func relTo(base, target string) string {
	r, err := filepath.Rel(base, target)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return target
	}
	return r
}

var errNoService = errors.New("which service? pass it: hl shell <app> <service>")
