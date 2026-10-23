package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/gen"
)

// proxyArg is `hl config proxy`: the proxy's Traefik config instead of an app's.
const proxyArg = "proxy"

// Labels compose puts on the containers and networks it creates.
const (
	composeProjectLabel    = defaults.ComposeProjectLabel
	composeWorkingDirLabel = defaults.ComposeWorkingDirLabel
	composeConfigHashLabel = defaults.ComposeConfigHashLabel
)

// proxyProject writes the proxy's files and returns its compose project. It
// refuses to touch a proxy or network that another homelab runs.
func proxyProject(cfg *config.Config) (*compose.Project, error) {
	if err := proxyClash(cfg); err != nil {
		return nil, err
	}
	files, _, err := proxyFiles(cfg)
	if err != nil {
		return nil, err
	}
	f := filepath.Join(cfg.ProxyDir(), defaults.ProxyComposeFile)
	if err := writeGenerated(f, gen.Proxy(cfg.Proxy, files)); err != nil {
		return nil, err
	}
	return &compose.Project{Name: cfg.Proxy.Project, Dir: cfg.ProxyDir(), Files: []string{f}}, nil
}

// proxyFiles writes the generated Traefik static config, or checks the
// user's own (proxy.config), and returns what the proxy mounts. warnings are
// things in the user's file that don't stop the proxy from working.
func proxyFiles(cfg *config.Config) (gen.ProxyFiles, []string, error) {
	var files gen.ProxyFiles
	if err := os.MkdirAll(cfg.ProxyDir(), defaults.DataDirMode); err != nil {
		return files, nil, err
	}
	if d := cfg.ProxyDynamicFile(); d != "" {
		st, err := os.Stat(d)
		if err != nil || st.IsDir() {
			return files, nil, fmt.Errorf("proxy.dynamic: %s is not a file - create it, or remove proxy.dynamic from hl.yml", d)
		}
		files.Dynamic = d
	}
	generated := filepath.Join(cfg.ProxyDir(), defaults.TraefikStaticFile)
	files.Static = cfg.ProxyStaticFile()
	if cfg.Proxy.Config == "" {
		text := gen.Traefik(cfg.Proxy, files.Dynamic)
		if err := writeGenerated(generated, text); err != nil {
			return files, nil, err
		}
		files.Hash, files.Ping = gen.Hash([]byte(text)), true
		return files, nil, nil
	}
	b, err := os.ReadFile(files.Static)
	if err != nil {
		return files, nil, fmt.Errorf("proxy.config: can't read %s - create it, or remove proxy.config from hl.yml to use the generated one", files.Static)
	}
	ping, problems, warnings := gen.CheckTraefik(b, cfg.Proxy, files.Dynamic)
	if len(problems) > 0 {
		return files, warnings, fmt.Errorf("proxy.config %s can't run hl's apps:\n  - %s", files.Static, strings.Join(problems, "\n  - "))
	}
	// Not used while proxy.config is set; don't leave a file that looks in effect.
	_ = os.Remove(generated)
	files.Hash, files.Ping = gen.Hash(b), ping
	return files, warnings, nil
}

// proxyClash is an error when the proxy's compose project or network already
// belongs to another homelab (or something else). Without docker it says nothing.
func proxyClash(cfg *config.Config) error {
	p := cfg.Proxy
	out, err := exec.Command("docker", "ps", "-a", "--filter", "label="+composeProjectLabel+"="+p.Project,
		"--format", `{{.Names}}|{{.Label "`+composeWorkingDirLabel+`"}}`).Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			name, dir, ok := strings.Cut(line, "|")
			if ok && dir != "" && !samePath(dir, cfg.ProxyDir()) {
				return fmt.Errorf("the proxy project %q is run by another homelab (%s) - to run this one beside it, "+
					"set a different proxy.project, proxy.network and proxy.port in %s (if that homelab is gone: docker rm -f %s)",
					p.Project, filepath.Dir(filepath.Dir(dir)), filepath.Join(cfg.Dir, config.FileName), name)
			}
		}
	}
	out, err = exec.Command("docker", "network", "inspect", "--format",
		`{{index .Labels "`+composeProjectLabel+`"}}`, p.Network).Output()
	if err != nil {
		return nil // no such network yet (or no docker)
	}
	switch owner := strings.TrimSpace(string(out)); owner {
	case p.Project:
		return nil
	case "":
		return fmt.Errorf("the Docker network %q exists but wasn't created by hl's proxy - remove it (docker network rm %s) or set another proxy.network", p.Network, p.Network)
	default:
		return fmt.Errorf("the Docker network %q belongs to compose project %q (another homelab?) - set a different proxy.network in hl.yml", p.Network, owner)
	}
}

func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// printProxyConfig prints the Traefik static config the proxy runs with.
func printProxyConfig() error {
	cfg, err := load()
	if err != nil {
		return err
	}
	files, warnings, ferr := proxyFiles(cfg)
	b, err := os.ReadFile(files.Static)
	if err != nil {
		if ferr != nil {
			return ferr
		}
		return err
	}
	fmt.Fprintf(stdout, "# %s\n%s", files.Static, b)
	for _, w := range warnings {
		warn("%s", w)
	}
	return ferr
}

func proxyConfigCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Print the proxy's Traefik static config (same as hl config proxy)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return printProxyConfig() },
	}
}

func proxyStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "The proxy's state, health, image and config files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			p := cfg.Proxy
			clash := proxyClash(cfg)
			files, warnings, ferr := proxyFiles(cfg)

			state, image, hash := red+"stopped"+reset, "", ""
			out, err := exec.Command("docker", "ps", "-a", "--filter", "label="+composeProjectLabel+"="+p.Project,
				"--format", `{{.State}}|{{.Status}}|{{.Image}}|{{.Label "`+composeConfigHashLabel+`"}}`).Output()
			if err != nil {
				state = yellow + "unknown (docker not reachable)" + reset
			} else if line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]); line != "" {
				f := strings.SplitN(line, "|", 4)
				for len(f) < 4 {
					f = append(f, "")
				}
				image, hash = f[2], f[3]
				switch {
				case f[0] != "running":
					state = red + f[0] + reset
				case strings.Contains(f[1], "(unhealthy)"):
					state = red + "running, unhealthy" + reset
				case strings.Contains(f[1], "health: starting"):
					state = yellow + "running, starting" + reset
				case strings.Contains(f[1], "(healthy)"):
					state = green + "running, healthy" + reset
				default:
					state = green + "running" + reset + " (no healthcheck)"
				}
			}

			host := ""
			if p.Port != 80 {
				host = fmt.Sprintf(":%d", p.Port)
			}
			source := "generated from hl.yml"
			if p.Config != "" {
				source = "proxy.config"
			}
			dash := "off"
			if gen.Dashboard(p) {
				dash = "http://" + defaults.DashboardHostPrefix + p.Domain + host
			}
			row := func(k, v string) { say("%s%-10s%s %s", bold, k, reset, v) }
			row("proxy", state+"  "+dim+"(project "+p.Project+", network "+p.Network+")"+reset)
			if image != "" && image != p.Image {
				row("image", image+"  "+dim+"(hl.yml: "+p.Image+")"+reset)
			} else {
				row("image", p.Image)
			}
			row("listen", fmt.Sprintf("http://%s:%d", p.Bind, p.Port))
			row("config", files.Static+"  "+dim+"("+source+")"+reset)
			if files.Dynamic != "" {
				row("dynamic", files.Dynamic)
			} else {
				row("dynamic", "-")
			}
			row("dashboard", dash)
			switch {
			case p.Config != "":
				row("access log", "as set in proxy.config")
			case p.AccessLog:
				row("access log", "on (hl proxy logs)")
			default:
				row("access log", "off")
			}

			for _, w := range warnings {
				warn("%s", w)
			}
			if clash != nil {
				say("%s✗%s %v", red, reset, clash)
			}
			if ferr != nil {
				say("%s✗%s %v", red, reset, ferr)
			}
			if older, known := gen.ImageOlder(p.Image, defaults.TestedTraefikVersion); older || !known {
				warn("%s", imageAdvice(p.Image, known))
			}
			if hash != "" && clash == nil && ferr == nil {
				if want := proxyConfigHash(cfg); want != "" && want != hash {
					warn("the running proxy predates the current config - apply it with: hl proxy up")
				}
			}
			if image == "" && err == nil {
				say("start it: hl proxy up")
			}
			return nil
		},
	}
}

// proxyConfigHash is compose's hash of the proxy service as hl would run it now.
func proxyConfigHash(cfg *config.Config) string {
	p, err := proxyProject(cfg)
	if err != nil {
		return ""
	}
	out, err := p.Command("config", "--hash", defaults.ProxyService).Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	if len(f) != 2 {
		return ""
	}
	return f[1]
}

func imageAdvice(image string, known bool) string {
	tested := defaults.DefaultProxyImage
	if !known {
		return fmt.Sprintf("can't tell which Traefik version proxy.image %q is; hl is tested with %s", image, tested)
	}
	return fmt.Sprintf("proxy.image %s is older than %s, the version hl is tested with - remove proxy.image from hl.yml to use it", image, tested)
}

// doctorProxy checks the proxy's config files, that its project and network
// are this homelab's, and its image version.
func doctorProxy(cfg *config.Config, check func(ok bool, what, fix string)) {
	files, warnings, err := proxyFiles(cfg)
	check(err == nil, "proxy config "+files.Static, fmt.Sprint(err))
	for _, w := range warnings {
		warn("%s", w)
	}
	err = proxyClash(cfg)
	check(err == nil, "proxy project "+cfg.Proxy.Project+" and network "+cfg.Proxy.Network+" are this homelab's", fmt.Sprint(err))
	if older, known := gen.ImageOlder(cfg.Proxy.Image, defaults.TestedTraefikVersion); older || !known {
		warn("%s", imageAdvice(cfg.Proxy.Image, known))
	}
}
