package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
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
	return out.Bytes(), nil
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
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return os.WriteFile(path, b, mode)
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
	case from == Version:
		return from, nil
	case from > Version:
		return from, tooNew(from)
	case from < 0:
		return from, fmt.Errorf("%s: version: must be a whole number (this hl writes version %d)", path, Version)
	}
	return from, editFile(dir, func(root *yaml.Node) error {
		if v := lookup(root, "version"); v != nil {
			// `version:` with no value.
			*v = yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(Version), LineComment: v.LineComment}
			return nil
		}
		key := &yaml.Node{Kind: yaml.ScalarNode, Value: "version"}
		val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(Version)}
		root.Content = append([]*yaml.Node{key, val}, root.Content...)
		return nil
	})
}

func tooNew(v int) error {
	return fmt.Errorf("%s is version %d, but this hl reads up to version %d - update hl", FileName, v, Version)
}
