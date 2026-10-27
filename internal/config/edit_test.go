package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/defaults"
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
	if v := FileVersion([]byte(Template)); v != defaults.ConfigVersion {
		t.Errorf("Template has version %d, want %d", v, defaults.ConfigVersion)
	}
	if strings.Contains(Template, "yaml-language-server") {
		t.Errorf("Template should not point editors at the schema (nobody opens hl.yml):\n%s", Template)
	}
	if ps := Check([]byte(Template)); len(ps) > 0 {
		t.Errorf("Template has problems: %v", ps)
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
	if err != nil || c.Version != defaults.ConfigVersion || c.Apps["my-app"] == nil {
		t.Fatalf("Load after upgrade: %+v, %v", c, err)
	}
	// Already current: untouched.
	before := read(t, dir)
	if from, err := Upgrade(dir); err != nil || from != defaults.ConfigVersion {
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
	if v := FileVersion([]byte(read(t, dir))); v != defaults.ConfigVersion {
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

// edit is Edit of one change that fails the test on an error.
func edit(t *testing.T, dir, scope string, c Change) {
	t.Helper()
	if err := Edit(dir, scope, []Change{c}); err != nil {
		t.Fatalf("Edit(%q, %+v): %v", scope, c, err)
	}
}

// The layout promise: on a file hl init wrote, unsetting any top-level setting and setting it
// back (or setting one and unsetting it) gives the file back byte for byte - the key's comment
// goes and comes back with it, and it returns to its place with its blank lines.
func TestUnsetSetRoundTripOnInitFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteNew(dir, InitSettings{Name: "lab2", Projects: []string{"~/code"}, StripPrefix: []string{"project__"}, Port: 8080}); err != nil {
		t.Fatal(err)
	}
	if err := AddApp(dir, "blog", &App{Path: "~/code/project__blog", Service: "web", Port: 8081}); err != nil {
		t.Fatal(err)
	}
	orig := read(t, dir)
	for _, c := range []struct{ scope, key, value string }{
		{"", "name", "lab2"},
		{"", "projects", "~/code"},
		{"", "strip_prefix", "project__"},
		{ProxyScope, "port", "8080"},
	} {
		edit(t, dir, c.scope, Change{Key: c.key, Unset: true})
		unset := read(t, dir)
		if strings.Contains(unset, "\n"+c.key+":") || strings.Contains(unset, "\n\n\n") {
			t.Errorf("after hl unset %s %s:\n%s", c.scope, c.key, unset)
		}
		edit(t, dir, c.scope, Change{Key: c.key, Value: c.value})
		if got := read(t, dir); got != orig {
			t.Errorf("hl unset then hl set %s %s changed the file:\n--- want\n%s--- got\n%s", c.scope, c.key, orig, got)
		}
	}
	for _, c := range []struct{ scope, key, value string }{
		{"", "ignore", "old"},
		{"", "env.TZ", "UTC"},
		{ProxyScope, "debug", "true"},
		{ProxyScope, "subnet", "172.31.0.0/16"},
		{"blog", "routing", "host"},
		{"blog", "env.BLOG_URL", "${HL_URL}"},
	} {
		edit(t, dir, c.scope, Change{Key: c.key, Value: c.value})
		if got := read(t, dir); strings.Contains(got, "\n\n\n") || !strings.Contains(got, "\n\napps:") {
			t.Errorf("hl set %s %s: blank lines off:\n%s", c.scope, c.key, got)
		}
		edit(t, dir, c.scope, Change{Key: c.key, Unset: true})
		if got := read(t, dir); got != orig {
			t.Errorf("hl set then hl unset %s %s changed the file:\n--- want\n%s--- got\n%s", c.scope, c.key, orig, got)
		}
	}
}

// A top-level key hl adds goes in its place among the others, one blank line apart, with
// init's comment when it has one.
func TestSetPutsTopLevelKeyInPlace(t *testing.T) {
	dir := write(t, "version: 1\n\nproxy:\n  port: 8080\n\napps: {}\n")
	edit(t, dir, "", Change{Key: "strip_prefix", Value: "project__"})
	edit(t, dir, "", Change{Key: "env.TZ", Value: "UTC"})
	want := "version: 1\n\n" +
		"# Left out of app names: folder project__blog becomes app blog.\nstrip_prefix: [project__]\n\n" +
		"proxy:\n  port: 8080\n\n" +
		"env:\n  TZ: UTC\n\n" +
		"apps: {}\n"
	if got := read(t, dir); got != want {
		t.Errorf("--- want\n%s--- got\n%s", want, got)
	}
}

// On a file commented by hand: a key's own comment (right above it) goes with it, a comment a
// blank line further up stays, inline comments keep their spacing, and a key set back comes
// with init's comment (if it has one), not the user's.
func TestUnsetSetOnHandCommentedFile(t *testing.T) {
	orig := `# my homelab

version: 1

# --- where my apps are ---

# the main one
projects: [~/code]   # all of them
ignore: [old]

proxy:
  # only this machine
  bind: 127.0.0.1    # keep
  port: 8081         # moved off 80

apps:
  blog:
    path: ~/code/blog   # the blog
    port: 8080          # its port
`
	dir := write(t, orig)
	edit(t, dir, "", Change{Key: "ignore", Value: "old,tmp"})
	edit(t, dir, ProxyScope, Change{Key: "port", Value: "9090"})
	edit(t, dir, "blog", Change{Key: "port", Value: "3000"})
	want := strings.NewReplacer("ignore: [old]", "ignore: [old, tmp]",
		"port: 8081         # moved off 80", "port: 9090         # moved off 80",
		"port: 8080          # its port", "port: 3000          # its port").Replace(orig)
	if got := read(t, dir); got != want {
		t.Errorf("hl set changed more than the values:\n--- want\n%s--- got\n%s", want, got)
	}

	edit(t, dir, "", Change{Key: "projects", Unset: true})
	want = `# my homelab

version: 1

# --- where my apps are ---

ignore: [old, tmp]

proxy:
  # only this machine
  bind: 127.0.0.1    # keep
  port: 9090         # moved off 80

apps:
  blog:
    path: ~/code/blog   # the blog
    port: 3000          # its port
`
	if got := read(t, dir); got != want {
		t.Errorf("hl unset projects:\n--- want\n%s--- got\n%s", want, got)
	}
	edit(t, dir, "", Change{Key: "projects", Value: "~/code"})
	// The section comment stays with the key it now heads; projects goes in its place above it.
	want = strings.Replace(want, "# --- where my apps are ---\n\nignore",
		"# Folders with your projects: each subfolder with a compose file is an app.\n"+
			"# `hl sync` adds new ones and removes apps whose folder is gone (their data stays).\n"+
			"# Skip a folder with ignore: [pattern, ...] or a .hlignore file in it.\n"+
			"projects: [~/code]\n\n# --- where my apps are ---\n\nignore", 1)
	if got := read(t, dir); got != want {
		t.Errorf("hl set projects:\n--- want\n%s--- got\n%s", want, got)
	}

	edit(t, dir, ProxyScope, Change{Key: "bind", Unset: true})
	want = strings.Replace(want, "  # only this machine\n  bind: 127.0.0.1    # keep\n", "", 1)
	if got := read(t, dir); got != want {
		t.Errorf("hl unset proxy bind:\n--- want\n%s--- got\n%s", want, got)
	}
}

func TestOldFileStillLoads(t *testing.T) {
	c, err := Load(write(t, "apps:\n  my-app: {path: ~/code/my-app}\n"))
	if err != nil || c.Version != 0 {
		t.Fatalf("%+v, %v", c, err)
	}
}
