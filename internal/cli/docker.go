package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// docker runs `docker <args>` and returns its stdout. A failure carries the
// end of docker's error output, so the message says what went wrong.
func docker(ctx context.Context, args ...string) (string, error) {
	return run2(ctx, "docker", args...)
}

func run2(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := compose.Exec(ctx, cmd)
	var ce *compose.Error
	if errors.As(err, &ce) && ce.Stderr != "" {
		return out.String(), &detailError{err: err, detail: lastLines(ce.Stderr, 3)}
	}
	return out.String(), err
}

// detailError adds the command's own error output to its message.
type detailError struct {
	err    error
	detail string
}

func (e *detailError) Error() string { return e.err.Error() + ": " + e.detail }
func (e *detailError) Unwrap() error { return e.err }

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}

// container is one container of a compose project, as `docker ps` sees it.
type container struct {
	project, service, state, status, configFiles, name string
}

// inventory is every compose container on this Docker, by project.
type inventory struct {
	byProject map[string][]container
}

// containers lists every compose container (one `docker ps` for the whole command).
func containers(ctx context.Context) (*inventory, error) {
	format := strings.Join([]string{
		`{{.Label "` + defaults.ComposeProjectLabel + `"}}`,
		`{{.Label "` + defaults.ComposeServiceLabel + `"}}`,
		"{{.State}}", "{{.Status}}",
		`{{.Label "` + defaults.ComposeConfigFilesLabel + `"}}`,
		"{{.Names}}",
	}, "\t")
	out, err := docker(ctx, "ps", "--all", "--filter", "label="+defaults.ComposeProjectLabel, "--format", format)
	if err != nil {
		return nil, err
	}
	inv := &inventory{byProject: map[string][]container{}}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(l, "\t")
		if len(f) < 6 {
			continue
		}
		c := container{project: f[0], service: f[1], state: f[2], status: f[3], configFiles: f[4], name: f[5]}
		inv.byProject[c.project] = append(inv.byProject[c.project], c)
	}
	for _, cs := range inv.byProject {
		sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	}
	return inv, nil
}

func (inv *inventory) of(project string) []container {
	if inv == nil {
		return nil
	}
	return inv.byProject[project]
}

// running reports whether any container of the project is up.
func (inv *inventory) running(project string) bool {
	for _, c := range inv.of(project) {
		if c.state == "running" {
			return true
		}
	}
	return false
}

// owner is the homelab folder whose generated files a project was started
// from, "" when it is this homelab's (or unknown).
func (inv *inventory) owner(cfg *config.Config, project string) string {
	mine := []string{cfg.StateRoot() + string(filepath.Separator)}
	if real, err := filepath.EvalSymlinks(cfg.Dir); err == nil {
		mine = append(mine, filepath.Join(real, defaults.StateDir)+string(filepath.Separator))
	}
	for _, c := range inv.of(project) {
		if c.configFiles == "" {
			continue
		}
		ours := false
		for _, m := range mine {
			if strings.Contains(c.configFiles, m) {
				ours = true
			}
		}
		if !ours {
			return homelabOf(c.configFiles)
		}
	}
	return ""
}

// homelabOf finds the homelab folder in a config_files label.
func homelabOf(configFiles string) string {
	marker := string(filepath.Separator) + defaults.StateDir + string(filepath.Separator)
	for _, f := range strings.Split(configFiles, ",") {
		if i := strings.LastIndex(f, marker); i >= 0 {
			return f[:i]
		}
	}
	return configFiles
}

// mine fails when another homelab runs the same compose project, so hl never
// stops or replaces it.
func (inv *inventory) mine(cfg *config.Config, project, what string) error {
	other := inv.owner(cfg, project)
	if other == "" {
		return nil
	}
	return withHint(fmt.Errorf("%s: Docker project %s belongs to another homelab (%s)", what, project, other),
		"two homelabs on one Docker need different names: add `name: <something>` to %s (projects become %s<name>-...), "+
			"or stop the other one (`hl --home %q down`)", cfg.File(), defaults.Prefix, other)
}

// checkPort fails when something else listens on the proxy's address.
func checkPort(ctx context.Context, cfg *config.Config) error {
	bind, port := cfg.Proxy.Bind, cfg.Proxy.Port
	if !portInUse(bind, port) {
		return nil
	}
	who := whoUses(ctx, port)
	if who == "" {
		who = "another program"
	}
	hint := "stop it, or set proxy.port in " + cfg.File() + " to a free port"
	if free := freePort(bind); free != 0 {
		hint += fmt.Sprintf(" (%d is free)", free)
	}
	return withHint(fmt.Errorf("port %d on %s is already in use by %s", port, bind, who), "%s", hint)
}

// addrInUse tries to listen on the address; when that isn't allowed (a low
// port without privileges) it tries to connect instead.
func addrInUse(bind string, port int) bool {
	addr := net.JoinHostPort(bind, strconv.Itoa(port))
	l, err := net.Listen("tcp", addr)
	if err == nil {
		l.Close()
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	host := bind
	if ip := net.ParseIP(bind); ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// freePort suggests a port near the usual alternatives that is free.
func freePort(bind string) int {
	for _, p := range []int{8080, 8000, 8081, 8888, 8090} {
		if !portInUse(bind, p) {
			return p
		}
	}
	return 0
}

var ssUserRE = regexp.MustCompile(`\(\("([^"]+)",pid=(\d+)`)

// whoUses names what listens on a port: a Docker container, else the process
// lsof or ss can see (only your own without root). "" when unknown.
func whoUses(ctx context.Context, port int) string {
	if out, err := docker(ctx, "ps", "--filter", "publish="+strconv.Itoa(port), "--format", "{{.Names}}"); err == nil {
		if names := strings.Fields(out); len(names) > 0 {
			return "Docker container " + strings.Join(names, ", ")
		}
	}
	if _, err := exec.LookPath("lsof"); err == nil {
		out, _ := run2(ctx, "lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fpc")
		var pid, cmd string
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "p") && pid == "" {
				pid = l[1:]
			}
			if strings.HasPrefix(l, "c") && cmd == "" {
				cmd = l[1:]
			}
		}
		if cmd != "" {
			return fmt.Sprintf("%s (pid %s)", cmd, pid)
		}
	}
	if _, err := exec.LookPath("ss"); err == nil {
		out, _ := run2(ctx, "ss", "-Hltnp", "sport = :"+strconv.Itoa(port))
		if m := ssUserRE.FindStringSubmatch(out); m != nil {
			return fmt.Sprintf("%s (pid %s)", m[1], m[2])
		}
	}
	return ""
}

// dockerNetwork is one Docker network and its subnets.
type dockerNetwork struct {
	name    string
	subnets []*net.IPNet
}

func networks(ctx context.Context) ([]dockerNetwork, error) {
	ids, err := docker(ctx, "network", "ls", "--quiet")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(ids) == "" {
		return nil, nil
	}
	args := append([]string{"network", "inspect", "--format", "{{.Name}}\t{{range .IPAM.Config}}{{.Subnet}} {{end}}"}, strings.Fields(ids)...)
	out, err := docker(ctx, args...)
	if err != nil {
		return nil, err
	}
	var nets []dockerNetwork
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		name, subnets, _ := strings.Cut(l, "\t")
		n := dockerNetwork{name: strings.TrimSpace(name)}
		for _, s := range strings.Fields(subnets) {
			if _, ipn, err := net.ParseCIDR(s); err == nil {
				n.subnets = append(n.subnets, ipn)
			}
		}
		nets = append(nets, n)
	}
	return nets, nil
}

func overlaps(a, b *net.IPNet) bool { return a.Contains(b.IP) || b.Contains(a.IP) }

// checkNetwork fails when proxy.subnet can't be used: the proxy network
// exists with another subnet, or the subnet overlaps another Docker network
// or this machine's own networks.
func checkNetwork(ctx context.Context, cfg *config.Config) error {
	if cfg.Proxy.Subnet == "" {
		return nil
	}
	_, want, err := net.ParseCIDR(cfg.Proxy.Subnet)
	if err != nil {
		return err
	}
	nets, err := networks(ctx)
	if err != nil {
		return err
	}
	var used []*net.IPNet
	for _, n := range nets {
		if n.name == cfg.Proxy.Network {
			if len(n.subnets) > 0 && n.subnets[0].String() != want.String() {
				return withHint(fmt.Errorf("network %s exists with subnet %s, but proxy.subnet is %s", n.name, n.subnets[0], want),
					"stop everything with `hl down` (that removes the network), then `hl up`; or set proxy.subnet: %s", n.subnets[0])
			}
			continue
		}
		for _, s := range n.subnets {
			used = append(used, s)
			if overlaps(s, want) {
				return withHint(fmt.Errorf("proxy.subnet %s overlaps Docker network %s (%s)", want, n.name, s),
					"%s", subnetFix(cfg, used, nets))
			}
		}
	}
	for _, s := range hostSubnets() {
		if overlaps(s, want) {
			return withHint(fmt.Errorf("proxy.subnet %s overlaps this machine's network %s", want, s),
				"%s", subnetFix(cfg, append(used, s), nets))
		}
	}
	return nil
}

func subnetFix(cfg *config.Config, used []*net.IPNet, nets []dockerNetwork) string {
	for _, n := range nets {
		used = append(used, n.subnets...)
	}
	used = append(used, hostSubnets()...)
	if free := freeSubnet(used); free != "" {
		return fmt.Sprintf("set proxy.subnet: %s in %s (free), or remove it to let Docker pick", free, cfg.File())
	}
	return "set another proxy.subnet in " + cfg.File() + ", or remove it to let Docker pick"
}

// hostSubnets are the networks of this machine's interfaces; tests replace it.
var hostSubnets = func() []*net.IPNet {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []*net.IPNet
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			out = append(out, ipn)
		}
	}
	return out
}

// freeSubnet is a private /16 that overlaps none of used, "" if none is.
func freeSubnet(used []*net.IPNet) string {
	for i := 31; i >= 16; i-- {
		_, c, _ := net.ParseCIDR(fmt.Sprintf("172.%d.0.0/16", i))
		ok := true
		for _, u := range used {
			if overlaps(c, u) {
				ok = false
				break
			}
		}
		if ok {
			return c.String()
		}
	}
	return ""
}
