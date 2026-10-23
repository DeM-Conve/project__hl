// Package fsutil writes files atomically, so a crash or Ctrl-C never leaves
// hl.yml or a generated file half written.
package fsutil

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to a temporary file next to path and renames it over
// path. An existing file keeps its permissions; a symlink keeps pointing where
// it did and its target is replaced instead.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // no-op once renamed
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
