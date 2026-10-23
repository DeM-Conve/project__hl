package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/config"
)

// upgradeConfig brings hl.yml to the current format before hl edits it, and says so: an older
// file keeps working as it is, but is never changed silently.
func upgradeConfig(dir string) error {
	from, err := config.Upgrade(dir)
	if err != nil {
		return err
	}
	if from != config.Version {
		fmt.Fprintf(os.Stderr, "%snote:%s upgraded %s to version %d (added `version: %d` at the top; nothing else changed)\n",
			yellow, reset, config.FileName, config.Version, config.Version)
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
					yellow, reset, config.Version)
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
		Long: `Open hl.yml in $VISUAL or $EDITOR (default: ` + config.DefaultEditor + `). You edit a copy (` + config.EditDraft + `);
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
			draft := filepath.Join(home, config.EditDraft)
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
				if !stdinIsTerminal() {
					return keep
				}
				again, err := confirm(plural(len(ps), "problem", "problems")+" - open the editor again?", true)
				if err != nil || !again {
					return keep
				}
			}
		},
	}
}

// runEditor opens file in $VISUAL, $EDITOR or the default editor, attached to the terminal.
// The variable may hold arguments too (e.g. "code --wait").
func runEditor(file string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = config.DefaultEditor
	}
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", file)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor %q: %v (set $EDITOR to the one you use)", editor, err)
	}
	return nil
}
