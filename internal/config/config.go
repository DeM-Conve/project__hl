// Package config reads and writes hl.yml, the one file that describes a
// homelab: the proxy and the apps behind it.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the config file at the root of a homelab folder.
const FileName = "hl.yml"

// Routing modes: an app is reached at <domain>/<app>/, at <app>.<domain>, or
// not through the proxy at all.
const (
	RoutingPath = "path"
	RoutingHost = "host"
	RoutingNone = "none"
)

type Config struct {
	Proxy Proxy `yaml:"proxy"`
	// Settings passed to every app (e.g. TZ); an app's own env wins.
	Env  map[string]string `yaml:"env,omitempty"`
	Apps map[string]*App   `yaml:"apps"`

	// --- projects folders for `hl init` / `hl sync` (see projects.go) ---
	// Folders whose subfolders with a compose file are apps.
	Projects []string `yaml:"projects,omitempty"`
	// Folder-name patterns sync skips, on top of scan.DefaultIgnore.
	Ignore []string `yaml:"ignore,omitempty"`
	// How deep below a projects folder to look for apps; 0 = scan.DefaultDepth.
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

var (
	nameRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	prefixRE = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)
)

// ValidName reports whether name can be used as an app name.
func ValidName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("app name %q: use lowercase letters, digits, '.', '_' and '-'", name)
	}
	return nil
}

// ID is name as Docker and Traefik accept it: tierlist.fm -> tierlist-fm.
func ID(name string) string {
	return strings.NewReplacer(".", "-", "_", "-").Replace(name)
}

// ProjectName is the compose project an app runs as.
func ProjectName(name string) string { return "hl-" + ID(name) }

// Load reads <dir>/hl.yml and fills in defaults.
func Load(dir string) (*Config, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, FileName), err)
	}
	c.Dir = dir
	c.defaults()
	return &c, c.Validate()
}

func (c *Config) defaults() {
	p := &c.Proxy
	if p.Bind == "" {
		p.Bind = "127.0.0.1"
	}
	if p.Port == 0 {
		p.Port = 80
	}
	if p.Domain == "" {
		p.Domain = "localhost"
	}
	if p.Routing == "" {
		p.Routing = RoutingPath
	}
	if p.Network == "" {
		p.Network = DefaultProxyNetwork
	}
	if p.Image == "" {
		p.Image = DefaultProxyImage
	}
	if p.Project == "" {
		p.Project = DefaultProxyProject
	}
	if c.Apps == nil {
		c.Apps = map[string]*App{}
	}
}

// Validate checks the settings hl can't work with.
func (c *Config) Validate() error {
	if r := c.Proxy.Routing; r != RoutingPath && r != RoutingHost {
		return fmt.Errorf("proxy.routing: %q is not path or host", r)
	}
	if err := c.validateProxy(); err != nil {
		return err
	}
	if err := c.validateProjects(); err != nil {
		return err
	}
	for name, a := range c.Apps {
		if err := ValidName(name); err != nil {
			return err
		}
		if a == nil || a.Path == "" {
			return fmt.Errorf("apps.%s: path is required", name)
		}
		switch c.Routing(a) {
		case RoutingPath, RoutingHost, RoutingNone:
		default:
			return fmt.Errorf("apps.%s.routing: %q is not path, host or none", name, a.Routing)
		}
		if a.Prefix != "" && !prefixRE.MatchString(a.Prefix) {
			return fmt.Errorf("apps.%s.prefix: %q must look like /name, without a trailing slash", name, a.Prefix)
		}
	}
	return nil
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
	for _, n := range c.Names() {
		if strings.HasPrefix(n, name) || strings.HasPrefix(name, n) {
			return nil, fmt.Errorf("no app %q - did you mean %q?", name, n)
		}
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

// URL is where the app is opened in a browser, without a trailing slash;
// "" when it isn't behind the proxy.
func (c *Config) URL(name string, a *App) string {
	if c.Routing(a) == RoutingNone {
		return ""
	}
	host := c.Host(name, a)
	if c.Proxy.Port != 80 {
		host += ":" + strconv.Itoa(c.Proxy.Port)
	}
	return "http://" + host + c.Prefix(name, a)
}

// DataDir is where the app's volumes live on this machine.
func (c *Config) DataDir(name string) string { return filepath.Join(c.Dir, "data", name) }

// StateDir holds the files hl generates for the app.
func (c *Config) StateDir(name string) string { return filepath.Join(c.Dir, ".hl", name) }

// ProxyDir holds the proxy's generated compose file.
func (c *Config) ProxyDir() string { return filepath.Join(c.Dir, ".hl", ProxyStateDir) }

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
	path := filepath.Join(dir, FileName)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s: expected a mapping at the top", path)
	}
	root := doc.Content[0]
	var apps *yaml.Node
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "apps" {
			apps = root.Content[i+1]
		}
	}
	if apps == nil {
		apps = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "apps"}, apps)
	}
	if apps.Kind != yaml.MappingNode {
		// `apps:` with nothing under it, or `apps: {}`.
		*apps = yaml.Node{Kind: yaml.MappingNode, HeadComment: apps.HeadComment, LineComment: apps.LineComment}
	}
	apps.Style = 0
	if err := edit(apps); err != nil {
		return err
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

// Template is the hl.yml `hl init` writes.
const Template = `# hl homelab: the proxy, and the apps behind it. Edit freely; hl reads it on every command.

proxy:
  # 127.0.0.1 = this machine only; 0.0.0.0 = reachable from your network.
  bind: 127.0.0.1
  port: 80
  # path: apps at http://<domain>/<app>/ (the app must support a base path, see README)
  # host: apps at http://<app>.<domain> (works with any app)
  routing: path
  domain: localhost

# Settings passed to every app, e.g. the time zone. An app's own env wins.
# env:
#   TZ: Europe/Berlin

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
	if env := os.Getenv("HL_HOME"); env != "" {
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
	return "", errors.New("no homelab found - run `hl init <folder>`, or set HL_HOME")
}

// SetDefaultHome records dir as the homelab used outside any homelab folder.
func SetDefaultHome(dir string) error {
	f := defaultHomeFile()
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f, []byte(dir+"\n"), 0o644)
}

func defaultHomeFile() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "hl", "home")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
