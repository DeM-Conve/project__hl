package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "with space")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "hl.yml")
	if err := WriteFile(f, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(f, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	st, _ := os.Stat(f)
	if string(b) != "two" || st.Mode().Perm() != 0o600 {
		t.Fatalf("got %q mode %v, want two 0600 (existing mode kept)", b, st.Mode().Perm())
	}
	// A symlink stays a symlink; its target gets the new content.
	link := filepath.Join(dir, "link.yml")
	if err := os.Symlink(f, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	if err := WriteFile(link, []byte("three"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink replaced by a file")
	}
	if b, _ := os.ReadFile(f); string(b) != "three" {
		t.Errorf("target = %q", b)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("temporary files left: %v", entries)
	}
	if err := WriteFile(filepath.Join(dir, "missing", "x"), nil, 0o644); err == nil {
		t.Error("expected an error for a missing folder")
	}
}
