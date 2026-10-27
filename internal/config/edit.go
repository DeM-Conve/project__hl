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
// sections keep their spacing. If the result doesn't decode to the same YAML as new (a blank
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
	// lcs[i][j]: the length of the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
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
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			pending = max(pending, before[i])
			emit(b[j])
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
			// A line new doesn't have: its blank lines go to the next one.
			pending = max(pending, before[i])
			i++
		default:
			emit(b[j])
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

// Replace writes b as <dir>/hl.yml, keeping the file's permissions, if b has no problems;
// otherwise it returns a *ProblemsError and leaves hl.yml as it was.
func Replace(dir string, b []byte) error {
	path := filepath.Join(dir, FileName)
	if err := problemsErr(path, Check(b)); err != nil {
		return err
	}
	return writeKeepMode(path, b)
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
