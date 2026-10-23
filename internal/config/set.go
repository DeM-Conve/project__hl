package config

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Change is one edit to an app's settings: Key=Value, or with Unset the key removed.
// Keys are App's hl.yml keys (port, routing, ...); a map setting takes a name: env.TZ.
type Change struct {
	Key, Value string
	Unset      bool
}

// AppKeys are the keys `hl set` accepts, sorted; map settings end in "." (env.).
func AppKeys() []string {
	var keys []string
	for k, t := range yamlFields(reflect.TypeOf(App{})) {
		if t.Kind() == reflect.Map {
			k += "."
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// EditApp applies changes to one app in <dir>/hl.yml, keeping the file's comments and order.
// The result is checked first: if it has any problem, hl.yml is left as it was and the error
// lists them.
func EditApp(dir, name string, changes []Change) error {
	path := filepath.Join(dir, FileName)
	out, err := edited(path, func(root *yaml.Node) error {
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
				return fmt.Errorf("no app %q in %s - did you mean %q?", name, FileName, s)
			}
			return fmt.Errorf("no app %q in %s (apps: %s)", name, FileName, strings.Join(names, ", "))
		}
		if app.Kind != yaml.MappingNode {
			*app = yaml.Node{Kind: yaml.MappingNode, LineComment: app.LineComment, HeadComment: app.HeadComment}
		}
		fields := yamlFields(reflect.TypeOf(App{}))
		for _, c := range changes {
			if err := applyChange(app, fields, c); err != nil {
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
		return fmt.Errorf("%s - use one of: %s", msg, strings.Join(AppKeys(), ", "))
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
				m = &yaml.Node{Kind: yaml.MappingNode}
				app.Content = append(app.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: field}, m)
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
