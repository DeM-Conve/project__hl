package config

// The projects folders `hl init` and `hl sync` look in for apps, and the
// comment-keeping hl.yml edits they need.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// ProxyDefaults is the proxy section as hl fills it in when hl.yml leaves it empty.
func ProxyDefaults() Proxy {
	var c Config
	c.defaults()
	return c.Proxy
}

// ProjectDirs are the `projects` folders as absolute paths, in hl.yml's order.
func (c *Config) ProjectDirs() []string {
	dirs := make([]string, 0, len(c.Projects))
	for _, p := range c.Projects {
		dirs = append(dirs, c.Abs(p))
	}
	return dirs
}

// Abs resolves a path from hl.yml: ~ is the current user's home, a relative
// path is relative to the homelab folder.
func (c *Config) Abs(p string) string {
	p = expandHome(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(c.Dir, p)
}

// ExpandHome turns a leading ~ into the current user's home folder.
func ExpandHome(p string) string { return expandHome(p) }

func (c *Config) validateProjects() error {
	for _, p := range c.Ignore {
		if _, err := filepath.Match(p, ""); err != nil {
			return fmt.Errorf("ignore: %q is not a valid pattern", p)
		}
	}
	if c.ScanDepth < 0 {
		return fmt.Errorf("scan_depth: %d must be 1 or more", c.ScanDepth)
	}
	for i, p := range c.Projects {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("projects[%d]: empty folder", i)
		}
	}
	for i, p := range c.StripPrefix {
		if p == "" {
			return fmt.Errorf("strip_prefix[%d]: empty prefix", i)
		}
	}
	return nil
}

// InitSettings are the answers `hl init` writes into a new hl.yml.
type InitSettings struct {
	// Name: the homelab's name, when another homelab on this Docker uses the default names.
	Name     string
	Bind     string
	Port     int
	Routing  string
	Projects []string
	// StripPrefix: folder-name prefixes left out of app names.
	StripPrefix []string
}

// Config is the homelab these settings make, defaults filled in, without writing anything:
// what `hl init` plans with before it asks.
func (s InitSettings) Config(dir string) *Config {
	c := &Config{Dir: dir, Name: s.Name, Projects: s.Projects, StripPrefix: s.StripPrefix,
		Proxy: Proxy{Bind: s.Bind, Port: s.Port, Routing: s.Routing}}
	c.defaults()
	return c
}

// nameComment heads the `name` hl init writes.
const nameComment = "# Another homelab on this Docker uses the default names, so this one's Docker projects\n" +
	"# and network are " + defaults.Prefix + "<name>-<app> and " + defaults.Prefix + "<name>-" + defaults.ProxyProjectSuffix + "."

// WriteNew writes a new, short <dir>/hl.yml: Template with the projects folders, strip_prefix
// and the proxy settings of s that differ from the defaults. It refuses to overwrite an
// existing file. Its top-level sections are one blank line apart, like Template's.
func WriteNew(dir string, s InitSettings) error {
	path := filepath.Join(dir, FileName)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(Template); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	out, err := edited(path, func(root *yaml.Node) error {
		if s.Name != "" {
			beforeApps(root, &yaml.Node{Kind: yaml.ScalarNode, Value: "name", HeadComment: nameComment},
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s.Name})
		}
		if len(s.Projects) > 0 {
			setProjects(root, s.Projects)
		}
		if len(s.StripPrefix) > 0 {
			setList(root, "strip_prefix", s.StripPrefix, stripPrefixComment)
		}
		pd := ProxyDefaults()
		proxy := &yaml.Node{Kind: yaml.MappingNode}
		if s.Bind != "" && s.Bind != pd.Bind {
			setScalar(proxy, "bind", s.Bind, "!!str")
		}
		if s.Port != 0 && s.Port != pd.Port {
			setScalar(proxy, "port", strconv.Itoa(s.Port), "!!int")
		}
		if s.Routing != "" && s.Routing != pd.Routing {
			setScalar(proxy, "routing", s.Routing, "!!str")
		}
		if len(proxy.Content) > 0 {
			beforeApps(root, &yaml.Node{Kind: yaml.ScalarNode, Value: "proxy"}, proxy)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return writeKeepMode(path, spaceSections(out))
}

// spaceSections puts one blank line before each top-level key of b (above its head comment)
// that doesn't have one yet. Only for a file hl writes new: edits never add blank lines.
func spaceSections(b []byte) []byte {
	lines := splitLines(b)
	var out []string
	topComment := false // the previous line is a top-level comment
	for i, l := range lines {
		top := l != "" && l[0] != ' ' && l[0] != '-'
		if top && i > 0 && lines[i-1] != "" && !topComment {
			out = append(out, "")
		}
		topComment = top && l[0] == '#'
		out = append(out, l)
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

// SetProjects writes the `projects` list into <dir>/hl.yml, keeping comments.
func SetProjects(dir string, projects []string) error {
	return editFile(dir, func(root *yaml.Node) error {
		setProjects(root, projects)
		return nil
	})
}

// PinApp sets an app's service and port in <dir>/hl.yml, keeping comments.
func PinApp(dir, name, service string, port int) error {
	return editApps(dir, func(apps *yaml.Node) error {
		a := mapValue(apps, name)
		if a == nil {
			return fmt.Errorf("no app %q in %s", name, FileName)
		}
		if a.Kind != yaml.MappingNode {
			return fmt.Errorf("apps.%s: expected a mapping", name)
		}
		setScalar(a, "service", service, "!!str")
		setScalar(a, "port", strconv.Itoa(port), "!!int")
		return nil
	})
}

const projectsComment = `Folders with your projects: each subfolder with a compose file is an app.
` + "`hl sync`" + ` adds new ones and removes apps whose folder is gone (their data stays).
Skip a folder with ignore: [pattern, ...] or a .hlignore file in it.`

const stripPrefixComment = `Left out of app names: folder project__blog becomes app blog.`

func setProjects(root *yaml.Node, projects []string) {
	setList(root, "projects", projects, projectsComment)
}

// SetList writes a top-level list (projects, ignore, strip_prefix) into <dir>/hl.yml, keeping comments;
// an empty list removes the key. The result is checked first: on any problem hl.yml is left
// as it was and the error lists them.
func SetList(dir, key string, values []string) error {
	path := filepath.Join(dir, FileName)
	out, err := edited(path, func(root *yaml.Node) error {
		if len(values) == 0 {
			remove(root, key)
			return nil
		}
		setList(root, key, values, "")
		return nil
	})
	if err != nil {
		return err
	}
	if ps := Check(out); len(ps) > 0 {
		return fmt.Errorf("%s not changed: %w", FileName, &ProblemsError{File: path, Problems: ps})
	}
	return writeKeepMode(path, out)
}

// setList sets key to a flow list of values, keeping the old value's comment; a new key goes
// before apps:, with comment above it.
func setList(root *yaml.Node, key string, values []string, comment string) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, p := range values {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p})
	}
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			seq.LineComment = root.Content[i+1].LineComment
			root.Content[i+1] = seq
			return
		}
	}
	beforeApps(root, &yaml.Node{Kind: yaml.ScalarNode, Value: key, HeadComment: comment}, seq)
}

// beforeApps adds key: val to the top-level mapping just before apps:, so the file reads
// top-down: settings, then apps.
func beforeApps(root, key, val *yaml.Node) {
	at := len(root.Content)
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "apps" {
			at = i
		}
	}
	rest := append([]*yaml.Node{key, val}, root.Content[at:]...)
	root.Content = append(root.Content[:at:at], rest...)
}

// mapValue is the value of key in mapping m, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setScalar sets key in mapping m to a scalar, keeping the old value's comment.
func setScalar(m *yaml.Node, key, value, tag string) {
	if v := mapValue(m, key); v != nil {
		*v = yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value, LineComment: v.LineComment}
		return
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value})
}
