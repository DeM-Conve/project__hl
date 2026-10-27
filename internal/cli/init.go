package cli

import (
	"context"
	"errors"
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
	strip    []string
	yes      bool
}

func initCmd() *cobra.Command {
	var o initOptions
	c := &cobra.Command{
		Use:   "init [folder]",
		Short: "Set up a homelab folder (default: ~/" + defaults.HomelabDir + "), find your apps, and make it the default",
		Long: `Set up a homelab folder: write a short hl.yml, find the apps in your projects
folders, and make it the default homelab.

Without a folder, init uses the homelab hl already uses (--home, $` + defaults.HomeEnv + `, an
hl.yml at or above the current folder, or the default), else ~/` + defaults.HomelabDir + `.
It never turns the current folder into a homelab unless you name it (hl init .).

hl init works out a plan from its flags and defaults - the homelab folder, the
projects folders (default: ` + strings.Join(defaults.CandidateProjects, ", ") + ` in your home, those that exist),
the proxy, every app found with its service, port and env, and the folders it
skips - shows it, and asks once before writing anything. --yes applies it
without asking; without a terminal (a script, a pipe) --yes is required, as
for hl sync. To change the plan, run it again with other flags.

init picks the proxy itself: ` + config.ProxyDefaults().Routing + ` routing on ` + config.ProxyDefaults().Bind + ` (this machine only), port
` + fmt.Sprint(config.ProxyDefaults().Port) + `, or the first free of ` + joinInts(defaults.AltPorts) + ` when that one is in use or is
another homelab's proxy port (the default homelab's, or one Docker has containers of).
Change it afterwards with hl set proxy (bind=0.0.0.0, port=8080, routing=host).
To leave out an app init adds, hl remove <app> afterwards (hl sync won't add it
back).

On an existing homelab hl.yml is kept; init adds the apps that are new in its
projects folders, and saves --projects or --strip-prefix if hl.yml has none.
Nothing else is written into it: the usual projects folders are only named in
the plan (hl init --projects ~/code saves one).`,
		Example: `  hl init                                   # plan for ~/homelab, then ask
  hl init ~/homelab --projects ~/code --yes # no question
  hl init --projects ~/code --strip-prefix project__
  hl set proxy port=8080 routing=host       # then change the proxy`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.InOrStdin(), cmd.OutOrStdout(), args, o)
		},
	}
	f := c.Flags()
	f.StringSliceVar(&o.projects, "projects", nil, "folders with your projects; each subfolder with a compose file is an app (default: "+strings.Join(defaults.CandidateProjects, ", ")+" in your home)")
	f.BoolVarP(&o.yes, "yes", "y", false, "apply the plan without asking")
	f.StringSliceVar(&o.strip, "strip-prefix", nil, "folder-name prefixes left out of app names, project__blog -> blog (default: a prefix most folders found share; "+pickNone+" for whole names)")
	return c
}

// portInUse reports whether bind:port is taken; tests replace it. A port
// hl isn't allowed to open (e.g. 80 as a normal user) counts as free: Docker
// opens it, not hl.
var portInUse = addrInUse

func runInit(in io.Reader, out io.Writer, args []string, o initOptions) error {
	say := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }
	note := func(format string, a ...any) { say("%snote:%s "+format, append([]any{yellow, reset}, a...)...) }

	dir, err := initDir(args)
	if err != nil {
		return err
	}
	existing := exists(filepath.Join(dir, config.FileName))

	dockerOK := preflight(out)

	// The homelab as it will be: loaded, or made from the flags and defaults.
	var cfg *config.Config
	var s config.InitSettings
	var saveProjects, saveStrip bool
	var why initWhy
	noStrip := len(o.strip) == 1 && o.strip[0] == pickNone
	if noStrip {
		o.strip = []string{}
	}
	if existing {
		if cfg, err = config.Load(dir); err != nil {
			return err
		}
		if len(cfg.Projects) > 0 && o.projects != nil {
			note("--projects is ignored: %s already lists projects folders - change them with hl set projects+=<folder>", config.FileName)
		}
		// An existing hl.yml changes only as the flags say: the usual projects folders are
		// not written into it unasked (the plan names them instead).
		if len(cfg.Projects) == 0 && o.projects != nil {
			if cfg.Projects, err = initProjects(dir, o.projects); err != nil {
				return err
			}
			saveProjects = len(cfg.Projects) > 0
		}
		switch {
		case o.strip == nil, noStrip:
		case len(cfg.StripPrefix) > 0:
			note("--strip-prefix is ignored: %s already has strip_prefix - change it with hl set strip_prefix=<prefix>", config.FileName)
		default:
			cfg.StripPrefix, saveStrip = o.strip, true
		}
		why.saveProjects, why.saveStrip = saveProjects, saveStrip
	} else {
		pd := config.ProxyDefaults()
		s = config.InitSettings{Routing: pd.Routing, Bind: pd.Bind, Port: pd.Port, StripPrefix: o.strip}
		if s.Projects, err = initProjects(dir, o.projects); err != nil {
			return err
		}
		if o.strip == nil && len(s.Projects) > 0 {
			// Folders named <prefix><app> (project__blog, project__wiki): names without it.
			if d, err := scan.Compare(s.Config(dir)); err == nil {
				var names []string
				for _, f := range d.New {
					names = append(names, filepath.Base(f.Dir))
				}
				s.StripPrefix = scan.SharedPrefixes(names)
				why.strip = len(s.StripPrefix) > 0
			}
		}
		claimed := otherHomelabPorts(dir, dockerOK)
		busy := func(p int) bool { return portInUse(s.Bind, p) || claimed[p] != "" }
		if busy(s.Port) {
			why.portOwner = claimed[s.Port]
			free := 0
			for _, p := range defaults.AltPorts {
				if !busy(p) {
					free = p
					break
				}
			}
			if free != 0 {
				why.busyPort, s.Port = s.Port, free
			} else {
				say("%swarning:%s port %d is in use on %s - free it before hl up, or hl set proxy port=<free port>", yellow, reset, s.Port, s.Bind)
			}
		}
		if dockerOK {
			s.Name, why.name = initName(context.Background(), dir, s)
		}
		cfg = s.Config(dir)
	}

	adds, err := initPlan(out, cfg, o, dockerOK, why)
	if err != nil {
		return err
	}
	toAdd := 0
	for _, a := range adds {
		if a.Kind == scan.Add {
			toAdd++
		}
	}

	// Ask once, when there is something to write (or the default homelab changes).
	old := config.DefaultHome()
	newDefault := old != "" && !samePath(old, dir)
	if !existing || saveProjects || saveStrip || toAdd > 0 || newDefault {
		switch {
		case o.yes:
		case !stdinTerminal(in):
			return withHint(errors.New("not a terminal, so nothing was written"), "pass --yes to apply the plan")
		case !newPrompter(in, out).confirm("Set it up like this?", true):
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
		// Only for a new homelab: one removed on purpose isn't put back.
		gi := filepath.Join(dir, ".gitignore")
		if _, err := os.Stat(gi); err != nil {
			_ = os.WriteFile(gi, []byte("# App data (databases, uploads) and files hl generates.\n"+defaults.DataDir+"/\n"+defaults.StateDir+"/\n"), defaults.FileMode)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, defaults.DataDir), defaults.DataDirMode); err != nil {
		return err
	}
	if err := config.SetDefaultHome(dir); err != nil {
		return err
	}
	say("default homelab: %s", dir)
	otherHome(out, dir)

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
// add.
func initPlan(out io.Writer, cfg *config.Config, o initOptions, dockerOK bool, why initWhy) ([]scan.Action, error) {
	w := func(label, format string, a ...any) {
		fmt.Fprintf(out, "  %-10s %s\n", label, fmt.Sprintf(format, a...))
	}
	fmt.Fprintf(out, "\n%shl init plan%s\n", bold, reset)
	existing := exists(cfg.File())
	if existing {
		kept := "hl.yml kept"
		if why.saveProjects || why.saveStrip {
			kept = "hl.yml updated as below"
		}
		w("homelab", "existing homelab %s (%s)", cfg.Dir, kept)
	} else {
		w("homelab", "%s (new hl.yml, data/)", cfg.Dir)
	}
	if old := config.DefaultHome(); old != "" && !samePath(old, cfg.Dir) {
		w("default", "this homelab becomes the default, instead of %s (hl --home %q ... still reaches that one)", old, old)
	}
	switch {
	case why.saveProjects:
		w("projects", "+ %s (hl.yml updated)", strings.Join(cfg.Projects, ", "))
	case len(cfg.Projects) > 0:
		w("projects", "%s", strings.Join(cfg.Projects, ", "))
	case existing && len(candidateProjects()) > 0:
		// Not written into an existing hl.yml unasked.
		w("projects", "none - to look for apps in %[1]s: hl init --projects %[1]s", strings.Join(candidateProjects(), ","))
	default:
		w("projects", "none - pass --projects <folder>, or later hl set projects+=<folder>")
	}
	if len(cfg.StripPrefix) > 0 {
		switch {
		case why.saveStrip:
			w("app names", "+ strip_prefix %s (hl.yml updated)", strings.Join(cfg.StripPrefix, ", "))
		case existing:
			w("app names", "without %s (strip_prefix)", strings.Join(cfg.StripPrefix, ", "))
		case why.strip:
			w("app names", "without %s - the folders share it (--strip-prefix none keeps whole names)", strings.Join(cfg.StripPrefix, ", "))
		default:
			w("app names", "without %s", strings.Join(cfg.StripPrefix, ", "))
		}
	}
	if why.name != "" {
		w("name", "%s - %s, so this one's Docker names are %s%s-<app> and %s", cfg.Name, why.name,
			defaults.Prefix, cfg.Name, cfg.ProxyProject())
	}
	reach := "this machine only"
	if ip := net.ParseIP(cfg.Proxy.Bind); ip != nil && !ip.IsLoopback() {
		reach = "your network too"
	}
	switch {
	case why.busyPort != 0 && why.portOwner != "":
		reach += fmt.Sprintf("; port %d is the proxy port of the homelab in %s", why.busyPort, why.portOwner)
	case why.busyPort != 0:
		reach += fmt.Sprintf("; port %d is in use", why.busyPort)
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
			plan = scan.PlanAdds(cfg, d, modelFunc(cfg), varsFunc(cfg))
		}
	}

	chosen := plan
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
	fmt.Fprintln(out)
	return chosen, nil
}

// initDir is the homelab folder init sets up: the one given; else the homelab hl already
// uses (--home, $HL_HOME, an hl.yml at or above the current folder, the recorded default), so
// running init again adds new apps to it; else ~/homelab (defaults.HomelabDir). Never the
// current folder by itself: `cd ~ && hl init` must not turn the home folder into a homelab.
func initDir(args []string) (string, error) {
	if len(args) == 1 {
		return filepath.Abs(config.ExpandHome(args[0]))
	}
	if home, err := config.Home(homeFlag); err == nil {
		return home, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", withHint(err, "name the homelab folder: hl init <folder>")
	}
	return filepath.Join(home, defaults.HomelabDir), nil
}

// initWhy is what init changed in a new homelab's plan to avoid a clash, for the plan to say.
type initWhy struct {
	busyPort  int    // the default proxy port, in use, so another one was picked
	portOwner string // the other homelab whose proxy has that port, "" when something else uses it
	name      string // why the homelab got a name: who uses the default Docker names
	strip     bool   // strip_prefix was worked out from the folder names, not given

	// An existing hl.yml: what the flags add to it.
	saveProjects, saveStrip bool
}

// initName picks a name for a new homelab when another homelab on this Docker already uses the
// default proxy project or network: the folder's name, else defaults.HomelabDir, with -2, -3,
// ... until the names are free. It returns the name and why one was needed; ("", "") when the
// defaults are free.
func initName(ctx context.Context, dir string, s config.InitSettings) (name, why string) {
	s.Name = ""
	if why = proxyNamesTaken(ctx, s.Config(dir)); why == "" {
		return "", ""
	}
	base := strings.Trim(config.ID(config.NameFor(filepath.Base(dir))), "-")
	if base == "" {
		base = defaults.HomelabDir
	}
	for i := 1; i <= defaults.MaxNameTries; i++ {
		s.Name = base
		if i > 1 {
			s.Name = fmt.Sprintf("%s-%d", base, i)
		}
		if proxyNamesTaken(ctx, s.Config(dir)) == "" {
			return s.Name, why
		}
	}
	return "", "" // hl doctor and hl up explain the clash
}

// otherHomelabPorts are the proxy ports other homelabs on this machine are set to, whether
// their proxy runs or not, with the homelab's folder: the default homelab, and (when Docker
// answers) every homelab that has containers, stopped ones too. A port free right now may be
// one of theirs, and both proxies can't have it once they run.
func otherHomelabPorts(dir string, dockerOK bool) map[int]string {
	homes := []string{config.DefaultHome()}
	if dockerOK {
		if inv, err := containers(context.Background()); err == nil {
			for _, cs := range inv.byProject {
				for _, c := range cs {
					if strings.Contains(c.configFiles, string(filepath.Separator)+defaults.StateDir+string(filepath.Separator)) {
						homes = append(homes, homelabOf(c.configFiles))
					}
				}
			}
		}
	}
	out := map[int]string{}
	for _, h := range homes {
		if h == "" || sameDir(h, dir) {
			continue
		}
		if cfg, err := config.Load(h); err == nil {
			if _, ok := out[cfg.Proxy.Port]; !ok {
				out[cfg.Proxy.Port] = cfg.Dir
			}
		}
	}
	return out
}

// otherHome warns when hl commands won't use the homelab init just made default, because
// --home, $HL_HOME or an hl.yml at or above the current folder comes first, and says how to fix it.
func otherHome(out io.Writer, dir string) {
	h, err := config.Home(homeFlag)
	if err != nil || sameDir(h, dir) {
		return
	}
	warn := func(format string, a ...any) {
		fmt.Fprintf(out, "%swarning:%s %s\n", yellow, reset, fmt.Sprintf(format, a...))
	}
	switch {
	case homeFlag != "":
		warn("--home %s is not the homelab init set up (%s); commands without --home use %[2]s", h, dir)
	case os.Getenv(defaults.HomeEnv) != "":
		warn("$%s is %s, so hl commands use that homelab, not %s - unset %[1]s, or set it to %[3]s", defaults.HomeEnv, h, dir)
	default:
		warn("the current folder is inside the homelab %s, so hl commands run here use that one - "+
			"run them outside it, or pass --home %s", h, dir)
	}
}

// sameDir reports whether a and b are the same folder, symlinks resolved.
func sameDir(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
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
