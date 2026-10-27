package cli

import (
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/gen"
)

// proxyArg names the proxy where commands take an app (hl up proxy, hl logs proxy, ...). It is
// the reserved app name, so no app has it.
const proxyArg = defaults.ReservedAppName

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
	files, err := proxyFiles(cfg)
	if err != nil {
		return nil, err
	}
	f := filepath.Join(cfg.ProxyDir(), defaults.ProxyComposeFile)
	if err := writeGenerated(f, gen.Proxy(cfg.Proxy, files)); err != nil {
		return nil, err
	}
	return &compose.Project{Name: cfg.Proxy.Project, Dir: cfg.ProxyDir(), Files: []string{f}}, nil
}

// proxyFiles writes the generated Traefik static config and returns what the proxy mounts.
func proxyFiles(cfg *config.Config) (gen.ProxyFiles, error) {
	files := gen.ProxyFiles{Static: cfg.ProxyStaticFile()}
	text := gen.Traefik(cfg.Proxy)
	if err := writeGenerated(files.Static, text); err != nil {
		return files, err
	}
	files.Hash = gen.Hash([]byte(text))
	return files, nil
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
					"give it a name: `hl set name=<name>` and `hl set proxy port=<free port>` (if that homelab is gone: docker rm -f %s)",
					p.Project, filepath.Dir(filepath.Dir(dir)), name)
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
		return fmt.Errorf("the Docker network %q exists but wasn't created by hl's proxy - remove it (docker network rm %s) or give this homelab a name, which gives it its own network: `hl set name=<name>`", p.Network, p.Network)
	default:
		return fmt.Errorf("the Docker network %q belongs to compose project %q (another homelab?) - give this homelab a name, which gives it its own network: `hl set name=<name>`", p.Network, owner)
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

// showProxy is `hl show proxy`: the proxy's state and health, where it listens, every setting
// with where it comes from, and what is wrong or out of date.
func showProxy(cfg *config.Config) error {
	p := cfg.Proxy
	clash := proxyClash(cfg)
	files, ferr := proxyFiles(cfg)
	settings, err := config.Settings(cfg.Dir)
	if err != nil {
		return err
	}

	state, image, hash := red+"stopped"+reset, "", ""
	out, derr := exec.Command("docker", "ps", "-a", "--filter", "label="+composeProjectLabel+"="+p.Project,
		"--format", `{{.State}}|{{.Status}}|{{.Image}}|{{.Label "`+composeConfigHashLabel+`"}}`).Output()
	if derr != nil {
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

	say("%sproxy%s %s", bold, reset, state)
	w := table()
	fmt.Fprintf(w, "  listen\thttp://%s\n", net.JoinHostPort(p.Bind, strconv.Itoa(p.Port)))
	for _, st := range settings {
		key, ok := strings.CutPrefix(st.Key, "proxy.")
		if !ok {
			continue
		}
		v := st.Value
		if key == "image" && image != "" && image != p.Image {
			v += "  (running: " + image + ")"
		}
		fmt.Fprintf(w, "  %s\t%s  %s\n", key, v, from(st.Source))
	}
	named := "default"
	if cfg.Name != "" {
		named = "from name"
	}
	fmt.Fprintf(w, "  project\t%s  %s\n", p.Project, from(named))
	fmt.Fprintf(w, "  network\t%s  %s\n", p.Network, from(named))
	fmt.Fprintf(w, "  traefik config\t%s  %s\n", tilde(files.Static), from("generated from "+config.FileName))
	_ = w.Flush()

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
			warn("the running proxy predates the current config - apply it with: hl up proxy")
		}
	}
	if image == "" && derr == nil {
		say("start it: hl up proxy")
	}
	return nil
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
	return fmt.Sprintf("proxy.image %s is older than %s, the version hl is tested with - use it: `hl unset proxy image`", image, tested)
}

// doctorProxy checks the proxy's config file, that its project and network
// are this homelab's, and its image version.
func doctorProxy(cfg *config.Config, check func(ok bool, what, fix string)) {
	files, err := proxyFiles(cfg)
	check(err == nil, "proxy config "+files.Static, fmt.Sprint(err))
	err = proxyClash(cfg)
	check(err == nil, "proxy project "+cfg.Proxy.Project+" and network "+cfg.Proxy.Network+" are this homelab's", fmt.Sprint(err))
	if older, known := gen.ImageOlder(cfg.Proxy.Image, defaults.TestedTraefikVersion); older || !known {
		warn("%s", imageAdvice(cfg.Proxy.Image, known))
	}
}
