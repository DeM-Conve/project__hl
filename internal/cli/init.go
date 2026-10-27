package cli

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/scan"
)

// initOptions are hl init's flags; each changes one line of its plan.
type initOptions struct {
	projects []string
	routing  string
	bind     string
	port     int
	apps     []string
	strip    []string
	yes      bool
	noEnv    bool
}

func initCmd() *cobra.Command {
	var o initOptions
	c := &cobra.Command{
		Use:   "init [folder]",
		Short: "Set up a homelab folder (default: the current one), find your apps, and make it the default",
		Long: `Set up a homelab folder: write a short hl.yml, find the apps in your projects
folders, and make it the default homelab.

hl init works out a plan from its flags and defaults - the homelab folder, the
projects folders (default: ` + strings.Join(defaults.CandidateProjects, ", ") + ` in your home, those that exist),
the proxy, every app found with its service, port and env, and the folders it
skips - shows it, and asks once before writing anything. --yes, or stdin that
isn't a terminal, applies it without asking. To change the plan, run it again
with other flags.

On an existing homelab hl.yml is kept; init adds the apps that are new in the
projects folders (and saves --projects or --strip-prefix if hl.yml has none).`,
		Example: `  hl init                                   # plan for the current folder, then ask
  hl init ~/homelab --projects ~/code --yes # no question
  hl init --projects ~/code --strip-prefix project__ --apps blog,wiki --routing host --port 8080`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.InOrStdin(), cmd.OutOrStdout(), args, o)
		},
	}
	f := c.Flags()
	f.StringSliceVar(&o.projects, "projects", nil, "folders with your projects; each subfolder with a compose file is an app (default: "+strings.Join(defaults.CandidateProjects, ", ")+" in your home)")
	f.StringVar(&o.routing, "routing", "", "path or host (default: "+config.ProxyDefaults().Routing+")")
	f.StringVar(&o.bind, "bind", "", "address the proxy listens on: "+config.ProxyDefaults().Bind+" = this machine only, "+defaults.BindNetwork+" = your network")
	f.IntVar(&o.port, "port", 0, fmt.Sprintf("host port of the proxy (default: %d, or %d when that one is in use)", config.ProxyDefaults().Port, defaults.AltPort))
	f.StringSliceVar(&o.apps, "apps", nil, "apps to add from the projects folders, by name, or "+pickNone+" (default: every web app found)")
	f.BoolVarP(&o.yes, "yes", "y", false, "apply the plan without asking")
	f.BoolVar(&o.noEnv, "no-env", false, "don't fill in the apps' env from their compose variables")
	f.StringSliceVar(&o.strip, "strip-prefix", nil, "folder-name prefixes left out of app names (project__blog -> blog)")
	return c
}

// portInUse reports whether bind:port is taken; tests replace it. A port
// hl isn't allowed to open (e.g. 80 as a normal user) counts as free: Docker
// opens it, not hl.
var portInUse = addrInUse

func runInit(in io.Reader, out io.Writer, args []string, o initOptions) error {
	say := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }
	note := func(format string, a ...any) { say("%snote:%s "+format, append([]any{yellow, reset}, a...)...) }

	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	dir, err := filepath.Abs(config.ExpandHome(dir))
	if err != nil {
		return err
	}
	existing := exists(filepath.Join(dir, config.FileName))
	if o.routing != "" && o.routing != config.RoutingPath && o.routing != config.RoutingHost {
		return &usageError{err: fmt.Errorf("--routing: %q is not %s or %s", o.routing, config.RoutingPath, config.RoutingHost), cmd: "hl init"}
	}
	if o.bind != "" && net.ParseIP(o.bind) == nil {
		return &usageError{err: fmt.Errorf("--bind: %q is not an IP address", o.bind), cmd: "hl init"}
	}
	if o.port < 0 || o.port > 65535 {
		return &usageError{err: fmt.Errorf("--port: %d is not a port", o.port), cmd: "hl init"}
	}

	dockerOK := preflight(out)

	// The homelab as it will be: loaded, or made from the flags and defaults.
	var cfg *config.Config
	var s config.InitSettings
	var saveProjects, saveStrip bool
	if existing {
		if cfg, err = config.Load(dir); err != nil {
			return err
		}
		for _, fl := range []struct {
			set  bool
			name string
		}{{o.routing != "", "--routing"}, {o.bind != "", "--bind"}, {o.port != 0, "--port"}} {
			if fl.set {
				note("%s is ignored for an existing homelab - change it with hl set or hl edit", fl.name)
			}
		}
		if len(cfg.Projects) > 0 && o.projects != nil {
			note("--projects is ignored: %s already lists projects folders - change them with hl set projects=...", config.FileName)
		}
		if len(cfg.Projects) == 0 {
			if cfg.Projects, err = initProjects(dir, o.projects); err != nil {
				return err
			}
			saveProjects = len(cfg.Projects) > 0
		}
		switch {
		case o.strip == nil:
		case len(cfg.StripPrefix) > 0:
			note("--strip-prefix is ignored: %s already has strip_prefix - change it with hl set strip_prefix=...", config.FileName)
		default:
			cfg.StripPrefix, saveStrip = o.strip, true
		}
	} else {
		pd := config.ProxyDefaults()
		s = config.InitSettings{Routing: o.routing, Bind: o.bind, Port: o.port, StripPrefix: o.strip}
		if s.Projects, err = initProjects(dir, o.projects); err != nil {
			return err
		}
		if s.Routing == "" {
			s.Routing = pd.Routing
		}
		if s.Bind == "" {
			s.Bind = pd.Bind
		}
		if s.Port == 0 {
			s.Port = pd.Port
			if portInUse(s.Bind, s.Port) && !portInUse(s.Bind, defaults.AltPort) {
				note("port %d is in use on %s - the proxy gets port %d (pass --port to choose another)", s.Port, s.Bind, defaults.AltPort)
				s.Port = defaults.AltPort
			}
		}
		if portInUse(s.Bind, s.Port) {
			say("%swarning:%s port %d is in use on %s - free it before hl up, or pass --port", yellow, reset, s.Port, s.Bind)
		}
		cfg = s.Config(dir)
	}

	adds, err := initPlan(out, cfg, o, dockerOK)
	if err != nil {
		return err
	}
	toAdd := 0
	for _, a := range adds {
		if a.Kind == scan.Add {
			toAdd++
		}
	}

	// Ask once, when there is something to write.
	if !existing || saveProjects || saveStrip || toAdd > 0 {
		if !o.yes && stdinTerminal(in) && !newPrompter(in, out).confirm("Set it up like this?", true) {
			say("nothing written - run hl init again with other flags to change the plan (hl init --help)")
			return nil
		}
	}

	if existing {
		if saveProjects {
			if err := config.SetProjects(dir, cfg.Projects); err != nil {
				return err
			}
			say("projects folders saved in %s: %s", config.FileName, strings.Join(cfg.Projects, ", "))
		}
		if saveStrip {
			if err := config.SetList(dir, "strip_prefix", cfg.StripPrefix); err != nil {
				return err
			}
			say("strip_prefix saved in %s: %s", config.FileName, strings.Join(cfg.StripPrefix, ", "))
		}
	} else {
		if err := os.MkdirAll(dir, defaults.DataDirMode); err != nil {
			return err
		}
		if err := config.WriteNew(dir, s); err != nil {
			return err
		}
		say("wrote %s", filepath.Join(dir, config.FileName))
	}
	if err := os.MkdirAll(filepath.Join(dir, defaults.DataDir), defaults.DataDirMode); err != nil {
		return err
	}
	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); err != nil {
		_ = os.WriteFile(gi, []byte("# App data (databases, uploads) and files hl generates.\n"+defaults.DataDir+"/\n"+defaults.StateDir+"/\n"), defaults.FileMode)
	}
	if err := config.SetDefaultHome(dir); err != nil {
		return err
	}
	say("default homelab: %s", dir)

	if cfg, err = config.Load(dir); err != nil {
		return err
	}
	if err := applyPlan(out, cfg, adds); err != nil {
		return err
	}
	switch {
	case toAdd > 0: // applyPlan said how to start them
	case len(cfg.Apps) > 0:
		say("next: hl up")
	case len(cfg.Projects) == 0:
		say("next: hl set projects+=<folder> (then hl sync), or hl add <app folder>")
	default:
		say("next: hl add <app folder>")
	}
	return nil
}

// initPlan finds the new apps in the projects folders, prints the plan, and returns the apps to
// add (the --apps ones, else every one hl can add).
func initPlan(out io.Writer, cfg *config.Config, o initOptions, dockerOK bool) ([]scan.Action, error) {
	w := func(label, format string, a ...any) {
		fmt.Fprintf(out, "  %-10s %s\n", label, fmt.Sprintf(format, a...))
	}
	fmt.Fprintf(out, "\n%shl init plan%s\n", bold, reset)
	if exists(cfg.File()) {
		w("homelab", "%s (hl.yml kept)", cfg.Dir)
	} else {
		w("homelab", "%s (new hl.yml, data/)", cfg.Dir)
	}
	if len(cfg.Projects) == 0 {
		w("projects", "none - pass --projects <folder>, or later hl set projects+=<folder>")
	} else {
		w("projects", "%s", strings.Join(cfg.Projects, ", "))
	}
	if len(cfg.StripPrefix) > 0 {
		w("names", "without %s", strings.Join(cfg.StripPrefix, ", "))
	}
	reach := "this machine only"
	if ip := net.ParseIP(cfg.Proxy.Bind); ip != nil && !ip.IsLoopback() {
		reach = "your network too"
	}
	w("proxy", "%s:%d (%s), %s routing: apps at %s/", cfg.Proxy.Bind, cfg.Proxy.Port, reach, cfg.Proxy.Routing,
		cfg.URL("<app>", &config.App{}))

	var plan []scan.Action
	if len(cfg.Projects) > 0 {
		d, err := scan.Compare(cfg)
		if err != nil {
			return nil, err
		}
		for _, r := range d.MissingRoots {
			fmt.Fprintf(out, "%swarning:%s projects folder %s not found\n", yellow, reset, r)
		}
		switch {
		case len(d.New) == 0:
			w("apps", "no new apps in %s", strings.Join(cfg.Projects, ", "))
		case !dockerOK:
			w("apps", "found %s, but hl needs docker to read their compose files - fix the above, then run hl sync",
				plural(len(d.New), "folder", "folders"))
		default:
			plan = scan.PlanAdds(cfg, d, modelFunc(cfg), varsFunc(cfg, o.noEnv))
		}
	}

	var chosen []scan.Action
	var left []string
	switch {
	case o.apps == nil:
		chosen = plan
	case len(o.apps) == 1 && o.apps[0] == pickNone:
		for _, a := range plan {
			if a.Kind == scan.Add {
				left = append(left, a.Name)
			}
		}
	default:
		byName := map[string]scan.Action{}
		for _, a := range plan {
			if a.Kind == scan.Add {
				byName[a.Name] = a
			}
		}
		want := map[string]bool{}
		for _, n := range o.apps {
			if _, ok := byName[n]; !ok {
				return nil, withHint(fmt.Errorf("--apps: %s is not a new app in the projects folders", n), "run hl init without --apps to see the plan")
			}
			want[n] = true
		}
		for _, a := range plan {
			if a.Kind == scan.Add && !want[a.Name] {
				left = append(left, a.Name)
			} else {
				chosen = append(chosen, a)
			}
		}
	}
	// Adds first, then what is skipped and why.
	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].Kind == scan.Add && chosen[j].Kind != scan.Add })
	adds := 0
	for _, a := range chosen {
		if a.Kind == scan.Add {
			adds++
		}
	}
	if len(plan) > 0 {
		w("apps", "%s to add", plural(adds, "app", "apps"))
		printPlan(out, cfg, chosen)
	}
	if len(left) > 0 {
		w("left out", "%s (--apps) - add later with hl sync, or skip for good with hl set ignore+=<folder name> or a %s file",
			strings.Join(left, ", "), defaults.IgnoreFile)
	}
	fmt.Fprintln(out)
	return chosen, nil
}

// initProjects returns the projects folders as they go into hl.yml: the flag's (each must
// exist), else the usual ones (defaults.CandidateProjects) that exist in the user's home.
func initProjects(homelab string, flag []string) ([]string, error) {
	if flag == nil {
		return candidateProjects(), nil
	}
	var stored, missing []string
	for _, f := range flag {
		abs, err := filepath.Abs(config.ExpandHome(f))
		if err != nil {
			return nil, err
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			missing = append(missing, f)
			continue
		}
		stored = append(stored, storedPath(homelab, abs))
	}
	if len(missing) > 0 {
		return nil, withHint(fmt.Errorf("projects folder %s not found", strings.Join(missing, ", ")), "pass folders that exist to --projects")
	}
	return stored, nil
}

// candidateProjects are the generic projects folders that exist in the
// user's home, written with ~.
func candidateProjects() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []string
	for _, c := range defaults.CandidateProjects {
		if st, err := os.Stat(filepath.Join(home, c)); err == nil && st.IsDir() {
			out = append(out, "~/"+c)
		}
	}
	return out
}

// storedPath is how a folder is written in hl.yml: relative inside the
// homelab, with ~ inside the user's home (so the homelab can move), else absolute.
func storedPath(homelab, abs string) string {
	if r, err := filepath.Rel(homelab, abs); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	if home, err := os.UserHomeDir(); err == nil {
		if r, err := filepath.Rel(home, abs); err == nil && !strings.HasPrefix(r, "..") {
			if r == "." {
				return "~"
			}
			return "~/" + r
		}
	}
	return abs
}

// preflight runs the docker checks of hl doctor and says what to fix. It
// reports whether docker can be used to read the apps' compose files.
func preflight(out io.Writer) bool {
	ok := true
	check := func(good bool, what, fix string) {
		if good {
			fmt.Fprintf(out, "%s✓%s %s\n", green, reset, what)
			return
		}
		ok = false
		fmt.Fprintf(out, "%s✗%s %s - %s\n", red, reset, what, fix)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		check(false, "docker is installed", "install Docker Engine (https://docs.docker.com/engine/install/)")
		return false
	}
	check(true, "docker is installed", "")
	check(exec.Command("docker", "info").Run() == nil, "the docker daemon is reachable", "start docker, or add yourself to the docker group")
	v, err := exec.Command("docker", "compose", "version", "--short").Output()
	ver := strings.TrimSpace(string(v))
	check(err == nil && composeAtLeast(ver, 2, 24), "docker compose v2.24+ ("+ver+")", "update the docker compose plugin")
	return ok
}
