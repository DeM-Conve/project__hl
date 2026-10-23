package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTemplateIsCurrent(t *testing.T) {
	if v := FileVersion([]byte(Template)); v != Version {
		t.Errorf("Template has version %d, want %d", v, Version)
	}
	if !strings.HasPrefix(Template, SchemaComment+"\n") {
		t.Errorf("Template should start with the schema comment:\n%s", Template)
	}
}

func TestUpgradeAddsVersionKeepingComments(t *testing.T) {
	old := `# my homelab

proxy:
  # only this machine
  bind: 127.0.0.1  # keep me
apps:
  my-app: {path: ~/code/my-app}  # the app
`
	dir := write(t, old)
	from, err := Upgrade(dir)
	if err != nil || from != 0 {
		t.Fatalf("Upgrade = %d, %v", from, err)
	}
	got := read(t, dir)
	for _, want := range []string{"# my homelab", "# only this machine", "# keep me", "# the app", "version: 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q after upgrade:\n%s", want, got)
		}
	}
	if strings.Index(got, "# my homelab") > strings.Index(got, "version: 1") {
		t.Errorf("the file's header comment should stay on top:\n%s", got)
	}
	c, err := Load(dir)
	if err != nil || c.Version != Version || c.Apps["my-app"] == nil {
		t.Fatalf("Load after upgrade: %+v, %v", c, err)
	}
	// Already current: untouched.
	before := read(t, dir)
	if from, err := Upgrade(dir); err != nil || from != Version {
		t.Fatalf("second Upgrade = %d, %v", from, err)
	}
	if read(t, dir) != before {
		t.Error("a current file was rewritten")
	}
}

func TestUpgradeEmptyVersionAndTooNew(t *testing.T) {
	dir := write(t, "version:\napps: {}\n")
	if _, err := Upgrade(dir); err != nil {
		t.Fatal(err)
	}
	if v := FileVersion([]byte(read(t, dir))); v != Version {
		t.Errorf("version = %d", v)
	}
	dir = write(t, "version: 99\n")
	if _, err := Upgrade(dir); err == nil || !strings.Contains(err.Error(), "update hl") {
		t.Errorf("Upgrade of a newer file: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("Load of a newer file should fail")
	}
}

func TestOldFileStillLoads(t *testing.T) {
	c, err := Load(write(t, "apps:\n  my-app: {path: ~/code/my-app}\n"))
	if err != nil || c.Version != 0 {
		t.Fatalf("%+v, %v", c, err)
	}
}
