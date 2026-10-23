package cli

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/scan"
)

// initOptions are hl init's flags; each answers one of its questions.
type initOptions struct {
	projects []string
	routing  string
	bind     string
	port     int
	apps     []string
	yes      bool
}

func initCmd() *cobra.Command {
	var o initOptions
	c := &cobra.Command{
		Use:   "init [folder]",
		Short: "Set up a homelab folder (default: the current one), find your apps, and make it the default",
		Long: `Set up a homelab folder: write a commented hl.yml, find the apps in your
projects folders and add the ones you pick, and make it the default homelab.

On a terminal hl init asks where things go (Enter takes the default). Every
question has a flag, and when stdin isn't a terminal (or with --yes) it asks
nothing: flags and defaults are used, and every app found is added.

On an existing homelab hl.yml is never overwritten; init offers to add apps
that are new in the projects folders instead.`,
		Example: `  hl init                                   # answer a few questions
  hl init ~/homelab --projects ~/code --yes # no questions
  hl init --projects ~/code --apps blog,wiki --routing host --bind 0.0.0.0 --port 8080`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.InOrStdin(), cmd.OutOrStdout(), args, o)
		},
	}
	f := c.Flags()
	f.StringSliceVar(&o.projects, "projects", nil, "folders with your projects; each subfolder with a compose file is an app")
	f.StringVar(&o.routing, "routing", "", "path or host (default: "+config.ProxyDefaults().Routing+")")
	f.StringVar(&o.bind, "bind", "", "address the proxy listens on: "+config.ProxyDefaults().Bind+" = this machine only, "+defaults.BindNetwork+" = your network")
	f.IntVar(&o.port, "port", 0, fmt.Sprintf("host port of the proxy (default: %d)", config.ProxyDefaults().Port))
	f.StringSliceVar(&o.apps, "apps", nil, "apps to add from the projects folders, by name, or "+pickNone+" (default: all found)")
	f.BoolVarP(&o.yes, "yes", "y", false, "ask nothing: use flags and defaults")
	return c
}

// portInUse reports whether bind:port is taken; tests replace it. A port
// hl isn't allowed to open (e.g. 80 as a normal user) counts as free: Docker
// opens it, not hl.
var portInUse = addrInUse

func runInit(in io.Reader, out io.Writer, args []string, o initOptions) error {
	interactive := !o.yes && stdinTerminal(in)
	var p *prompter
	if interactive {
		p = newPrompter(in, out)
	}
	say := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }

	// Where the homelab goes.
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	} else if interactive {
		wd, _ := os.Getwd()
		dir = p.ask("Homelab folder (hl.yml, app data)", wd)
	}
	dir, err := filepath.Abs(config.ExpandHome(dir))
	if err != nil {
		return err
	}
	existing := false
	if _, err := os.Stat(filepath.Join(dir, config.FileName)); err == nil {
		existing = true
	}

	dockerOK := preflight(out)

	if existing {
		say("%s already exists - left as it is; looking for new apps instead", filepath.Join(dir, config.FileName))
		for _, fl := range []struct {
			set  bool
			name string
		}{{o.routing != "", "--routing"}, {o.bind != "", "--bind"}, {o.port != 0, "--port"}} {
			if fl.set {
				say("%snote:%s %s is ignored for an existing homelab - edit %s instead", yellow, reset, fl.name, config.FileName)
			}
		}
		cfg, err := config.Load(dir)
		if err != nil {
			return err
		}
		if len(cfg.Projects) > 0 && o.projects != nil {
			say("%snote:%s --projects is ignored: %s already lists projects folders", yellow, reset, config.FileName)
		}
		if len(cfg.Projects) == 0 {
			projects, err := askProjects(p, out, dir, o.projects)
			if err != nil {
				return err
			}
			if len(projects) > 0 {
				if err := config.SetProjects(dir, projects); err != nil {
					return err
				}
				say("projects folders saved in %s: %s", config.FileName, strings.Join(projects, ", "))
			}
		}
	} else {
		pd := config.ProxyDefaults()
		projects, err := askProjects(p, out, dir, o.projects)
		if err != nil {
			return err
		}
		s := config.InitSettings{Projects: projects, Routing: o.routing, Bind: o.bind, Port: o.port}
		if s.Routing == "" {
			s.Routing = pd.Routing
			if interactive {
				opts := []string{config.RoutingPath, config.RoutingHost}
				labels := []string{
					config.RoutingPath + ": apps at http://" + pd.Domain + "/<app>/ (the app must support a base path)",
					config.RoutingHost + ": apps at http://<app>." + pd.Domain + " (works with any app)",
				}
				s.Routing = opts[p.choose("How should apps be reached?", labels, index(opts, pd.Routing))]
			}
		}
		if s.Routing != config.RoutingPath && s.Routing != config.RoutingHost {
			return fmt.Errorf("--routing: %q is not %s or %s", s.Routing, config.RoutingPath, config.RoutingHost)
		}
		if s.Bind == "" {
			s.Bind = pd.Bind
			if interactive {
				opts := []string{pd.Bind, defaults.BindNetwork}
				labels := []string{"this machine only (" + pd.Bind + ")", "other devices on your network too (" + defaults.BindNetwork + ")"}
				s.Bind = opts[p.choose("Who can open the apps?", labels, 0)]
			}
		}
		if net.ParseIP(s.Bind) == nil {
			return fmt.Errorf("--bind: %q is not an IP address", s.Bind)
		}
		portFlag := s.Port != 0
		if s.Port == 0 {
			s.Port = pd.Port
		}
		if portInUse(s.Bind, s.Port) {
			if interactive {
				say("%sport %d is in use%s on %s", yellow, s.Port, reset, s.Bind)
				for {
					n, err := strconv.Atoi(p.ask("Port for the proxy", strconv.Itoa(defaults.AltPort)))
					if err == nil && n > 0 && n < 65536 {
						s.Port = n
						break
					}
					say("type a port number")
				}
			} else if portFlag {
				say("%swarning:%s port %d is in use on %s - free it before hl up", yellow, reset, s.Port, s.Bind)
			} else {
				say("%swarning:%s port %d is in use on %s - free it, or run init again with --port %d", yellow, reset, s.Port, s.Bind, defaults.AltPort)
			}
		}
		if s.Port <= 0 || s.Port > 65535 {
			return fmt.Errorf("--port: %d is not a port", s.Port)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := config.WriteNew(dir, s); err != nil {
			return err
		}
		say("wrote %s", filepath.Join(dir, config.FileName))
	}

	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		return err
	}
	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); err != nil {
		_ = os.WriteFile(gi, []byte("# App data (databases, uploads) and files hl generates.\ndata/\n.hl/\n"), 0o644)
	}
	if err := config.SetDefaultHome(dir); err != nil {
		return err
	}
	say("default homelab: %s", dir)

	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	added, err := initApps(p, out, cfg, o.apps, dockerOK)
	if err != nil {
		return err
	}
	switch {
	case len(added) > 0: // applyPlan said how to start them
	case len(cfg.Apps) > 0:
		say("next: hl up")
	default:
		say("next: hl add <app folder>")
	}
	return nil
}

// askProjects returns the projects folders as they go into hl.yml: the flag,
// else the answer (suggesting folders that exist), else none.
func askProjects(p *prompter, out io.Writer, homelab string, flag []string) ([]string, error) {
	answer := flag
	for {
		if answer == nil && p != nil {
			s := p.ask("Folders with your projects, comma-separated (empty for none)", strings.Join(candidateProjects(), ", "))
			answer = []string{}
			for _, f := range strings.Split(s, ",") {
				if f = strings.TrimSpace(f); f != "" {
					answer = append(answer, f)
				}
			}
		}
		var stored, missing []string
		for _, f := range answer {
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
		if len(missing) == 0 {
			return stored, nil
		}
		if p == nil || flag != nil {
			return nil, fmt.Errorf("projects folder %s not found", strings.Join(missing, ", "))
		}
		fmt.Fprintf(out, "not found: %s\n", strings.Join(missing, ", "))
		answer = nil
	}
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

// initApps adds apps found in the projects folders: the ones picked on a
// terminal, else the --apps ones, else all.
func initApps(p *prompter, out io.Writer, cfg *config.Config, want []string, dockerOK bool) ([]string, error) {
	if len(cfg.Projects) == 0 {
		return nil, nil
	}
	d, err := scan.Compare(cfg)
	if err != nil {
		return nil, err
	}
	for _, r := range d.MissingRoots {
		fmt.Fprintf(out, "%swarning:%s projects folder %s not found\n", yellow, reset, r)
	}
	if len(d.New) == 0 {
		fmt.Fprintf(out, "no new apps in %s\n", strings.Join(cfg.Projects, ", "))
		return nil, nil
	}
	if !dockerOK {
		fmt.Fprintf(out, "found %s, but hl needs docker to read their compose files - fix the above, then run hl sync\n",
			plural(len(d.New), "app", "apps"))
		return nil, nil
	}
	var adds []scan.Action
	for _, a := range scan.PlanAdds(cfg, d, modelFunc(cfg)) {
		if a.Kind == scan.Add {
			adds = append(adds, a)
		} else {
			fmt.Fprintf(out, "  ! skip %s (%s): %s\n", a.Name, a.Path, a.Reason)
		}
	}
	if len(adds) == 0 {
		return nil, nil
	}
	var chosen []scan.Action
	switch {
	case p != nil && want == nil:
		items := make([]string, len(adds))
		for i, a := range adds {
			items[i] = fmt.Sprintf("%-20s %s  (%s)", a.Name, a.Path, pinned(a.Service, a.Port))
		}
		for _, i := range p.pick(fmt.Sprintf("Found %s. Add which?", plural(len(adds), "app", "apps")), items) {
			chosen = append(chosen, adds[i])
		}
	case want == nil:
		chosen = adds
	case len(want) == 1 && want[0] == pickNone:
	default:
		byName := map[string]scan.Action{}
		for _, a := range adds {
			byName[a.Name] = a
		}
		for _, n := range want {
			a, ok := byName[n]
			if !ok {
				return nil, fmt.Errorf("--apps: %s is not a new app in the projects folders", n)
			}
			chosen = append(chosen, a)
		}
	}
	if err := applyPlan(out, cfg, chosen); err != nil {
		return nil, err
	}
	names := make([]string, len(chosen))
	for i, a := range chosen {
		names[i] = a.Name
	}
	if left := len(adds) - len(chosen); left > 0 {
		fmt.Fprintf(out, "left out %s - add later with hl sync, or skip for good with ignore: in %s or a %s file\n",
			plural(left, "app", "apps"), config.FileName, defaults.IgnoreFile)
	}
	return names, nil
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

func index(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return 0
}
