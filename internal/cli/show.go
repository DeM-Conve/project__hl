package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

func showCmd() *cobra.Command {
	var composeOut bool
	c := &cobra.Command{
		Use:   "show [app|proxy]",
		Short: "What hl knows about an app (folder, route, URL, env and where each value comes from), the proxy, or the homelab",
		Long: `Show what hl stores in hl.yml and works out from it, without opening the file.

With an app: its folder and compose files, the service and port behind the proxy, routing,
URL, prefix and host, the data folder, and every variable hl gives its compose files with
where the value comes from (hl, the shared env or the app's env). With proxy: its state and
health, where it listens, every proxy setting, and warnings (a running proxy that predates the
config, an old image). With no app: the homelab folder, the proxy's settings that hl.yml sets,
the shared env (with the time zone apps get), where hl init and hl sync look for apps, and the
apps.

Each value says where it comes from: hl.yml, default, or detected (from the app's compose
config). Change them with hl set / hl unset. hl show <app> --compose prints the app's whole
compose config as hl runs it: its own files plus hl's override.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTarget,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return showHomelab(cfg)
			}
			if args[0] == proxyArg {
				if composeOut {
					p, err := proxy(cfg)
					if err != nil {
						return err
					}
					return p.Run(cmd.Context(), "config")
				}
				return showProxy(cfg)
			}
			a, err := cfg.App(args[0])
			if err != nil {
				return withHint(err, "`hl show` lists the apps")
			}
			if composeOut {
				p, err := prepared(cmd.Context(), cfg, args[0])
				if err != nil {
					return err
				}
				return p.Run(cmd.Context(), "config")
			}
			showApp(cmd.Context(), cfg, args[0], a)
			return nil
		},
	}
	c.Flags().BoolVar(&composeOut, "compose", false, "print the whole compose config as hl runs it (the app's files plus hl's override)")
	return c
}

// from formats where a value comes from, dimmed.
func from(source string) string { return dim + source + reset }

func setIn(set bool) string {
	if set {
		return config.FileName
	}
	return "default"
}

func showApp(ctx context.Context, cfg *config.Config, name string, a *config.App) {
	w := table()
	row := func(k, v, src string) { fmt.Fprintf(w, "  %s\t%s\t%s\n", k, v, from(src)) }
	say("%s%s%s", bold, name, reset)
	row("folder", cfg.AppDir(a), config.FileName)
	routing := cfg.Routing(a)
	row("project", cfg.ProjectName(name), "")

	// The compose files and, unless pinned, the service and port hl routes to.
	var model *compose.Model
	var modelErr error
	p, err := project(cfg, name, a)
	if err != nil {
		modelErr = err
	} else {
		var files []string
		for _, f := range p.Files {
			if r, err := filepath.Rel(p.Dir, f); err == nil {
				f = r
			}
			files = append(files, f)
		}
		src := "found in the folder"
		if len(a.Compose) > 0 {
			src = config.FileName
		}
		row("compose", strings.Join(files, ", "), src)
		if routing != config.RoutingNone && (a.Service == "" || a.Port == 0) {
			model, modelErr = p.Model(ctx)
		}
	}

	if routing == config.RoutingNone {
		row("routing", routing+" (not behind the proxy)", setIn(a.Routing != ""))
	} else {
		service, serviceFrom := a.Service, config.FileName
		port, portFrom := fmt.Sprint(a.Port), config.FileName
		if a.Service == "" {
			service, serviceFrom = detected(model, func(m *compose.Model) (string, error) { return m.Exposed() })
		}
		if a.Port == 0 {
			port, portFrom = detected(model, func(m *compose.Model) (string, error) {
				s := a.Service
				if s == "" {
					var err error
					if s, err = m.Exposed(); err != nil {
						return "", err
					}
				}
				n, err := m.TargetPort(s)
				return fmt.Sprint(n), err
			})
		}
		row("service", service, serviceFrom)
		row("port", port, portFrom)
		routingFrom := setIn(a.Routing != "")
		if a.Routing == "" {
			routingFrom = "default (proxy.routing)"
		}
		row("routing", routing, routingFrom)
		row("url", cfg.URL(name, a)+"/", "")
		if routing == config.RoutingPath {
			row("prefix", cfg.Prefix(name, a), setIn(a.Prefix != ""))
		}
		if routing == config.RoutingHost {
			row("host", cfg.Host(name, a), setIn(a.Host != ""))
		}
	}
	row("data", cfg.DataDir(name), defaults.DataDir+"/<app>")
	if p != nil && len(p.EnvFiles) > 0 {
		row("env files", strings.Join(p.EnvFiles, ", "), "the app's "+defaults.AppEnvFile+", then env_files")
	}
	_ = w.Flush()

	say("  env %s(what the app's compose files can use as ${NAME})%s", dim, reset)
	w = table()
	for _, v := range cfg.EnvVars(name, a) {
		src := envSource(name, v.Source)
		if v.Raw != "" && v.Raw != v.Value {
			src += ": " + v.Raw
		}
		if v.Overrides != "" {
			src += ", over the " + envSource(name, v.Overrides)
		}
		fmt.Fprintf(w, "    %s=%s\t%s\n", v.Name, v.Value, from(src))
	}
	_ = w.Flush()
	if modelErr != nil {
		say("%snote:%s %v", yellow, reset, modelErr)
	}
}

// envSource names an env value's source for the user.
func envSource(app, source string) string {
	switch source {
	case config.SourceAppEnv:
		return "apps." + app + ".env"
	case config.SourceSharedEnv:
		return "shared env"
	}
	return source
}

// detected is a value hl works out from the app's compose model, and its source.
func detected(m *compose.Model, get func(*compose.Model) (string, error)) (value, source string) {
	if m == nil {
		return "?", "detected when the app starts (its compose config can't be read now)"
	}
	v, err := get(m)
	if err != nil {
		return "?", "can't detect: " + err.Error()
	}
	return v, "detected"
}

func showHomelab(cfg *config.Config) error {
	settings, err := config.Settings(cfg.Dir)
	if err != nil {
		return err
	}
	say("%shomelab%s", bold, reset)
	w := table()
	fmt.Fprintf(w, "  folder\t%s\t%s\n", cfg.Dir, from(homeSource(cfg.Dir)))
	fmt.Fprintf(w, "  config\t%s\t\n", cfg.File())
	if cfg.Name != "" {
		fmt.Fprintf(w, "  name\t%s\t%s\n", cfg.Name, from(config.FileName))
	}
	_ = w.Flush()

	say("%sproxy%s", bold, reset)
	w = table()
	for _, s := range settings {
		key, ok := strings.CutPrefix(s.Key, "proxy.")
		if !ok || s.Source == "default" {
			continue // hl show proxy lists every setting
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\n", key, s.Value, from(s.Source))
	}
	fmt.Fprintf(w, "  listen\thttp://%s\t%s\n", net.JoinHostPort(cfg.Proxy.Bind, strconv.Itoa(cfg.Proxy.Port)), from("hl show proxy: state and every setting"))
	_ = w.Flush()

	say("%senv%s %s(every app; an app's own env wins)%s", bold, reset, dim, reset)
	w = table()
	if _, ok := cfg.Env[defaults.TZEnv]; !ok {
		if tz, source := config.HostTimeZone(); tz != "" {
			fmt.Fprintf(w, "  %s=%s\t%s\n", defaults.TZEnv, tz, from(source+", unless an app's env or env files set one"))
		} else {
			fmt.Fprintf(w, "  %s\t%s\n", defaults.TZEnv, from("none: the host's time zone can't be told, so apps keep their own default"))
		}
	}
	for _, k := range sortedKeys(cfg.Env) {
		fmt.Fprintf(w, "  %s=%s\t%s\n", k, cfg.Env[k], from(config.FileName))
	}
	_ = w.Flush()

	say("%sprojects%s %s(where hl init and hl sync look for apps)%s", bold, reset, dim, reset)
	w = table()
	if len(cfg.Projects) == 0 {
		fmt.Fprintf(w, "  folders\t-\t%s\n", from("none: hl sync needs one"))
	}
	for i, p := range cfg.Projects {
		note := config.FileName
		if st, err := os.Stat(cfg.ProjectDirs()[i]); err != nil || !st.IsDir() {
			note = yellow + "not found: " + cfg.ProjectDirs()[i] + reset
		}
		fmt.Fprintf(w, "  folder\t%s\t%s\n", p, from(note))
	}
	ignore := strings.Join(cfg.Ignore, ", ")
	if ignore == "" {
		ignore = "-"
	}
	fmt.Fprintf(w, "  ignore\t%s\t%s\n", ignore, from("always: "+strings.Join(defaults.DefaultIgnore, ", ")+", folders with a "+defaults.IgnoreFile))
	depth, depthFrom := cfg.ScanDepth, config.FileName
	if depth == 0 {
		depth, depthFrom = defaults.ScanDepth, "default"
	}
	fmt.Fprintf(w, "  scan_depth\t%d\t%s\n", depth, from(depthFrom))
	if len(cfg.StripPrefix) > 0 {
		fmt.Fprintf(w, "  strip_prefix\t%s\t%s\n", strings.Join(cfg.StripPrefix, ", "), from(config.FileName))
	}
	_ = w.Flush()

	say("%sapps%s", bold, reset)
	if len(cfg.Apps) == 0 {
		say("  %snone yet - hl add <folder>%s", dim, reset)
		return nil
	}
	w = table()
	for _, n := range cfg.Names() {
		a := cfg.Apps[n]
		u := cfg.URL(n, a)
		if u == "" {
			u = "- (routing none)"
		} else {
			u += "/"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\n", n, u, from(cfg.AppDir(a)))
	}
	_ = w.Flush()
	say("%shl show <app> for one app's details%s", dim, reset)
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
