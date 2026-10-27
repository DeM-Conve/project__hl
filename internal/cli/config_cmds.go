package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// upgradeConfig brings hl.yml to the current format before hl edits it, and says so: an older
// file keeps working as it is, but is never changed silently.
func upgradeConfig(dir string) error {
	from, err := config.Upgrade(dir)
	if err != nil {
		return err
	}
	if from != defaults.ConfigVersion {
		fmt.Fprintf(stderr, "%snote:%s upgraded %s to version %d (added `version: %d` at the top; nothing else changed)\n",
			yellow, reset, config.FileName, defaults.ConfigVersion, defaults.ConfigVersion)
	}
	return nil
}

// homeDir is the homelab folder without loading hl.yml, for commands that must work on a
// broken hl.yml (checking or fixing it).
func homeDir() (string, error) { return config.Home(homeFlag) }

func configCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check hl.yml: every problem with its line number and how to fix it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := homeDir()
			if err != nil {
				return err
			}
			path := filepath.Join(home, config.FileName)
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if ps := config.Check(b); len(ps) > 0 {
				for _, p := range ps {
					say("%s", p.In(path))
				}
				return fmt.Errorf("%s in %s", plural(len(ps), "problem", "problems"), path)
			}
			say("%s✓%s %s is fine", green, reset, path)
			if config.FileVersion(b) == 0 {
				say("%snote:%s it has no `version:` line (the format before versions) - hl adds `version: %d` the next time it changes the file",
					yellow, reset, defaults.ConfigVersion)
			}
			return nil
		},
	}
}

const setScopesHelp = `What to change is the first argument:
  <app>    that app's settings. Keys: ` + "%s" + `
  proxy    the proxy (proxy: in hl.yml). Keys: ` + "%s" + `
  (none)   the settings every app gets (env: in hl.yml; an app's own env wins). Keys: ` + "%s" + `
env takes a name: env.TZ=UTC.`

func scopesHelp() string {
	return fmt.Sprintf(setScopesHelp, strings.Join(config.AppKeys(), ", "), strings.Join(config.ProxyKeys(), ", "),
		strings.Join(config.HomelabKeys(), ", "))
}

func setCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set [<app>|proxy] key=value...",
		Short: "Change settings of an app, the proxy or every app (keeps hl.yml's comments; checks the result first)",
		Long: `Change settings in hl.yml without opening it. ` + scopesHelp() + `
Lists are comma-separated (compose=compose.yml,compose.prod.yml); switches take true or false.
hl.yml keeps its comments and order, and is only written if the result is valid; hl says
which command applies the change.

  hl set my-app routing=host host=my-app.example.com
  hl set my-app port=8080 env.TZ=UTC
  hl set proxy port=8080 bind=0.0.0.0
  hl set env.TZ=Europe/Berlin`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeSetKeys(true),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, kvs := args[0], args[1:]
			if strings.Contains(scope, "=") {
				// App names have no "=": no app given, the shared settings.
				scope, kvs = config.HomelabScope, args
			} else if len(kvs) == 0 {
				return &usageError{err: fmt.Errorf("nothing to set for %q - give key=value, e.g. hl set %s port=8080", scope, scope), cmd: cmd.CommandPath()}
			}
			var changes []config.Change
			for _, kv := range kvs {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return fmt.Errorf("%q: use key=value, e.g. port=8080", kv)
				}
				if v == "" {
					return fmt.Errorf("%q: no value - to remove a setting use %s", kv, unsetCommand(scope, k))
				}
				changes = append(changes, config.Change{Key: k, Value: v})
			}
			return editScope(scope, changes)
		},
	}
}

func unsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset [<app>|proxy] key...",
		Short: "Remove settings of an app, the proxy or every app, back to their defaults",
		Long: `Remove settings from hl.yml, so their defaults apply again (e.g. an app's
routing falls back to proxy.routing). ` + scopesHelp() + `
env.NAME removes one env setting, env removes them all.

  hl unset my-app prefix env.TZ
  hl unset proxy port
  hl unset env.TZ`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeSetKeys(false),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := homeDir()
			if err != nil {
				return err
			}
			scope, keys := args[0], args[1:]
			// env and env.NAME without an app are the shared env - unless an app has that name.
			if config.IsHomelabKey(scope) && !config.HasApp(home, scope) {
				scope, keys = config.HomelabScope, args
			} else if len(keys) == 0 {
				return &usageError{err: fmt.Errorf("nothing to unset for %q - name the keys, e.g. hl unset %s port", scope, scope), cmd: cmd.CommandPath()}
			}
			var changes []config.Change
			for _, k := range keys {
				changes = append(changes, config.Change{Key: k, Unset: true})
			}
			return editScope(scope, changes)
		},
	}
}

// unsetCommand is the hl unset command line that removes key from scope.
func unsetCommand(scope, key string) string {
	if scope == config.HomelabScope {
		return "hl unset " + key
	}
	return "hl unset " + scope + " " + key
}

// editScope applies changes to scope (an app, the proxy or the shared settings) and says what
// changed and how to apply it.
func editScope(scope string, changes []config.Change) error {
	home, err := homeDir()
	if err != nil {
		return err
	}
	if err := config.Edit(home, scope, changes); err != nil {
		return withHint(err, "`hl show` lists the apps and settings; `hl set --help` lists the keys")
	}
	if err := upgradeConfig(home); err != nil {
		return err
	}
	prefix := ""
	switch scope {
	case config.HomelabScope:
	case config.ProxyScope:
		prefix = "proxy."
	default:
		prefix = "apps." + scope + "."
	}
	for _, c := range changes {
		if c.Unset {
			say("unset %s%s", prefix, c.Key)
		} else {
			say("set %s%s = %s", prefix, c.Key, c.Value)
		}
	}
	say("apply it: %s", applyCommand(scope, changes))
	return nil
}

// applyCommand is what makes changes to scope take effect.
func applyCommand(scope string, changes []config.Change) string {
	switch scope {
	case config.HomelabScope:
		return "hl up (compose recreates the apps whose settings changed)"
	case config.ProxyScope:
		apps := false
		for _, c := range changes {
			switch c.Key {
			case "network", "subnet", "project":
				// Every app joins the proxy's network: both move together.
				return "hl down, then hl up (the proxy and every app move to the new network or project)"
			case "domain", "routing":
				apps = true
			}
		}
		if apps {
			return "hl proxy up, then hl up (the apps' routes change too)"
		}
		return "hl proxy up"
	}
	return "hl up " + scope
}

// completeSetKeys completes the first argument (an app, proxy, or a shared key), then that
// scope's setting keys (with "=" when setting).
func completeSetKeys(setting bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	keys := func(list []string) []string {
		out := make([]string, 0, len(list))
		for _, k := range list {
			if setting && !strings.HasSuffix(k, ".") {
				k += "="
			}
			out = append(out, k)
		}
		return out
	}
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		const dir = cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
		if len(args) == 0 {
			apps, _ := completeApps(cmd, args, toComplete)
			// A trailing space after a scope, none after a shared key being typed.
			if strings.HasPrefix(toComplete, "env.") {
				return keys(config.HomelabKeys()), dir
			}
			return append(append(apps, config.ProxyScope), keys(config.HomelabKeys())...), cobra.ShellCompDirectiveNoFileComp
		}
		switch {
		case strings.Contains(args[0], "=") || config.IsHomelabKey(args[0]) && !setting:
			return keys(config.HomelabKeys()), dir
		case args[0] == config.ProxyScope:
			return keys(config.ProxyKeys()), dir
		}
		return keys(config.AppKeys()), dir
	}
}

func editCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open hl.yml in your editor; it is saved only once it checks out",
		Long: `Open hl.yml in $VISUAL or $EDITOR (default: ` + defaults.DefaultEditor + `). You edit a copy (` + defaults.EditDraft + `);
when the editor closes hl checks it and replaces hl.yml only if it has no problems.
Otherwise it lists them and offers to open the editor again, so hl.yml is never left broken.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := homeDir()
			if err != nil {
				return err
			}
			path := filepath.Join(home, config.FileName)
			orig, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			draft := filepath.Join(home, defaults.EditDraft)
			if err := os.MkdirAll(filepath.Dir(draft), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(draft, orig, 0o644); err != nil {
				return err
			}
			for {
				if err := runEditor(draft); err != nil {
					return fmt.Errorf("%w - hl.yml not changed; your edit is in %s", err, draft)
				}
				b, err := os.ReadFile(draft)
				if err != nil {
					return err
				}
				if bytes.Equal(b, orig) {
					_ = os.Remove(draft)
					say("no changes")
					return nil
				}
				ps := config.Check(b)
				if len(ps) == 0 {
					if err := config.Replace(home, b); err != nil {
						return err
					}
					_ = os.Remove(draft)
					say("%s✓%s saved %s", green, reset, path)
					return nil
				}
				for _, p := range ps {
					say("%s", p.In(draft))
				}
				keep := fmt.Errorf("hl.yml not changed; your edit is in %s (fix it there, or run hl edit again to start over)", draft)
				if !stdinTerminal(cmd.InOrStdin()) ||
					!newPrompter(cmd.InOrStdin(), stdout).confirm(plural(len(ps), "problem", "problems")+" - open the editor again?", true) {
					return keep
				}
			}
		},
	}
}

// runEditor opens file in $VISUAL, $EDITOR or the default editor, attached to the terminal.
// The variable may hold arguments too (e.g. "code --wait").
func runEditor(file string) error {
	editor, _ := editorCommand()
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", file)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q: %v (set $EDITOR to the one you use)", editor, err)
	}
	return nil
}

// editorCommand is the editor hl edit runs, and where that came from.
func editorCommand() (cmd, source string) {
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if e := os.Getenv(v); e != "" {
			return e, "env ($" + v + ")"
		}
	}
	return defaults.DefaultEditor, "default"
}

// homeSource says where the homelab folder home came from, in config.Home's order.
func homeSource(home string) string {
	switch {
	case homeFlag != "":
		return "flag (--home)"
	case os.Getenv("HL_HOME") != "":
		return "env ($HL_HOME)"
	}
	if wd, err := os.Getwd(); err == nil {
		if r, err := filepath.Rel(home, wd); err == nil && !strings.HasPrefix(r, "..") {
			return "nearest " + config.FileName
		}
	}
	return "default (hl init)"
}

// builtinDefaults are the named values hl uses that hl.yml doesn't set (yet), as name, value.
// The one place `hl config defaults` reads them from; point it at a shared defaults package
// once there is one.
func builtinDefaults() [][2]string {
	itoa := strconv.Itoa
	return [][2]string{
		{"traefik.tested_version", defaults.TestedTraefikVersion},
		{"traefik.entrypoint", defaults.EntryPointWeb},
		{"traefik.container_port", itoa(defaults.ProxyContainerPort)},
		{"traefik.ping", defaults.EntryPointPing + ":" + itoa(defaults.PingPort)},
		{"traefik.healthcheck", fmt.Sprintf("every %s, timeout %s, %d retries, start %s",
			defaults.ProxyHealthInterval, defaults.ProxyHealthTimeout, defaults.ProxyHealthRetries, defaults.ProxyHealthStartPeriod)},
		{"traefik.access_log_format", defaults.AccessLogFormat},
		{"traefik.log_level", defaults.DefaultProxyLogLevel},
		{"traefik.dashboard_host", defaults.DashboardHostPrefix + "<domain>"},
		{"proxy.files", filepath.Join(defaults.StateDir, defaults.ProxyStateDir, defaults.ProxyComposeFile) + ", " + defaults.TraefikStaticFile},
		{"proxy.restart", defaults.ProxyRestart},
		{"app_name.reserved", strings.Join(config.ReservedAppNames(), ", ")},
		{"sync.ignore_file", defaults.IgnoreFile},
		{"init.projects", strings.Join(defaults.CandidateProjects, ", ") + " (under your home, if they exist)"},
		{"init.alt_port", itoa(defaults.AltPort)},
		{"init.network_bind", defaults.BindNetwork},
		{"edit.draft", defaults.EditDraft},
		{"schema", defaults.SchemaURL},
		{"docker.project_prefix", defaults.Prefix + "<app>, " + defaults.Prefix + "<name>-<app> with name:"},
		{"docker.compose_min", fmt.Sprintf("v%d.%d", defaults.ComposeMinMajor, defaults.ComposeMinMinor)},
		{"files.data", defaults.DataDir + "/<app>/<volume>"},
		{"files.generated", filepath.Join(defaults.StateDir, "<app>", defaults.OverrideFile)},
		{"logs.tail", itoa(defaults.LogTail) + " (hl logs --tail)"},
		{"up.wait_timeout", defaults.WaitTimeout.String() + " (--wait-timeout)"},
		{"up.wait_fail_log_lines", itoa(defaults.FailLogTail)},
		{"add.preferred_services", strings.Join(defaults.ExposedServices, ", ")},
		{"open.browser", defaults.OpenerLinux + " (Linux), " + defaults.OpenerMac + " (macOS)"},
	}
}

// tzSetting is the TZ every app gets: the shared env's, else the host's time zone. An app's
// own env or env files may still set another (hl show <app> says).
func tzSetting(home string) config.Setting {
	s := config.Setting{Key: "env." + defaults.TZEnv, Default: "the host's time zone"}
	if cfg, err := config.Load(home); err == nil {
		if tz, ok := cfg.Env[defaults.TZEnv]; ok {
			s.Value, s.Source = tz, config.FileName
			return s
		}
	}
	if tz, source := config.HostTimeZone(); tz != "" {
		s.Value, s.Source = tz, source
		return s
	}
	s.Value, s.Source = "-", "none: the host's time zone can't be told, so apps keep their own default"
	return s
}

func configDefaultsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "defaults",
		Short: "Every setting that has a default: the default, the value in effect, and where it comes from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := homeDir()
			if err != nil {
				return err
			}
			settings, err := config.Settings(home)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(filepath.Join(home, config.FileName))
			if err != nil {
				return err
			}
			ver, verFrom := strconv.Itoa(config.FileVersion(b)), config.FileName
			if ver == "0" {
				ver, verFrom = "-", "none (the format before versions)"
			}
			// Settings whose default lives outside config (0 / empty in hl.yml means "use it").
			for i, st := range settings {
				switch st.Key {
				case "scan_depth":
					settings[i].Default = strconv.Itoa(defaults.ScanDepth)
					if st.Source == "default" {
						settings[i].Value = settings[i].Default
					}
				case "ignore":
					settings[i].Default = "-  (always: " + strings.Join(defaults.DefaultIgnore, ", ") + ")"
				}
			}
			editor, editorFrom := editorCommand()
			settings = append(settings, tzSetting(home))
			settings = append(settings,
				config.Setting{Key: "home", Default: "-", Value: home, Source: homeSource(home)},
				config.Setting{Key: "version", Default: strconv.Itoa(defaults.ConfigVersion), Value: ver, Source: verFrom},
				config.Setting{Key: "editor", Default: defaults.DefaultEditor, Value: editor, Source: editorFrom},
			)
			for _, b := range builtinDefaults() {
				settings = append(settings, config.Setting{Key: b[0], Default: b[1], Value: b[1], Source: "built in"})
			}
			w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SETTING\tDEFAULT\tVALUE\tFROM")
			for _, s := range settings {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Key, s.Default, s.Value, s.Source)
			}
			return w.Flush()
		},
	}
}
