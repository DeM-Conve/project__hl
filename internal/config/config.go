// Package config reads and writes hl.yml, the one file that describes a
// homelab: the proxy and the apps behind it.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/fsutil"
)

// FileName is the config file at the root of a homelab folder.
const FileName = defaults.ConfigFile

// Routing modes: an app is reached at <domain>/<app>/, at <app>.<domain>, or
// not through the proxy at all.
const (
	RoutingPath = "path"
	RoutingHost = "host"
	RoutingNone = "none"
)

type Config struct {
	// Name sets this homelab apart from others on the same Docker: its compose
	// projects become hl-<name>-<app> and its default network hl-<name>-proxy.
	// Empty for the only (or first) homelab.
	Name string `yaml:"name,omitempty"`
	// The hl.yml format; 0 (no version line) is the format before versions.
	Version int   `yaml:"version,omitempty"`
	Proxy   Proxy `yaml:"proxy"`
	// Settings passed to every app (e.g. TZ); an app's own env wins.
	Env  map[string]string `yaml:"env,omitempty"`
	Apps map[string]*App   `yaml:"apps"`

	// --- projects folders for `hl init` / `hl sync` (see projects.go) ---
	// Folders whose subfolders with a compose file are apps.
	Projects []string `yaml:"projects,omitempty"`
	// Folder-name patterns sync skips, on top of defaults.DefaultIgnore.
	Ignore []string `yaml:"ignore,omitempty"`
	// How deep below a projects folder to look for apps; 0 = defaults.ScanDepth.
	ScanDepth int `yaml:"scan_depth,omitempty"`
	// --- end projects ---

	// Dir is the homelab folder hl.yml was read from.
	Dir string `yaml:"-"`
}

type Proxy struct {
	// Address and host port the proxy listens on. 127.0.0.1 = this machine only.
	Bind string `yaml:"bind,omitempty"`
	Port int    `yaml:"port,omitempty"`
	// Path routing serves apps at <domain>/<app>/, host routing at <app>.<domain>.
	Domain string `yaml:"domain,omitempty"`
	// Default routing for apps that don't set their own: path or host.
	Routing string `yaml:"routing,omitempty"`
	// Docker network the proxy and the exposed app services share.
	Network string `yaml:"network,omitempty"`
	// Optional fixed subnet for that network, so apps can trust the proxy by address.
	Subnet string `yaml:"subnet,omitempty"`
	Image  string `yaml:"image,omitempty"`
	// Compose project the proxy runs as; set it to run a second homelab's proxy beside the first.
	Project string `yaml:"project,omitempty"`
	// Your own Traefik static config file instead of the generated one (relative to the homelab).
	Config string `yaml:"config,omitempty"`
	// A Traefik dynamic config file (file provider) for routes that aren't hl apps.
	Dynamic string `yaml:"dynamic,omitempty"`
	// Traefik's access log, on the proxy's output (hl proxy logs).
	AccessLog bool `yaml:"access_log,omitempty"`
	// Traefik's log level (default ERROR).
	LogLevel string `yaml:"log_level,omitempty"`
	// Dashboard turns on Traefik's dashboard at traefik.<domain>. It has no
	// password, so on a bind other than loopback it also needs DashboardPublic.
	Dashboard       bool `yaml:"dashboard,omitempty"`
	DashboardPublic bool `yaml:"dashboard_public,omitempty"`
}

type App struct {
	// The app's folder (its own compose project): absolute, or relative to the homelab folder.
	Path string `yaml:"path"`
	// Compose files inside Path. Default: the one `docker compose` would pick.
	Compose []string `yaml:"compose,omitempty"`
	// The service the proxy forwards to, and its port inside the container.
	Service string `yaml:"service,omitempty"`
	Port    int    `yaml:"port,omitempty"`
	// path, host or none. Default: proxy.routing.
	Routing string `yaml:"routing,omitempty"`
	// Path routing: the URL prefix. Default: /<app>.
	Prefix string `yaml:"prefix,omitempty"`
	// Host routing: the host name. Default: <app>.<domain>.
	Host string `yaml:"host,omitempty"`
	// Extra env files, relative to the homelab folder; the app's own .env is always read first.
	EnvFiles []string `yaml:"env_files,omitempty"`
	// Settings passed to the app's compose file. Values may use ${HL_URL},
	// ${HL_PREFIX}, ${HL_HOST}, ${HL_DATA} and ${HL_APP}.
	Env map[string]string `yaml:"env,omitempty"`
}

// ValidName reports whether name can be used as an app name.
func ValidName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("app name %q: use lowercase letters, digits, '.', '_' and '-', starting with a letter or digit", name)
	}
	if why, ok := IsReserved(name); ok {
		return fmt.Errorf("app name %q is reserved: %s - choose another name", name, why)
	}
	return nil
}

// NameFor turns a folder name into an app name: "My App" -> "my-app".
func NameFor(folder string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(folder) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			if b.Len() == 0 && (r == '.' || r == '_' || r == '-') {
				continue
			}
			if dash {
				b.WriteByte('-')
				dash = false
			}
			b.WriteRune(r)
		default:
			dash = b.Len() > 0
		}
	}
	return b.String()
}

// ID is name as Docker and Traefik accept it: my-app.io -> my-app-io.
func ID(name string) string {
	return strings.NewReplacer(".", "-", "_", "-").Replace(name)
}

// ProjectName is the compose project an app runs as in a homelab without a name.
func ProjectName(name string) string { return defaults.Prefix + ID(name) }

// ProjectName is the compose project the app runs as: hl-<app>, or
// hl-<name>-<app> in a named homelab.
func (c *Config) ProjectName(app string) string { return c.projectPrefix() + ID(app) }

// ProxyProject is the proxy's compose project: proxy.project, by default
// hl-proxy or hl-<name>-proxy.
func (c *Config) ProxyProject() string { return c.Proxy.Project }

func (c *Config) projectPrefix() string {
	if c.Name == "" {
		return defaults.Prefix
	}
	return defaults.Prefix + c.Name + "-"
}

// Load reads <dir>/hl.yml, checks it strictly and fills in defaults.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, FileName)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &NotFoundError{Dir: dir}
	}
	if err != nil {
		return nil, err
	}
	if err := problemsErr(path, check(b, dir)); err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Dir = dir
	c.defaults()
	return &c, nil
}

func (c *Config) defaults() {
	p := &c.Proxy
	if p.Bind == "" {
		p.Bind = defaults.Bind
	}
	if p.Port == 0 {
		p.Port = defaults.Port
	}
	if p.Domain == "" {
		p.Domain = defaults.Domain
	}
	if p.Routing == "" {
		p.Routing = defaults.Routing
	}
	if p.Network == "" {
		p.Network = defaults.DefaultProxyNetwork
		if c.Name != "" {
			p.Network = defaults.Prefix + c.Name + "-" + defaults.NetworkSuffix
		}
	}
	if p.Image == "" {
		p.Image = defaults.DefaultProxyImage
	}
	if p.Project == "" {
		p.Project = defaults.DefaultProxyProject
		if c.Name != "" {
			p.Project = defaults.Prefix + c.Name + "-" + defaults.ProxyProjectSuffix
		}
	}
	if c.Apps == nil {
		c.Apps = map[string]*App{}
	}
}

// Names returns the app names, sorted.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Apps))
	for n := range c.Apps {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// App returns the named app, or an error that suggests a close name.
func (c *Config) App(name string) (*App, error) {
	if a, ok := c.Apps[name]; ok {
		return a, nil
	}
	if len(c.Apps) == 0 {
		return nil, fmt.Errorf("no app %q - this homelab has no apps yet; add one with `hl add <folder>`", name)
	}
	if s := suggest(name, c.Names()); s != "" {
		return nil, fmt.Errorf("no app %q - did you mean %q?", name, s)
	}
	return nil, fmt.Errorf("no app %q (apps: %s)", name, strings.Join(c.Names(), ", "))
}

// AppDir is the app's folder as an absolute path.
func (c *Config) AppDir(a *App) string {
	p := expandHome(a.Path)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(c.Dir, p)
}

// Routing is how the app is reached: its own setting, else the proxy's default.
func (c *Config) Routing(a *App) string {
	if a.Routing != "" {
		return a.Routing
	}
	return c.Proxy.Routing
}

// Prefix is the app's URL prefix under path routing, "" otherwise.
func (c *Config) Prefix(name string, a *App) string {
	if c.Routing(a) != RoutingPath {
		return ""
	}
	if a.Prefix != "" {
		return a.Prefix
	}
	return "/" + name
}

// Host is the host name the app is reached at.
func (c *Config) Host(name string, a *App) string {
	if c.Routing(a) == RoutingHost {
		if a.Host != "" {
			return a.Host
		}
		return name + "." + c.Proxy.Domain
	}
	return c.Proxy.Domain
}

// DashboardHost is the host name of the Traefik dashboard.
func (c *Config) DashboardHost() string { return defaults.DashboardHostPrefix + c.Proxy.Domain }

// URL is where the app is opened in a browser, without a trailing slash;
// "" when it isn't behind the proxy.
func (c *Config) URL(name string, a *App) string {
	if c.Routing(a) == RoutingNone {
		return ""
	}
	return "http://" + c.hostPort(c.Host(name, a)) + c.Prefix(name, a)
}

// DashboardURL is where the Traefik dashboard is, "" when it is off.
func (c *Config) DashboardURL() string {
	if !c.Proxy.Dashboard {
		return ""
	}
	return "http://" + c.hostPort(c.DashboardHost()) + "/"
}

func (c *Config) hostPort(host string) string {
	if c.Proxy.Port != 80 {
		host += ":" + strconv.Itoa(c.Proxy.Port)
	}
	return host
}

// DataDir is where the app's volumes live on this machine.
func (c *Config) DataDir(name string) string { return filepath.Join(c.Dir, defaults.DataDir, name) }

// File is the homelab's hl.yml.
func (c *Config) File() string { return filepath.Join(c.Dir, FileName) }

// StateRoot holds everything hl generates.
func (c *Config) StateRoot() string { return filepath.Join(c.Dir, defaults.StateDir) }

// StateDir holds the files hl generates for the app.
func (c *Config) StateDir(name string) string { return filepath.Join(c.StateRoot(), name) }

// OverrideFile is the app's generated override.
func (c *Config) OverrideFile(name string) string {
	return filepath.Join(c.StateDir(name), defaults.OverrideFile)
}

// ProxyDir holds the proxy's generated compose file.
func (c *Config) ProxyDir() string { return filepath.Join(c.StateRoot(), defaults.ProxyStateDir) }

// ProxyFile is the proxy's generated compose file.
func (c *Config) ProxyFile() string { return filepath.Join(c.ProxyDir(), defaults.ProxyComposeFile) }

// hlVars are the values an app's settings can refer to, in a fixed order.
var hlVars = []string{"HL_APP", "HL_URL", "HL_PREFIX", "HL_HOST", "HL_DATA"}

// AppEnv is what the app's compose files can interpolate, as KEY=VALUE: the
// HL_* values, then the shared env, then the app's own (later ones win).
// Values may refer to HL_* and to the shell's environment as ${NAME}.
func (c *Config) AppEnv(name string, a *App) []string {
	hl := map[string]string{
		"HL_APP":    name,
		"HL_URL":    c.URL(name, a),
		"HL_PREFIX": c.Prefix(name, a),
		"HL_HOST":   c.Host(name, a),
		"HL_DATA":   c.DataDir(name),
	}
	expand := func(v string) string {
		return os.Expand(v, func(key string) string {
			if v, ok := hl[key]; ok {
				return v
			}
			return os.Getenv(key)
		})
	}
	env := make([]string, 0, len(hl)+len(c.Env)+len(a.Env))
	for _, k := range hlVars {
		env = append(env, k+"="+hl[k])
	}
	for _, m := range []map[string]string{c.Env, a.Env} {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			env = append(env, k+"="+expand(m[k]))
		}
	}
	return env
}

// EnvFiles are the app's extra env files as absolute paths.
func (c *Config) EnvFiles(a *App) []string {
	files := make([]string, 0, len(a.EnvFiles))
	for _, f := range a.EnvFiles {
		f = expandHome(f)
		if !filepath.IsAbs(f) {
			f = filepath.Join(c.Dir, f)
		}
		files = append(files, f)
	}
	return files
}

// AddApp appends an app to <dir>/hl.yml, keeping the file's comments and order.
func AddApp(dir, name string, a *App) error {
	return editApps(dir, func(apps *yaml.Node) error {
		for i := 0; i < len(apps.Content); i += 2 {
			if apps.Content[i].Value == name {
				return fmt.Errorf("app %q is already in %s", name, FileName)
			}
		}
		var v yaml.Node
		if err := v.Encode(a); err != nil {
			return err
		}
		apps.Content = append(apps.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: name}, &v)
		return nil
	})
}

// RemoveApp deletes an app from <dir>/hl.yml.
func RemoveApp(dir, name string) error {
	return editApps(dir, func(apps *yaml.Node) error {
		for i := 0; i < len(apps.Content); i += 2 {
			if apps.Content[i].Value == name {
				apps.Content = append(apps.Content[:i], apps.Content[i+2:]...)
				return nil
			}
		}
		return fmt.Errorf("no app %q in %s", name, FileName)
	})
}

func editApps(dir string, edit func(apps *yaml.Node) error) error {
	return editFile(dir, func(root *yaml.Node) error {
		apps := lookup(root, "apps")
		if apps == nil {
			apps = &yaml.Node{Kind: yaml.MappingNode}
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "apps"}, apps)
		}
		if apps.Kind != yaml.MappingNode {
			// `apps:` with nothing under it, or `apps: {}`.
			*apps = yaml.Node{Kind: yaml.MappingNode, HeadComment: apps.HeadComment, LineComment: apps.LineComment}
		}
		apps.Style = 0
		return edit(apps)
	})
}

// Template is the hl.yml `hl init` writes.
const Template = defaults.SchemaComment + `
# hl homelab: the proxy, and the apps behind it. Edit freely; hl reads it on every command.

version: 1

# name: lab2   # only to run a second homelab on the same Docker (projects hl-lab2-<app>)

proxy:
  # 127.0.0.1 = this machine only; 0.0.0.0 = reachable from your network.
  bind: 127.0.0.1
  port: 80
  # path: apps at http://<domain>/<app>/ (the app must support a base path, see README)
  # host: apps at http://<app>.<domain> (works with any app)
  routing: path
  domain: localhost
  # Traefik's dashboard at http://traefik.<domain>, no password. Off unless set; on a bind
  # other than 127.0.0.1 it also needs dashboard_public: true.
  # dashboard: true

# Settings passed to every app, e.g. the time zone. An app's own env wins.
# env:
#   TZ: UTC

# Added with ` + "`hl add <folder>`" + `. Per app you can set: routing, prefix, host,
# service, port, compose, env_files and env (see README).
apps: {}
`

// Home finds the homelab folder: the --home flag, $HL_HOME, the nearest
// folder above the current one with an hl.yml, then the default set by `hl init`.
func Home(flag string) (string, error) {
	if flag != "" {
		return filepath.Abs(expandHome(flag))
	}
	if env := os.Getenv(defaults.HomeEnv); env != "" {
		return filepath.Abs(expandHome(env))
	}
	if wd, err := os.Getwd(); err == nil {
		for d := wd; ; d = filepath.Dir(d) {
			if _, err := os.Stat(filepath.Join(d, FileName)); err == nil {
				return d, nil
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	if b, err := os.ReadFile(defaultHomeFile()); err == nil {
		if d := strings.TrimSpace(string(b)); d != "" {
			return d, nil
		}
	}
	return "", errors.New("no homelab found - create one with `hl init <folder>`, or point to it with --home or $" + defaults.HomeEnv)
}

// SetDefaultHome records dir as the homelab used outside any homelab folder.
func SetDefaultHome(dir string) error {
	f := defaultHomeFile()
	if err := os.MkdirAll(filepath.Dir(f), defaults.DataDirMode); err != nil {
		return err
	}
	return fsutil.WriteFile(f, []byte(dir+"\n"), defaults.FileMode)
}

// DefaultHomeFile is where `hl init` records the default homelab.
func DefaultHomeFile() string { return defaultHomeFile() }

func defaultHomeFile() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, defaults.UserConfigDir, defaults.HomeFile)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Validate checks the values hl can't work with, and clashes between apps and
// settings. Every problem is reported (a *ProblemsError); Check adds their lines.
func (c *Config) Validate() error {
	if c.Version > defaults.ConfigVersion {
		return tooNew(c.Version)
	}
	ps := &problems{}
	if c.Name != "" && !labNameRE.MatchString(c.Name) {
		ps.add("name", "%q: use lowercase letters, digits and '-'", c.Name)
	}
	p := c.Proxy
	ip := net.ParseIP(p.Bind)
	if ip == nil {
		ps.add("proxy.bind", "%q is not an IP address - use 127.0.0.1 (this machine) or 0.0.0.0 (your network)", p.Bind)
	}
	if p.Port < 1 || p.Port > 65535 {
		ps.add("proxy.port", "%d is not a port (1-65535)", p.Port)
	}
	if !hostRE.MatchString(p.Domain) {
		ps.add("proxy.domain", "%q is not a host name like localhost or home.example.com", p.Domain)
	}
	if r := p.Routing; r != RoutingPath && r != RoutingHost {
		ps.add("proxy.routing", "%q is not path or host", r)
	}
	if !networkRE.MatchString(p.Network) {
		ps.add("proxy.network", "%q is not a Docker network name (letters, digits, '_', '.', '-')", p.Network)
	}
	if p.Subnet != "" {
		if _, _, err := net.ParseCIDR(p.Subnet); err != nil {
			ps.add("proxy.subnet", "%q is not a subnet like 172.30.0.0/16", p.Subnet)
		}
	}
	if p.Image == "" || strings.ContainsAny(p.Image, " \t") {
		ps.add("proxy.image", "%q is not an image name like %s", p.Image, defaults.DefaultProxyImage)
	}
	// validateProxy's and validateProjects' messages start with the key: "proxy.log_level: ...".
	for _, check := range []func() error{c.validateProxy, c.validateProjects} {
		if err := check(); err != nil {
			key, msg, _ := strings.Cut(err.Error(), ": ")
			ps.add(key, "%s", msg)
		}
	}
	if p.Dashboard && ip != nil && !ip.IsLoopback() && !p.DashboardPublic {
		ps.add("proxy.dashboard", "the Traefik dashboard has no password, and proxy.bind %s makes it reachable from your network - "+
			"set proxy.dashboard_public: true if that is what you want, or bind 127.0.0.1", p.Bind)
	}
	for k := range c.Env {
		if !envKeyRE.MatchString(k) {
			ps.add("env."+k, "%q is not a valid variable name", k)
		}
	}

	ids := map[string]string{}
	prefixes := map[string]string{}
	hosts := map[string]string{}
	if p.Dashboard {
		hosts[strings.ToLower(c.DashboardHost())] = "the Traefik dashboard"
	}
	for _, name := range c.Names() {
		a := c.Apps[name]
		key := "apps." + name
		if err := ValidName(name); err != nil {
			ps.add(key, "%s", strings.TrimPrefix(err.Error(), fmt.Sprintf("app name %q", name)+": "))
			continue
		}
		if c.ProjectName(name) == c.Proxy.Project {
			ps.add(key, "its compose project %s is proxy.project - rename the app or set another proxy.project", c.ProjectName(name))
		}
		if other, ok := ids[ID(name)]; ok {
			ps.add(key, "runs as the same Docker project as %q (%s) - rename one", other, c.ProjectName(name))
		}
		ids[ID(name)] = name
		if a == nil || a.Path == "" {
			ps.add(key, "path is required: the app's folder, e.g. path: ~/code/%s", name)
			continue
		}
		routing := c.Routing(a)
		switch routing {
		case RoutingPath, RoutingHost, RoutingNone:
		default:
			ps.add(key+".routing", "%q is not path, host or none", a.Routing)
		}
		if a.Prefix != "" && !prefixRE.MatchString(a.Prefix) {
			ps.add(key+".prefix", "%q must look like /name, without a trailing slash", a.Prefix)
		}
		if a.Host != "" && !hostRE.MatchString(a.Host) {
			ps.add(key+".host", "%q is not a host name like %s.example.com", a.Host, name)
		}
		if a.Port < 0 || a.Port > 65535 {
			ps.add(key+".port", "%d is not a port (1-65535)", a.Port)
		}
		if a.Service != "" && !serviceRE.MatchString(a.Service) {
			ps.add(key+".service", "%q is not a compose service name", a.Service)
		}
		for k := range a.Env {
			if !envKeyRE.MatchString(k) {
				ps.add(key+".env."+k, "%q is not a valid variable name", k)
			}
		}
		switch routing {
		case RoutingPath:
			pre := c.Prefix(name, a)
			if other, ok := prefixes[pre]; ok {
				ps.add(key, "has the same URL prefix %s as %q - set apps.%s.prefix", pre, other, name)
			}
			prefixes[pre] = name
		case RoutingHost:
			h := strings.ToLower(c.Host(name, a))
			if other, ok := hosts[h]; ok {
				ps.add(key, "has the same host %s as %s - set apps.%s.host", h, other, name)
			}
			hosts[h] = strconv.Quote(name)
		}
	}
	return ps.err()
}

// problems collects what Validate finds.
type problems struct{ list []Problem }

func (ps *problems) add(key, format string, a ...any) {
	ps.list = append(ps.list, Problem{Key: key, Message: fmt.Sprintf(format, a...)})
}

func (ps *problems) err() error {
	if len(ps.list) == 0 {
		return nil
	}
	return &ProblemsError{File: FileName, Problems: ps.list}
}

// NotFoundError is a homelab folder without an hl.yml.
type NotFoundError struct{ Dir string }

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no %s in %s - run `hl init %s` to create it, or pass --home <homelab folder>", FileName, e.Dir, e.Dir)
}
