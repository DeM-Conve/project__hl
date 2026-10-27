package config

import (
	"errors"
	"fmt"
	"os"
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
// A list setting takes Op: "" replaces it with Value (comma-separated), OpAdd adds the
// entries of Value that aren't in it yet, OpRemove removes them (each must be in it).
type Change struct {
	Key, Value string
	Unset      bool
	Op         string
}

// List operations of a Change: hl set ignore+=old, hl set ignore-=old.
const (
	OpAdd    = "+"
	OpRemove = "-"
)

// The scopes `hl set`/`hl unset` change besides an app: the settings shared by every app
// (HomelabScope) and the proxy section. ProxyScope is the reserved app name, so no app has it.
const (
	HomelabScope = ""
	ProxyScope   = defaults.ReservedAppName
)

// homelabKeys are the top-level settings `hl set` changes without an app: every one but proxy,
// apps and version (which hl manages).
var homelabKeys = []string{"env", "name", "projects", "ignore", "scan_depth", "strip_prefix"}

// AppKeys are the keys `hl set <app>` accepts, sorted; map settings end in "." (env.).
func AppKeys() []string { return keysOf(appFields()) }

// ProxyKeys are the keys `hl set proxy` accepts, sorted.
func ProxyKeys() []string { return keysOf(proxyFields()) }

// HomelabKeys are the keys `hl set` accepts without an app (the shared env., name, the scan
// settings), sorted.
func HomelabKeys() []string { return keysOf(homelabFields()) }

// IsHomelabKey reports whether key (env, env.TZ, ignore, ...) is a homelab-wide setting.
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

// UnknownError is a setting or app `hl set`/`hl unset` doesn't know: a usage mistake.
type UnknownError struct{ msg string }

func (e *UnknownError) Error() string { return e.msg }

func unknown(format string, a ...any) error { return &UnknownError{msg: fmt.Sprintf(format, a...)} }

// Edit applies changes to scope in <dir>/hl.yml - HomelabScope (top-level settings), ProxyScope
// (proxy:) or an app's name - keeping the file's comments and order. The result is checked
// first: if it has any problem, hl.yml is left as it was and the error lists them.
func Edit(dir, scope string, changes []Change) error {
	switch scope {
	case HomelabScope:
		for _, c := range changes {
			if field, _, _ := strings.Cut(c.Key, "."); managedKeys[field] {
				return unknown("unknown setting %q - use one of: %s", c.Key, strings.Join(HomelabKeys(), ", "))
			}
		}
		return editScope(dir, "", changes, homelabFields(), func(root *yaml.Node) (*yaml.Node, error) { return root, nil })
	case ProxyScope:
		fields := proxyFields()
		for k := range removedKeys {
			if f, ok := strings.CutPrefix(k, "proxy."); ok {
				fields[f] = reflect.TypeOf("") // only so hl unset can remove it; Check refuses it
			}
		}
		return editScope(dir, "proxy.", changes, fields, func(root *yaml.Node) (*yaml.Node, error) {
			return section(root, "proxy"), nil
		})
	}
	return EditApp(dir, scope, changes)
}

// EditApp is Edit for one app.
func EditApp(dir, name string, changes []Change) error {
	fields := appFields()
	for _, f := range removedAppKeys() {
		fields[f] = reflect.TypeOf("") // only so hl unset can remove it; Check refuses it
	}
	return editScope(dir, "apps."+name+".", changes, fields, func(root *yaml.Node) (*yaml.Node, error) {
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
				return nil, unknown("no app %q in %s - did you mean %q?", name, FileName, s)
			}
			return nil, unknown("no app %q in %s (apps: %s)", name, FileName, strings.Join(names, ", "))
		}
		return app, nil
	})
}

// ApplyApp applies changes to a the way `hl set <app>` applies them to hl.yml: the same keys,
// types and list operations (what `hl add <folder> key=value...` takes).
func ApplyApp(a *App, changes []Change) error {
	var n yaml.Node
	if err := n.Encode(a); err != nil {
		return err
	}
	for _, c := range changes {
		if err := applyChange(&n, appFields(), c); err != nil {
			return err
		}
	}
	return n.Decode(a)
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
// prefix is the scope's part of a full key ("apps.my-app.", "proxy.", "").
func editScope(dir, prefix string, changes []Change, fields map[string]reflect.Type, find func(root *yaml.Node) (*yaml.Node, error)) error {
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
		if len(target.Content) == 0 && target == lookup(root, "proxy") {
			remove(root, "proxy") // the last proxy setting is gone: no `proxy: {}` left behind
		}
		return nil
	})
	var ve *ValueError
	if errors.As(err, &ve) {
		for i := range ve.Problems {
			ve.Problems[i].Key = prefix + ve.Problems[i].Key
		}
		return ve
	}
	if err != nil {
		return err
	}
	if ps := Check(out); len(ps) > 0 {
		orig, err := os.ReadFile(path)
		if err == nil && allUnset(changes) && onlyOld(ps, Check(orig)) {
			// hl unset on a broken hl.yml: one problem at a time may go, as long as none is added.
			return writeKeepMode(path, out)
		}
		if err == nil && len(Check(orig)) == 0 {
			// hl.yml was fine: the values given are wrong, not a place in the file.
			for i := range ps {
				ps[i].Line, ps[i].Column = 0, 0
			}
			return &ValueError{Problems: ps}
		}
		return fmt.Errorf("%s not changed: %w", FileName, &ProblemsError{File: path, Problems: ps})
	}
	return writeKeepMode(path, out)
}

func allUnset(changes []Change) bool {
	for _, c := range changes {
		if !c.Unset {
			return false
		}
	}
	return true
}

// onlyOld reports whether every problem in ps (after an edit) was in old (before it), by key
// and message: lines may have moved.
func onlyOld(ps, old []Problem) bool {
	n := map[string]int{}
	for _, p := range old {
		n[p.Key+"\x00"+p.Message]++
	}
	for _, p := range ps {
		k := p.Key + "\x00" + p.Message
		if n[k] == 0 {
			return false
		}
		n[k]--
	}
	return true
}

// ValueError is a value `hl set`/`hl unset` can't use (a usage mistake): each problem's key is
// the full one (apps.my-app.routing), with no place in hl.yml, since hl.yml wasn't written.
type ValueError struct{ Problems []Problem }

func (e *ValueError) Error() string {
	var lines []string
	for _, p := range e.Problems {
		what, _, _ := strings.Cut(p.Message, " - ")
		if p.Key != "" {
			what = p.Key + ": " + what
		}
		lines = append(lines, what)
	}
	return strings.Join(lines, "\n")
}

// Hint is what to do about it: the fix each problem names ("did you mean ...", "use ..."),
// "" when none does.
func (e *ValueError) Hint() string {
	var fixes []string
	for _, p := range e.Problems {
		if _, fix, ok := strings.Cut(p.Message, " - "); ok {
			fixes = append(fixes, fix)
		}
	}
	return strings.Join(fixes, "; ")
}

// valueErr is a *ValueError for one key: what is wrong, and (optional) the fix.
func valueErr(key, what, fix string) error {
	if fix != "" {
		what += " - " + fix
	}
	return &ValueError{Problems: []Problem{{Key: key, Message: what}}}
}

func applyChange(app *yaml.Node, fields map[string]reflect.Type, c Change) error {
	field, sub, hasSub := strings.Cut(c.Key, ".")
	t, ok := fields[field]
	if !ok && c.Unset && present(app, c.Key) {
		// Not a setting, but in the file (a typo, or a key of another hl version): hl unset
		// removes it, so a file Check refuses can be fixed without an editor.
		if !hasSub {
			remove(app, field)
			return nil
		}
		m := lookup(app, field)
		remove(m, sub)
		if len(m.Content) == 0 {
			remove(app, field)
		}
		return nil
	}
	if !ok {
		names := make([]string, 0, len(fields))
		for k := range fields {
			names = append(names, k)
		}
		sort.Strings(names)
		msg := fmt.Sprintf("unknown setting %q", c.Key)
		if s := suggest(field, names); s != "" {
			return unknown("%s - did you mean %q?", msg, s)
		}
		return unknown("%s - use one of: %s", msg, strings.Join(keysOf(fields), ", "))
	}
	isMap := t.Kind() == reflect.Map
	if c.Op != "" && t.Kind() != reflect.Slice {
		return valueErr(c.Key, "is not a list", fmt.Sprintf("use %s=value", c.Key))
	}
	switch {
	case isMap && !hasSub && !c.Unset:
		return valueErr(field, "needs a name", fmt.Sprintf("%s.NAME=value", field))
	case !isMap && hasSub:
		return valueErr(field, "has no parts", fmt.Sprintf("use %s=value", field))
	case isMap && hasSub && sub == "":
		return valueErr(field, "needs a name", fmt.Sprintf("%s.NAME", field))
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
			return valueErr(c.Key, fmt.Sprintf("%q is not a whole number", c.Value), "")
		}
		v = n
	case reflect.Slice:
		var items []string
		for _, s := range strings.Split(c.Value, ",") {
			if s = strings.TrimSpace(s); s != "" {
				items = append(items, s)
			}
		}
		if c.Op != "" {
			var err error
			if items, err = listOp(lookup(target, key), c, items); err != nil {
				return err
			}
			if len(items) == 0 {
				remove(target, key) // nothing left in the list
				return nil
			}
		}
		v = items
	case reflect.String:
		v = c.Value
	case reflect.Bool:
		b, err := strconv.ParseBool(c.Value)
		if err != nil {
			return valueErr(c.Key, fmt.Sprintf("%q is not true or false", c.Value), "")
		}
		v = b
	default:
		return fmt.Errorf("%s can't be set with hl set", c.Key)
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

// listOp is the list old (a sequence node, or nil) with items added (OpAdd: the ones not in
// it yet) or removed (OpRemove: each must be in it).
func listOp(old *yaml.Node, c Change, items []string) ([]string, error) {
	var have []string
	if old != nil && old.Kind == yaml.SequenceNode {
		for _, n := range old.Content {
			have = append(have, n.Value)
		}
	}
	in := func(list []string, s string) bool {
		for _, x := range list {
			if x == s {
				return true
			}
		}
		return false
	}
	switch c.Op {
	case OpAdd:
		for _, it := range items {
			if !in(have, it) {
				have = append(have, it)
			}
		}
		return have, nil
	case OpRemove:
		for _, it := range items {
			if !in(have, it) {
				if len(have) == 0 {
					return nil, valueErr(c.Key, fmt.Sprintf("%q is not in it (it is empty)", it), "")
				}
				return nil, valueErr(c.Key, fmt.Sprintf("%q is not in it (%s)", it, strings.Join(have, ", ")), "")
			}
		}
		var kept []string
		for _, h := range have {
			if !in(items, h) {
				kept = append(kept, h)
			}
		}
		return kept, nil
	}
	return nil, fmt.Errorf("%s: unknown list operation %q", c.Key, c.Op)
}

// set puts key: val into mapping m, keeping the old value's comments, or adds it: before
// apps: in the top-level mapping, so settings stay above the apps, else at the end.
func set(m *yaml.Node, key string, val *yaml.Node) {
	at := len(m.Content)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			old := m.Content[i+1]
			val.HeadComment, val.LineComment, val.FootComment = old.HeadComment, old.LineComment, old.FootComment
			m.Content[i+1] = val
			return
		}
		if m.Content[i].Value == "apps" && m.Content[i+1].Kind == yaml.MappingNode {
			at = i
		}
	}
	kv := []*yaml.Node{{Kind: yaml.ScalarNode, Value: key}, val}
	m.Content = append(m.Content[:at], append(kv, m.Content[at:]...)...)
}

// remove deletes key from mapping m; a comment above it moves to the next key.
// present reports whether mapping m has key (field, or field.sub with field a mapping).
func present(m *yaml.Node, key string) bool {
	field, sub, hasSub := strings.Cut(key, ".")
	v := lookup(m, field)
	if !hasSub || v == nil {
		return v != nil
	}
	return v.Kind == yaml.MappingNode && sub != "" && lookup(v, sub) != nil
}

// managedKeys are top-level keys hl.yml has that aren't homelab settings: hl unset never
// removes them as a whole.
var managedKeys = map[string]bool{"version": true, ProxyScope: true, "apps": true}

// Present reports whether <dir>/hl.yml has key (port, env.TZ, ...) in scope (as for Edit),
// known setting or not. The keys hl manages at the top (version, proxy, apps) are not the
// homelab scope's.
func Present(dir, scope, key string) bool {
	found := false
	_, _ = edited(filepath.Join(dir, FileName), func(root *yaml.Node) error {
		m := root
		switch scope {
		case HomelabScope:
			if field, _, _ := strings.Cut(key, "."); managedKeys[field] {
				return nil
			}
		case ProxyScope:
			m = lookup(root, "proxy")
		default:
			if apps := lookup(root, "apps"); apps != nil && apps.Kind == yaml.MappingNode {
				m = lookup(apps, scope)
			} else {
				m = nil
			}
		}
		found = m != nil && m.Kind == yaml.MappingNode && present(m, key)
		return nil
	})
	return found
}

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
