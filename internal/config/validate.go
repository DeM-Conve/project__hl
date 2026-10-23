package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Problem is one thing wrong in hl.yml, with where it is and how to fix it.
type Problem struct {
	Line, Column int    // 1-based; 0 when the place isn't known
	Key          string // e.g. apps.my-app.port; "" for the whole file
	Message      string // what is wrong, then " - " and the fix
}

func (p Problem) String() string {
	var b strings.Builder
	if p.Line > 0 {
		fmt.Fprintf(&b, "%d:%d: ", p.Line, p.Column)
	}
	if p.Key != "" {
		b.WriteString(p.Key + ": ")
	}
	b.WriteString(p.Message)
	return b.String()
}

// In is the problem prefixed with the file it is in, as file:line:column: key: message.
func (p Problem) In(file string) string {
	if p.Line == 0 {
		return file + ": " + p.String()
	}
	return file + ":" + p.String()
}

// ProblemsError is a list of problems in one file.
type ProblemsError struct {
	File     string
	Problems []Problem
}

func (e *ProblemsError) Error() string {
	var b strings.Builder
	n := len(e.Problems)
	fmt.Fprintf(&b, "%s has %d problem%s:", e.File, n, map[bool]string{true: "", false: "s"}[n == 1])
	for _, p := range e.Problems {
		b.WriteString("\n  " + p.In(e.File))
	}
	return b.String()
}

// CheckFile checks <dir>/hl.yml; it returns a *ProblemsError when something is wrong.
func CheckFile(dir string) error {
	path := filepath.Join(dir, FileName)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return problemsErr(path, Check(b))
}

func problemsErr(path string, ps []Problem) error {
	if len(ps) == 0 {
		return nil
	}
	return &ProblemsError{File: path, Problems: ps}
}

// Check finds every problem in an hl.yml's content: syntax, unknown keys (with a suggestion),
// keys set twice, wrong value types, and the values hl can't work with. Known keys come from
// the Config type, so a field added there is accepted here without further changes.
func Check(b []byte) []Problem {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return []Problem{syntaxProblem(err)}
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil // empty: every default
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return []Problem{{Line: root.Line, Column: root.Column, Message: "expected settings (key: value) at the top - start from the README's example"}}
	}
	var ps []Problem
	walk(root, reflect.TypeOf(Config{}), "", "", &ps)
	if len(ps) == 0 {
		// Rules Validate knows that the walk doesn't (without a line number).
		var c Config
		if err := root.Decode(&c); err != nil {
			ps = append(ps, Problem{Message: err.Error()})
		} else {
			c.defaults()
			if err := c.Validate(); err != nil {
				ps = append(ps, Problem{Message: err.Error()})
			}
		}
	}
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].Line != ps[j].Line {
			return ps[i].Line < ps[j].Line
		}
		return ps[i].Column < ps[j].Column
	})
	return ps
}

var yamlLineRE = regexp.MustCompile(`^yaml: line (\d+): (.*)$`)

func syntaxProblem(err error) Problem {
	msg := err.Error()
	if m := yamlLineRE.FindStringSubmatch(msg); m != nil {
		n, _ := strconv.Atoi(m[1])
		return Problem{Line: n, Column: 1, Message: "not valid YAML: " + m[2] + " - check the indentation and the ':' after each key"}
	}
	return Problem{Message: "not valid YAML: " + strings.TrimPrefix(msg, "yaml: ")}
}

// rule checks one scalar value; it returns "" when the value is fine, else the problem and fix.
type rule func(v string) string

// rules are keyed by the setting's path with app names replaced by "*".
var rules = map[string]rule{
	"version": func(v string) string {
		n, _ := strconv.Atoi(v)
		if n > Version {
			return fmt.Sprintf("%d is newer than this hl understands (%d) - update hl", n, Version)
		}
		if n < 0 {
			return fmt.Sprintf("%d is not a version - use %d", n, Version)
		}
		return ""
	},
	"proxy.routing": oneOf(RoutingPath, RoutingHost),
	"proxy.port":    portRule,
	"proxy.project": func(v string) string {
		if !projectRE.MatchString(v) {
			return fmt.Sprintf("%q - use lowercase letters, digits, '_' and '-'", v)
		}
		return ""
	},
	"proxy.log_level": func(v string) string {
		for _, l := range ProxyLogLevels {
			if strings.EqualFold(v, l) {
				return ""
			}
		}
		return oneOf(ProxyLogLevels...)(strings.ToUpper(v))
	},
	"apps.*.routing": oneOf(RoutingPath, RoutingHost, RoutingNone),
	"apps.*.port":    portRule,
	"apps.*.prefix": func(v string) string {
		if v != "" && !prefixRE.MatchString(v) {
			return fmt.Sprintf("%q must look like /name, without a trailing slash - e.g. /my-app", v)
		}
		return ""
	},
}

// required are the keys a mapping at that path must have, with a hint for the value.
var required = map[string][]struct{ key, hint string }{
	"apps.*": {{"path", "the app's folder, e.g. path: ~/code/my-app"}},
}

func oneOf(vals ...string) rule {
	return func(v string) string {
		for _, ok := range vals {
			if v == ok {
				return ""
			}
		}
		msg := fmt.Sprintf("%q is not %s", v, orList(vals))
		if s := suggest(v, vals); s != "" {
			msg += fmt.Sprintf(" - did you mean %q?", s)
		} else {
			msg += " - use " + orList(vals)
		}
		return msg
	}
}

func orList(vals []string) string {
	if len(vals) == 1 {
		return vals[0]
	}
	return strings.Join(vals[:len(vals)-1], ", ") + " or " + vals[len(vals)-1]
}

func portRule(v string) string {
	n, _ := strconv.Atoi(v)
	if n < 1 || n > 65535 {
		return fmt.Sprintf("%s is not a port - use a number from 1 to 65535", v)
	}
	return ""
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// walk checks node against the Go type t. path is the key as the user reads it
// (apps.my-app.port), pattern the same with map keys replaced by "*" (apps.*.port).
func walk(n *yaml.Node, t reflect.Type, path, pattern string, ps *[]Problem) {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	add := func(at *yaml.Node, msg string) {
		*ps = append(*ps, Problem{Line: at.Line, Column: at.Column, Key: path, Message: msg})
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if n.Tag == "!!null" && n.Kind == yaml.ScalarNode {
		return // empty: the default
	}
	switch t.Kind() {
	case reflect.Struct:
		if n.Kind != yaml.MappingNode {
			add(n, "expected settings (key: value) here")
			return
		}
		fields := yamlFields(t)
		names := make([]string, 0, len(fields))
		for k := range fields {
			names = append(names, k)
		}
		sort.Strings(names)
		seen := map[string]int{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Tag == "!!merge" {
				continue
			}
			kp := join(path, k.Value)
			if line, dup := seen[k.Value]; dup {
				*ps = append(*ps, Problem{Line: k.Line, Column: k.Column, Key: kp, Message: fmt.Sprintf("set twice (also on line %d) - keep one", line)})
				continue
			}
			seen[k.Value] = k.Line
			ft, ok := fields[k.Value]
			if !ok {
				msg := "unknown key"
				if s := suggest(k.Value, names); s != "" {
					msg += fmt.Sprintf(" - did you mean %q?", s)
				} else {
					msg += " - known keys here: " + strings.Join(names, ", ")
				}
				*ps = append(*ps, Problem{Line: k.Line, Column: k.Column, Key: kp, Message: msg})
				continue
			}
			walk(v, ft, kp, join(pattern, k.Value), ps)
		}
		for _, r := range required[pattern] {
			if _, ok := seen[r.key]; !ok {
				add(n, r.key+" is required - "+r.hint)
			}
		}
	case reflect.Map:
		if n.Kind != yaml.MappingNode {
			add(n, "expected a list of key: value settings here")
			return
		}
		seen := map[string]int{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			kp := join(path, k.Value)
			if line, dup := seen[k.Value]; dup {
				*ps = append(*ps, Problem{Line: k.Line, Column: k.Column, Key: kp, Message: fmt.Sprintf("set twice (also on line %d) - keep one", line)})
				continue
			}
			seen[k.Value] = k.Line
			if pattern == "apps" {
				if err := ValidName(k.Value); err != nil {
					*ps = append(*ps, Problem{Line: k.Line, Column: k.Column, Key: kp, Message: strings.TrimPrefix(err.Error(), fmt.Sprintf("app name %q", k.Value)+": ")})
				}
			}
			if pattern == "apps" && v.Kind == yaml.ScalarNode && v.Tag == "!!null" {
				*ps = append(*ps, Problem{Line: k.Line, Column: k.Column, Key: kp, Message: "path is required - the app's folder, e.g. path: ~/code/my-app"})
				continue
			}
			walk(v, t.Elem(), kp, join(pattern, "*"), ps)
		}
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode {
			add(n, "expected a list here, e.g. [a.yml, b.yml]")
			return
		}
		for _, item := range n.Content {
			walk(item, t.Elem(), path, pattern, ps)
		}
	default:
		if n.Kind != yaml.ScalarNode {
			add(n, "expected a single value here, not a list or settings")
			return
		}
		switch t.Kind() {
		case reflect.Int, reflect.Int64, reflect.Int32:
			if n.Tag != "!!int" {
				add(n, fmt.Sprintf("%q is not a whole number", n.Value))
				return
			}
		case reflect.Bool:
			if n.Tag != "!!bool" {
				add(n, fmt.Sprintf("%q is not true or false", n.Value))
				return
			}
		}
		if r := rules[pattern]; r != nil {
			if msg := r(n.Value); msg != "" {
				add(n, msg)
			}
		}
	}
}

// yamlFields maps a struct's yaml keys to their types (inline structs flattened).
func yamlFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if strings.Contains(opts, "inline") {
			for k, v := range yamlFields(f.Type) {
				out[k] = v
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		out[name] = f.Type
	}
	return out
}

// suggest is the candidate closest to s, if it's close enough to be a likely typo.
func suggest(s string, candidates []string) string {
	best, bestD := "", -1
	for _, c := range candidates {
		d := distance(strings.ToLower(s), strings.ToLower(c))
		if bestD < 0 || d < bestD {
			best, bestD = c, d
		}
	}
	limit := 2
	if len(s) > 8 {
		limit = 3
	}
	if bestD >= 0 && (bestD <= limit || strings.HasPrefix(best, s) && len(s) >= 3) {
		return best
	}
	return ""
}

// distance is the Levenshtein edit distance between a and b.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
