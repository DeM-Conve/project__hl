package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/fsutil"
)

// edited applies edit to hl.yml's node tree (the top-level mapping) and returns the new file
// content, keeping comments and key order. Nothing is written.
func edited(path string, edit func(root *yaml.Node) error) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind == 0 {
		// An empty file.
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: expected a mapping at the top", path)
	}
	if err := edit(doc.Content[0]); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	return keepBlankLines(b, out.Bytes()), nil
}

// keepBlankLines puts the blank lines of old (the file before an edit) back into new (the
// encoder's output, which drops them). Each blank line belongs to the line after it: new's
// lines are matched against old's other lines, and a line still there gets its blank lines
// back. A changed or removed line passes its blank lines on to the next line written, so
// sections keep their spacing. A blank line of new's own counts as one blank line before the
// next line, merged with old's there, so an edit never adds blank lines. If the result doesn't decode to the same YAML as new (a blank
// line inside a block scalar, say), new is returned as it is.
func keepBlankLines(old, new []byte) []byte {
	blank := func(s string) bool { return strings.TrimSpace(s) == "" }
	// a: old's other lines; before[i]: the blank lines above a[i].
	var a []string
	var before []int
	n := 0
	for _, l := range splitLines(old) {
		if blank(l) {
			n++
			continue
		}
		a, before, n = append(a, l), append(before, n), 0
	}
	b := splitLines(new)
	// Lines are compared without the spaces before a comment, which the encoder makes one.
	na := make([]string, len(a))
	for k, l := range a {
		na[k] = normComment(l)
	}
	nb := make([]string, len(b))
	for k, l := range b {
		nb[k] = normComment(l)
	}
	// lcs[i][j]: the length of the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if na[i] == nb[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []string
	pending := 0 // blank lines to write before the next line
	emit := func(l string) {
		if len(out) > 0 {
			for ; pending > 0; pending-- {
				out = append(out, "")
			}
		}
		pending = 0
		out = append(out, l)
	}
	// topBlank is the blank lines between old's top-level lines: 1 when it has any.
	topBlank := 0
	for k := 1; k < len(a); k++ {
		if before[k] > 0 && isTopLine(a[k]) {
			topBlank = 1
		}
	}
	var gone []string // old's lines left out since the last line kept
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && na[i] == nb[j]:
			pending = max(pending, before[i])
			emit(a[i]) // old's line: the same but for the spaces before its comment
			i, j, gone = i+1, j+1, nil
		case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
			// A line new doesn't have: its blank lines go to the next one.
			pending = max(pending, before[i])
			gone = append(gone, a[i])
			i++
		case blank(b[j]):
			// The encoder's own blank line (after the document's head comment, or in a block
			// scalar): it merges with old's blank lines there instead of adding one.
			pending = max(pending, 1)
			j++
		default:
			if isTopLine(b[j]) && !replaces(b[j], gone) && (len(out) == 0 || !isTopComment(out[len(out)-1])) {
				// A top-level key (or its comment) hl added: spaced like the key after it, or,
				// at the end, like the file's other top-level keys.
				if i < len(a) {
					pending = max(pending, before[i])
				} else {
					pending = max(pending, topBlank)
				}
			}
			emit(commentAt(b[j], gone))
			j++
		}
	}
	res := []byte(strings.Join(out, "\n") + "\n")
	var x, y any
	if yaml.Unmarshal(res, &x) != nil || yaml.Unmarshal(new, &y) != nil || !reflect.DeepEqual(x, y) {
		return new
	}
	return res
}

// isTopLine reports whether l is a top-level line of hl.yml: a key or a comment, not indented.
func isTopLine(l string) bool { return l != "" && l[0] != ' ' && l[0] != '-' && l[0] != '\t' }

// replaces reports whether key line l takes the place of one of gone: the same key.
func replaces(l string, gone []string) bool {
	key, _, ok := strings.Cut(l, ":")
	if !ok || isTopComment(l) {
		return false
	}
	for _, g := range gone {
		if gkey, _, ok := strings.Cut(g, ":"); ok && gkey == key {
			return true
		}
	}
	return false
}

// isTopComment reports whether l is a comment line that isn't indented.
func isTopComment(l string) bool { return strings.HasPrefix(l, "#") }

// splitComment splits l at the comment after its value (" # ..."): the part before it,
// without the spaces in between, and the comment. ok is false when l has no such comment
// (a line that is only a comment has none either).
func splitComment(l string) (code, comment string, ok bool) {
	if strings.HasPrefix(strings.TrimSpace(l), "#") {
		return l, "", false
	}
	var quote byte
	for k := 0; k < len(l); k++ {
		c := l[k]
		switch {
		case quote == '"' && c == '\\':
			k++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && k > 0 && (l[k-1] == ' ' || l[k-1] == '\t'):
			return strings.TrimRight(l[:k], " \t"), l[k:], true
		}
	}
	return l, "", false
}

// normComment is l with one space before its comment, as the encoder writes it.
func normComment(l string) string {
	if code, comment, ok := splitComment(l); ok {
		return code + " " + comment
	}
	return l
}

// commentAt is l (a line the encoder wrote, with one space before its comment) with its
// comment at the column it had in the line it replaces: the line of gone (old's lines left
// out just before) for the same key with the same comment, unless that one had a single space
// before its comment too. Otherwise l as it is.
func commentAt(l string, gone []string) string {
	code, comment, ok := splitComment(l)
	if !ok {
		return l
	}
	key, _, _ := strings.Cut(code, ":")
	for _, g := range gone {
		gc, gcomment, ok := splitComment(g)
		gkey, _, _ := strings.Cut(gc, ":")
		if !ok || gkey != key || gcomment != comment {
			continue
		}
		if len(g)-len(gcomment)-len(gc) == 1 {
			return l // one space before it, as the encoder writes it
		}
		col := len(g) - len(gcomment) // where the comment started: aligned comments stay aligned
		return code + strings.Repeat(" ", max(1, col-len(code))) + comment
	}
	return l
}

// splitLines splits b into lines, without their line ends.
func splitLines(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// editFile applies edit to <dir>/hl.yml and writes it back.
func editFile(dir string, edit func(root *yaml.Node) error) error {
	path := filepath.Join(dir, FileName)
	out, err := edited(path, edit)
	if err != nil {
		return err
	}
	return writeKeepMode(path, out)
}

// writeKeepMode replaces path's content, keeping its permissions.
func writeKeepMode(path string, b []byte) error {
	// Atomic, keeping an existing file's permissions (and a symlink).
	return fsutil.WriteFile(path, b, defaults.FileMode)
}

// lookup returns the value node of key in mapping m, or nil.
func lookup(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// FileVersion is the `version:` of an hl.yml's content: 0 when it has none (the format before
// versions), -1 when it isn't a whole number or the file doesn't parse.
func FileVersion(b []byte) int {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return -1
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return 0
	}
	v := lookup(doc.Content[0], "version")
	if v == nil || v.Tag == "!!null" {
		return 0
	}
	n, err := strconv.Atoi(v.Value)
	if err != nil {
		return -1
	}
	return n
}

// Upgrade brings <dir>/hl.yml to the current format and returns the version it had. Today that
// only means adding `version: 1` at the top of an unversioned file; nothing else changes. A
// file that is already current is left untouched.
func Upgrade(dir string) (from int, err error) {
	path := filepath.Join(dir, FileName)
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	from = FileVersion(b)
	switch {
	case from == defaults.ConfigVersion:
		return from, nil
	case from > defaults.ConfigVersion:
		return from, tooNew(from)
	case from < 0:
		return from, fmt.Errorf("%s: version: must be a whole number (this hl writes version %d)", path, defaults.ConfigVersion)
	}
	return from, editFile(dir, func(root *yaml.Node) error {
		if v := lookup(root, "version"); v != nil {
			// `version:` with no value.
			*v = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(defaults.ConfigVersion), LineComment: v.LineComment}
			return nil
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Value: "version"}
		val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(defaults.ConfigVersion)}
		root.Content = append([]*yaml.Node{key, val}, root.Content...)
		return nil
	})
}

func tooNew(v int) error {
	return fmt.Errorf("%s is version %d, but this hl reads up to version %d - update hl", FileName, v, defaults.ConfigVersion)
}
