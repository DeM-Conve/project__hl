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

func setCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <app> key=value...",
		Short: "Change an app's settings in hl.yml (keeps its comments; checks the result first)",
		Long: `Change an app's settings in hl.yml. Keys: ` + strings.Join(config.AppKeys(), ", ") + `
env takes a name: env.TZ=UTC. Lists are comma-separated: compose=compose.yml,compose.prod.yml.
hl.yml keeps its comments and order, and is only written if the result is valid.
Run hl up <app> to apply the change.

  hl set my-app routing=host host=my-app.example.com
  hl set my-app port=8080 env.TZ=UTC`,
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeAppKeys(true),
		RunE: func(cmd *cobra.Command, args []string) error {
			var changes []config.Change
			for _, kv := range args[1:] {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return fmt.Errorf("%q: use key=value, e.g. port=8080", kv)
				}
				if v == "" {
					return fmt.Errorf("%q: no value - to remove a setting use hl unset %s %s", kv, args[0], k)
				}
				changes = append(changes, config.Change{Key: k, Value: v})
			}
			return editApp(args[0], changes)
		},
	}
}

func unsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <app> key...",
		Short: "Remove settings of an app from hl.yml, back to their defaults",
		Long: `Remove settings of an app from hl.yml, so their defaults apply again
(e.g. routing falls back to proxy.routing). Keys: ` + strings.Join(config.AppKeys(), ", ") + `
env.NAME removes one env setting, env removes them all.

  hl unset my-app prefix env.TZ`,
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: completeAppKeys(false),
		RunE: func(cmd *cobra.Command, args []string) error {
			var changes []config.Change
			for _, k := range args[1:] {
				changes = append(changes, config.Change{Key: k, Unset: true})
			}
			return editApp(args[0], changes)
		},
	}
}

func editApp(name string, changes []config.Change) error {
	home, err := homeDir()
	if err != nil {
		return err
	}
	if err := config.EditApp(home, name, changes); err != nil {
		return err
	}
	if err := upgradeConfig(home); err != nil {
		return err
	}
	for _, c := range changes {
		if c.Unset {
			say("unset apps.%s.%s", name, c.Key)
		} else {
			say("set apps.%s.%s = %s", name, c.Key, c.Value)
		}
	}
	say("apply it: hl up %s", name)
	return nil
}

// completeAppKeys completes an app name, then its setting keys (with "=" when setting).
func completeAppKeys(setting bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return completeApps(cmd, args, toComplete)
		}
		var out []string
		for _, k := range config.AppKeys() {
			if setting && !strings.HasSuffix(k, ".") {
				k += "="
			}
			out = append(out, k)
		}
		return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
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
		{"proxy.files", filepath.Join(".hl", defaults.ProxyStateDir, defaults.ProxyComposeFile) + ", " + defaults.TraefikStaticFile},
		{"proxy.restart", defaults.ProxyRestart},
		{"app_name.reserved", strings.Join(config.ReservedAppNames(), ", ")},
		{"sync.ignore_file", defaults.IgnoreFile},
		{"init.projects", strings.Join(defaults.CandidateProjects, ", ") + " (under your home, if they exist)"},
		{"init.alt_port", itoa(defaults.AltPort)},
		{"init.network_bind", defaults.BindNetwork},
		{"edit.draft", defaults.EditDraft},
		{"schema", defaults.SchemaURL},
	}
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
