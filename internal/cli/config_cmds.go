package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

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
// broken hl.yml (fixing it).
func homeDir() (string, error) { return config.Home(homeFlag) }

const setScopesHelp = `What to change is the first argument:
  <app>    that app's settings. Keys: ` + "%s" + `
  proxy    the proxy (proxy: in hl.yml). Keys: ` + "%s" + `
  (none)   the homelab: the env every app gets (an app's own env wins), name, and where hl init
           and hl sync look for apps. Keys: ` + "%s" + `
env takes a name: env.TZ=UTC.`

// setAppKeys are the keys `hl set <app>` takes: the app's settings, and name (a rename).
func setAppKeys() []string {
	keys := append(config.AppKeys(), "name")
	sort.Strings(keys)
	return keys
}

func scopesHelp() string {
	return fmt.Sprintf(setScopesHelp, strings.Join(setAppKeys(), ", "), strings.Join(config.ProxyKeys(), ", "),
		strings.Join(config.HomelabKeys(), ", "))
}

func setCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set [<app>|proxy] key=value...",
		Short: "Change settings of an app, the proxy or the homelab (keeps hl.yml's comments; checks the result first)",
		Long: `Change settings in hl.yml without opening it. ` + scopesHelp() + `
Lists are comma-separated and replaced as a whole (projects=~/code,~/src); key+=value adds
entries and key-=value removes them (ignore+=old, compose-=compose.dev.yml). Switches take true
or false. A projects folder must exist; it is written like hl init does (~/..., or relative to
the homelab). name renames every Docker project and network, so hl stops the change while any
of this homelab's apps or its proxy runs. hl.yml keeps its comments and order, and is only
written if the result is valid; hl says which command applies the change.

  hl set my-app routing=host host=my-app.example.com
  hl set my-app port=8080 env.TZ=UTC
  hl set proxy port=8080 bind=0.0.0.0
  hl set env.TZ=Europe/Berlin
  hl set projects+=~/src ignore+='scratch-*' scan_depth=2`,
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
			changes, err := parseSettings(scope, kvs)
			if err != nil {
				return err
			}
			return editScope(cmd.Context(), scope, changes)
		},
	}
}

func unsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset [<app>|proxy] key...",
		Short: "Remove settings of an app, the proxy or the homelab, back to their defaults",
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
			return editScope(cmd.Context(), scope, changes)
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

// parseSettings turns key=value, key+=value and key-=value arguments into changes; unset names
// the command that removes a setting given no value.
func parseSettings(scope string, kvs []string) ([]config.Change, error) {
	var changes []config.Change
	for _, kv := range kvs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("%q: use key=value, e.g. port=8080", kv)
		}
		op := ""
		if strings.HasSuffix(k, config.OpAdd) || strings.HasSuffix(k, config.OpRemove) {
			k, op = k[:len(k)-1], k[len(k)-1:]
		}
		if v == "" {
			return nil, fmt.Errorf("%q: no value - to remove a setting use %s", kv, unsetCommand(scope, k))
		}
		changes = append(changes, config.Change{Key: k, Value: v, Op: op})
	}
	return changes, nil
}

// homelabChanges prepares changes to the homelab's own settings: projects folders are checked
// and written like hl init writes them, and name may change only while nothing of this
// homelab runs, since it renames every Docker project and network.
func homelabChanges(ctx context.Context, home string, changes []config.Change) error {
	cfg, _ := config.Load(home) // for the projects already listed; nil when hl.yml doesn't load
	for i, c := range changes {
		switch c.Key {
		case "projects":
			if c.Unset {
				continue
			}
			var out []string
			for _, item := range strings.Split(c.Value, ",") {
				if item = strings.TrimSpace(item); item == "" {
					continue
				}
				abs, err := filepath.Abs(config.ExpandHome(item))
				if err != nil {
					return err
				}
				if c.Op == config.OpRemove {
					// The entry as hl.yml has it, in whichever spelling it was given.
					if cfg != nil {
						for _, e := range cfg.Projects {
							if e == item || cfg.Abs(e) == abs {
								item = e
							}
						}
					}
					out = append(out, item)
					continue
				}
				if st, err := os.Stat(abs); err != nil || !st.IsDir() {
					return withHint(fmt.Errorf("projects: folder %s not found", abs), "pass a folder that exists")
				}
				out = append(out, storedPath(home, abs))
			}
			changes[i].Value = strings.Join(out, ",")
		case "name":
			if err := nothingRuns(ctx, cfg); err != nil {
				return err
			}
		}
	}
	return nil
}

// nothingRuns is an error when one of this homelab's apps or its proxy has containers.
func nothingRuns(ctx context.Context, cfg *config.Config) error {
	if cfg == nil {
		return nil // hl.yml doesn't load: nothing hl could have started from it
	}
	inv, err := containers(ctx)
	if err != nil {
		return withHint(fmt.Errorf("can't tell whether this homelab's apps run, so name is left as it is: %w", err),
			"name renames every Docker project; start Docker, `hl down`, then set it")
	}
	var running []string
	if len(inv.of(cfg.ProxyProject())) > 0 && inv.owner(cfg, cfg.ProxyProject()) == "" {
		running = append(running, proxyArg)
	}
	for _, n := range cfg.Names() {
		if pn := cfg.ProjectName(n); len(inv.of(pn)) > 0 && inv.owner(cfg, pn) == "" {
			running = append(running, n)
		}
	}
	if len(running) > 0 {
		return withHint(fmt.Errorf("name renames every Docker project and network, and %s still run under the old names", strings.Join(running, ", ")),
			"stop them first: hl down, then set name, then hl up")
	}
	return nil
}

// editScope applies changes to scope (an app, the proxy or the homelab) and says what
// changed and how to apply it.
func editScope(ctx context.Context, scope string, changes []config.Change) error {
	home, err := homeDir()
	if err != nil {
		return err
	}
	if scope == config.HomelabScope {
		if err := homelabChanges(ctx, home, changes); err != nil {
			return err
		}
	}
	isApp := scope != config.HomelabScope && scope != config.ProxyScope
	if isApp {
		for _, c := range changes {
			if c.Key != "name" || c.Unset {
				continue
			}
			if len(changes) > 1 || c.Op != "" {
				return &usageError{err: errors.New("name= renames the app: give it on its own, e.g. hl set " + scope + " name=<new name>"), cmd: "hl set"}
			}
			if err := renameApp(ctx, home, scope, c.Value); err != nil {
				return err
			}
			if err := upgradeConfig(home); err != nil {
				return err
			}
			say("apply it: hl up %s", c.Value)
			return nil
		}
	}
	edits := changes
	if isApp {
		// compose+= on an app that names no compose file starts from the one compose picks.
		if cfg, err := config.Load(home); err == nil {
			if a, ok := cfg.Apps[scope]; ok {
				if edits, err = seedCompose(cfg.AppDir(a), a.Compose, changes); err != nil {
					return err
				}
			}
		}
	}
	if err := config.Edit(home, scope, edits); err != nil {
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
			say("set %s%s %s= %s", prefix, c.Key, c.Op, c.Value)
		}
	}
	say("apply it: %s", applyCommand(scope, changes))
	return nil
}

// renameApp is `hl set <old> name=<new>`: the app keeps its settings (and hl.yml its comments),
// its data folder and generated files move along. Refused while the app has containers, since
// they belong to the Docker project of the old name.
func renameApp(ctx context.Context, home, old, new string) error {
	cfg, err := config.Load(home)
	if err != nil {
		return err
	}
	if _, err := cfg.App(old); err != nil {
		return &usageError{err: err, cmd: "hl set"}
	}
	if new == old {
		say("%s is already called %s", old, new)
		return nil
	}
	if err := config.ValidName(new); err != nil {
		return withHint(err, "pick another: hl set %s name=<name>", old)
	}
	if _, ok := cfg.Apps[new]; ok {
		return withHint(fmt.Errorf("app %q exists already", new), "pick another name, or `hl remove %s` first", new)
	}
	inv, err := containers(ctx)
	if err != nil {
		return withHint(fmt.Errorf("can't tell whether %s runs, so it keeps its name: %w", old, err),
			"start Docker; renaming needs the app stopped (`hl down %s`)", old)
	}
	if len(inv.of(cfg.ProjectName(old))) > 0 {
		return withHint(fmt.Errorf("%s has containers (Docker project %s); renaming it would leave them behind", old, cfg.ProjectName(old)),
			"stop it first: hl down %s, then hl set %s name=%s, then hl up %s", old, old, new, new)
	}
	if pn := cfg.ProjectName(new); len(inv.of(pn)) > 0 {
		return withHint(fmt.Errorf("%s would use Docker project %s, which exists already", new, pn),
			"pick another name, or remove that project (`docker compose -p %s down`)", pn)
	}
	oldData, newData := cfg.DataDir(old), cfg.DataDir(new)
	if exists(newData) {
		return withHint(fmt.Errorf("%s exists already, and %s's data would move there", newData, old),
			"move or remove that folder, or pick another name")
	}
	// hl.yml first: it is checked (a clash with another app's prefix, host or project) and
	// nothing has moved if it is refused.
	if err := config.RenameApp(home, old, new); err != nil {
		return withHint(err, "pick another name: hl set %s name=<name>", old)
	}
	say("renamed apps.%s to apps.%s", old, new)
	if exists(oldData) {
		if err := os.Rename(oldData, newData); err != nil {
			if rerr := config.RenameApp(home, new, old); rerr != nil {
				return fmt.Errorf("moving %s to %s: %w (and hl.yml couldn't be put back: %v)", oldData, newData, err, rerr)
			}
			return fmt.Errorf("moving %s to %s: %w - hl.yml is unchanged", oldData, newData, err)
		}
		say("moved its data to %s", newData)
	}
	// Generated files and the build record: disposable, but moving them saves a rebuild.
	_ = os.RemoveAll(cfg.StateDir(new))
	if exists(cfg.StateDir(old)) {
		if err := os.Rename(cfg.StateDir(old), cfg.StateDir(new)); err != nil {
			warn("%s stays where it is: %v", cfg.StateDir(old), err)
		}
	}
	return nil
}

// applyCommand is what makes changes to scope take effect.
func applyCommand(scope string, changes []config.Change) string {
	switch scope {
	case config.HomelabScope:
		scanned, env := false, false
		for _, c := range changes {
			switch k, _, _ := strings.Cut(c.Key, "."); k {
			case "name":
				return "hl up (the apps and the proxy start under the new names)"
			case "env":
				env = true
			default:
				scanned = true
			}
		}
		switch {
		case scanned && env:
			return "hl sync for the projects settings, hl up for the env"
		case scanned:
			return "hl sync (adds and removes apps to match)"
		}
		return "hl up (compose recreates the apps whose settings changed)"
	case config.ProxyScope:
		apps := false
		for _, c := range changes {
			switch c.Key {
			case "network", "subnet", "project":
				// Every app joins the proxy's network: both move together.
				return "hl down, then hl up (the proxy and every app move to the new network or project)"
			case "domain", "routing", "port":
				// The apps' routes or their HL_URL (which has the port unless it is 80) change too.
				apps = true
			}
		}
		if apps {
			return "hl up proxy, then hl up (the apps' routes or URLs change too)"
		}
		return "hl up proxy"
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
		return keys(setAppKeys()), dir
	}
}

func editCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit",
		Short: "Open hl.yml in your editor (a fallback: hl set/unset change every setting); saved only once it checks out",
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
