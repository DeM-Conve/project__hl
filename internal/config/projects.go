package config

// The projects folders `hl init` and `hl sync` look in for apps, and the
// comment-keeping hl.yml edits they need.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
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
	return nil
}

// InitSettings are the answers `hl init` writes into a new hl.yml.
type InitSettings struct {
	Bind     string
	Port     int
	Routing  string
	Projects []string
}

// WriteNew writes a new <dir>/hl.yml from Template with s filled in. It
// refuses to overwrite an existing file.
func WriteNew(dir string, s InitSettings) error {
	path := filepath.Join(dir, FileName)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(Template); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return editDoc(dir, func(root *yaml.Node) error {
		proxy := mapValue(root, "proxy")
		if proxy == nil || proxy.Kind != yaml.MappingNode {
			return fmt.Errorf("%s: no proxy section", path)
		}
		if s.Bind != "" {
			setScalar(proxy, "bind", s.Bind, "!!str")
		}
		if s.Port != 0 {
			setScalar(proxy, "port", strconv.Itoa(s.Port), "!!int")
		}
		if s.Routing != "" {
			setScalar(proxy, "routing", s.Routing, "!!str")
		}
		if len(s.Projects) > 0 {
			setProjects(root, s.Projects)
		}
		return nil
	})
}

// SetProjects writes the `projects` list into <dir>/hl.yml, keeping comments.
func SetProjects(dir string, projects []string) error {
	return editDoc(dir, func(root *yaml.Node) error {
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

func setProjects(root *yaml.Node, projects []string) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, p := range projects {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p})
	}
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "projects" {
			seq.LineComment = root.Content[i+1].LineComment
			root.Content[i+1] = seq
			return
		}
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Value: "projects", HeadComment: projectsComment}
	// Before apps:, so the file reads top-down: proxy, projects, apps.
	at := len(root.Content)
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "apps" {
			at = i
		}
	}
	rest := append([]*yaml.Node{key, seq}, root.Content[at:]...)
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

// editDoc edits the top-level mapping of <dir>/hl.yml, keeping comments and order.
func editDoc(dir string, edit func(root *yaml.Node) error) error {
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
	if err := edit(doc.Content[0]); err != nil {
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
