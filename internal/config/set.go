package config

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// Change is one edit to a group of settings: Key=Value, or with Unset the key removed.
// Keys are hl.yml keys of that group (port, routing, ...); a map setting takes a name: env.TZ.
type Change struct {
	Key, Value string
	Unset      bool
}

// The scopes `hl set`/`hl unset` change besides an app: the settings shared by every app
// (HomelabScope) and the proxy section. ProxyScope is the reserved app name, so no app has it.
const (
	HomelabScope = ""
	ProxyScope   = defaults.ReservedAppName
)

// homelabKeys are the top-level settings `hl set` changes. The others stay with hl edit or
// commands of their own: name renames every Docker object, projects/ignore/scan_depth
// belong to hl init and hl sync.
var homelabKeys = []string{"env"}

// AppKeys are the keys `hl set <app>` accepts, sorted; map settings end in "." (env.).
func AppKeys() []string { return keysOf(appFields()) }

// ProxyKeys are the keys `hl set proxy` accepts, sorted.
func ProxyKeys() []string { return keysOf(proxyFields()) }

// HomelabKeys are the keys `hl set` accepts without an app (the shared env.), sorted.
func HomelabKeys() []string { return keysOf(homelabFields()) }

// IsHomelabKey reports whether key (env, env.TZ) is a setting shared by every app.
func IsHomelabKey(key string) bool {
	field, _, _ := strings.Cut(key, ".")
	_, ok := homelabFields()[field]
	return ok
}

func appFields() map[string]reflect.Type   { return yamlFields(reflect.TypeOf(App{})) }
func proxyFields() map[string]reflect.Type { return yamlFields(reflect.TypeOf(Proxy{})) }
func homelabFields() map[string]reflect.Type {
	all := yamlFields(reflect.TypeOf(Config{}))
	out := map[string]reflect.Type{}
	for _, k := range homelabKeys {
		out[k] = all[k]
	}
	return out
}

func keysOf(fields map[string]reflect.Type) []string {
	keys := make([]string, 0, len(fields))
	for k, t := range fields {
		if t.Kind() == reflect.Map {
			k += "."
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// HasApp reports whether <dir>/hl.yml has an app called name (false when it can't be read).
func HasApp(dir, name string) bool {
	found := false
	_, _ = edited(filepath.Join(dir, FileName), func(root *yaml.Node) error {
		if apps := lookup(root, "apps"); apps != nil && apps.Kind == yaml.MappingNode {
			found = lookup(apps, name) != nil
		}
		return nil
	})
	return found
}

// Edit applies changes to scope in <dir>/hl.yml - HomelabScope (the shared env), ProxyScope
// (proxy:) or an app's name - keeping the file's comments and order. The result is checked
// first: if it has any problem, hl.yml is left as it was and the error lists them.
func Edit(dir, scope string, changes []Change) error {
	switch scope {
	case HomelabScope:
		return editScope(dir, changes, homelabFields(), func(root *yaml.Node) (*yaml.Node, error) { return root, nil })
	case ProxyScope:
		return editScope(dir, changes, proxyFields(), func(root *yaml.Node) (*yaml.Node, error) {
			return section(root, "proxy"), nil
		})
	}
	return EditApp(dir, scope, changes)
}

// EditApp is Edit for one app.
func EditApp(dir, name string, changes []Change) error {
	return editScope(dir, changes, appFields(), func(root *yaml.Node) (*yaml.Node, error) {
		apps := lookup(root, "apps")
		var app *yaml.Node
		var names []string
		if apps != nil && apps.Kind == yaml.MappingNode {
			app = lookup(apps, name)
			for i := 0; i < len(apps.Content); i += 2 {
				names = append(names, apps.Content[i].Value)
			}
		}
		if app == nil {
			if s := suggest(name, names); s != "" {
				return nil, fmt.Errorf("no app %q in %s - did you mean %q?", name, FileName, s)
			}
			return nil, fmt.Errorf("no app %q in %s (apps: %s)", name, FileName, strings.Join(names, ", "))
		}
		return app, nil
	})
}

// section is the mapping under key in m, added before apps: when missing.
func section(m *yaml.Node, key string) *yaml.Node {
	if n := lookup(m, key); n != nil {
		return n
	}
	n := &yaml.Node{Kind: yaml.MappingNode}
	at := len(m.Content)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == "apps" {
			at = i
			break
		}
	}
	kv := []*yaml.Node{{Kind: yaml.ScalarNode, Value: key}, n}
	m.Content = append(m.Content[:at], append(kv, m.Content[at:]...)...)
	return n
}

// editScope applies changes to the mapping find returns, checks the result and writes it.
func editScope(dir string, changes []Change, fields map[string]reflect.Type, find func(root *yaml.Node) (*yaml.Node, error)) error {
	path := filepath.Join(dir, FileName)
	out, err := edited(path, func(root *yaml.Node) error {
		target, err := find(root)
		if err != nil {
			return err
		}
		if target.Kind != yaml.MappingNode {
			// `proxy:` or an app with nothing under it.
			*target = yaml.Node{Kind: yaml.MappingNode, LineComment: target.LineComment, HeadComment: target.HeadComment}
		}
		if len(target.Content) == 0 {
			target.Style = 0 // `{}` becomes a block mapping
		}
		for _, c := range changes {
			if err := applyChange(target, fields, c); err != nil {
				return err
			}
		}
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

func applyChange(app *yaml.Node, fields map[string]reflect.Type, c Change) error {
	field, sub, hasSub := strings.Cut(c.Key, ".")
	t, ok := fields[field]
	if !ok {
		names := make([]string, 0, len(fields))
		for k := range fields {
			names = append(names, k)
		}
		sort.Strings(names)
		msg := fmt.Sprintf("unknown setting %q", c.Key)
		if s := suggest(field, names); s != "" {
			return fmt.Errorf("%s - did you mean %q?", msg, s)
		}
		return fmt.Errorf("%s - use one of: %s", msg, strings.Join(keysOf(fields), ", "))
	}
	isMap := t.Kind() == reflect.Map
	switch {
	case isMap && !hasSub && !c.Unset:
		return fmt.Errorf("%s needs a name: %s.NAME=value", field, field)
	case !isMap && hasSub:
		return fmt.Errorf("%s has no parts: use %s=value", field, field)
	case isMap && hasSub && sub == "":
		return fmt.Errorf("%s needs a name: %s.NAME", field, field)
	}

	target, key := app, field
	if hasSub {
		m := lookup(app, field)
		if m == nil || m.Kind != yaml.MappingNode {
			if c.Unset {
				return nil
			}
			if m == nil {
				m = section(app, field) // the shared env goes above apps:
			} else {
				*m = yaml.Node{Kind: yaml.MappingNode, LineComment: m.LineComment, HeadComment: m.HeadComment}
			}
		}
		if len(m.Content) == 0 {
			m.Style = 0 // `env: {}` becomes a block mapping
		}
		target, key, t = m, sub, t.Elem()
	}

	if c.Unset {
		remove(target, key)
		if hasSub && len(target.Content) == 0 {
			remove(app, field) // nothing left in env
		}
		return nil
	}
	var v any
	switch t.Kind() {
	case reflect.Int:
		n, err := strconv.Atoi(c.Value)
		if err != nil {
			return fmt.Errorf("%s: %q is not a whole number", c.Key, c.Value)
		}
		v = n
	case reflect.Slice:
		var items []string
		for _, s := range strings.Split(c.Value, ",") {
			if s = strings.TrimSpace(s); s != "" {
				items = append(items, s)
			}
		}
		v = items
	case reflect.String:
		v = c.Value
	case reflect.Bool:
		b, err := strconv.ParseBool(c.Value)
		if err != nil {
			return fmt.Errorf("%s: %q is not true or false", c.Key, c.Value)
		}
		v = b
	default:
		return fmt.Errorf("%s can't be set from the command line - use hl edit", c.Key)
	}
	var val yaml.Node
	if err := val.Encode(v); err != nil {
		return err
	}
	if val.Kind == yaml.SequenceNode {
		val.Style = yaml.FlowStyle
	}
	set(target, key, &val)
	return nil
}

// set puts key: val into mapping m, keeping the old value's comments, or appends it.
func set(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			old := m.Content[i+1]
			val.HeadComment, val.LineComment, val.FootComment = old.HeadComment, old.LineComment, old.FootComment
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, val)
}

// remove deletes key from mapping m; a comment above it moves to the next key.
func remove(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			if hc := m.Content[i].HeadComment; hc != "" && i+2 < len(m.Content) && m.Content[i+2].HeadComment == "" {
				m.Content[i+2].HeadComment = hc
			}
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}
