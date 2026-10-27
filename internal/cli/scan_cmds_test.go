package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/config"
)

func TestScanSettingCommands(t *testing.T) {
	f := newFake(t)
	code := filepath.Join(f.home, "my code")
	more := filepath.Join(f.home, "more")
	for _, d := range []string{code, more} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lab := f.lab("lab", "# my homelab\nversion: 1\napps: {} # none yet\n")
	load := func() *config.Config {
		t.Helper()
		cfg, err := config.Load(lab)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}

	r := f.must("projects")
	contains(t, "empty list", r.out, "no projects in hl.yml - add one with hl projects add <folder>")

	r = f.must("projects", "add", code, "~/more")
	contains(t, "projects add", r.out, "added to projects: ~/my code, ~/more", "run hl sync")
	if got := strings.Join(load().Projects, "|"); got != "~/my code|~/more" {
		t.Errorf("projects = %q", got)
	}
	r = f.must("projects", "add", "~/my code")
	contains(t, "again", r.out, "~/my code is already in projects")
	r = f.hl("projects", "add", filepath.Join(f.home, "nope"))
	if r.code != 1 || !strings.Contains(r.errOut, "not found") {
		t.Errorf("missing folder: %s", r)
	}

	if err := os.RemoveAll(more); err != nil {
		t.Fatal(err)
	}
	r = f.must("projects", "list")
	contains(t, "list", r.out, "~/my code\n", "~/more  (not found: "+more+")")

	// By the absolute path too.
	r = f.must("projects", "remove", more)
	contains(t, "remove", r.out, "removed from projects: ~/more", "its apps stay")
	r = f.hl("projects", "rm", "~/elsewhere")
	if r.code != 1 || !strings.Contains(r.errOut, "~/elsewhere is not in projects") || !strings.Contains(r.errOut, "projects: ~/my code") {
		t.Errorf("remove unknown: %s", r)
	}
	if r := f.hl("projects", "add"); r.code != 2 {
		t.Errorf("add without folders: %s", r)
	}

	f.must("ignore", "add", "old", "scratch-*")
	r = f.hl("ignore", "add", "[")
	if r.code != 1 || !strings.Contains(r.errOut, "not a valid pattern") {
		t.Errorf("bad pattern: %s", r)
	}
	r = f.must("ignore")
	contains(t, "ignore", r.out, "old\nscratch-*\n", "always: .*, node_modules")
	f.must("ignore", "remove", "old")

	f.must("strip-prefix", "add", "project__")
	r = f.must("strip-prefix", "ls")
	contains(t, "strip-prefix", r.out, "project__")

	cfg := load()
	if strings.Join(cfg.Ignore, ",") != "scratch-*" || strings.Join(cfg.StripPrefix, ",") != "project__" {
		t.Errorf("ignore = %q, strip_prefix = %q", cfg.Ignore, cfg.StripPrefix)
	}
	text := f.read(filepath.Join(lab, config.FileName))
	contains(t, "hl.yml", text, "# my homelab", "# none yet")

	// Removing the last entry drops the key.
	f.must("strip-prefix", "remove", "project__")
	lacks(t, "hl.yml", f.read(filepath.Join(lab, config.FileName)), "strip_prefix")
}
