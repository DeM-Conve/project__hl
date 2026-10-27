package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteNewFillsAnswersAndKeepsComments(t *testing.T) {
	dir := t.TempDir()
	s := InitSettings{Bind: "0.0.0.0", Port: 8080, Projects: []string{"~/code", "../more projects"}}
	if err := WriteNew(dir, s); err != nil {
		t.Fatal(err)
	}
	if err := WriteNew(dir, s); err == nil {
		t.Error("WriteNew must not overwrite an existing hl.yml")
	}
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	text := string(b)
	for _, want := range []string{"# hl homelab.", "# Folders with your projects", "apps: {}", "bind: 0.0.0.0", "port: 8080"} {
		if !strings.Contains(text, want) {
			t.Errorf("hl.yml lost %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "projects:") > strings.Index(text, "proxy:") || strings.Index(text, "proxy:") > strings.Index(text, "apps:") {
		t.Errorf("order should be projects, proxy, apps:\n%s", text)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Proxy.Bind != "0.0.0.0" || c.Proxy.Port != 8080 {
		t.Errorf("proxy = %+v", c.Proxy)
	}
	if strings.Join(c.Projects, "|") != "~/code|../more projects" {
		t.Errorf("projects = %q", c.Projects)
	}
	home, _ := os.UserHomeDir()
	if got := c.ProjectDirs(); got[0] != filepath.Join(home, "code") || got[1] != filepath.Join(filepath.Dir(dir), "more projects") {
		t.Errorf("ProjectDirs = %q", got)
	}
}

func TestWriteNewIsMinimal(t *testing.T) {
	dir := t.TempDir()
	pd := ProxyDefaults()
	// Defaults spelled out are left out; only what differs is written.
	s := InitSettings{Bind: pd.Bind, Port: pd.Port, Projects: []string{"~/code"}, StripPrefix: []string{"project__"}}
	if err := WriteNew(dir, s); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	text := string(b)
	for _, unwanted := range []string{"proxy:", "bind:", "dashboard", "env:", "name:"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("a new hl.yml should not have %q:\n%s", unwanted, text)
		}
	}
	for _, want := range []string{"version: 1", "projects: [~/code]", "strip_prefix: [project__]", "apps: {}"} {
		if !strings.Contains(text, want) {
			t.Errorf("hl.yml lacks %q:\n%s", want, text)
		}
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := s.Config(dir); c.Proxy != want.Proxy || strings.Join(c.Projects, ",") != strings.Join(want.Projects, ",") {
		t.Errorf("loaded %+v, planned %+v", c, want)
	}
	if err := WriteNew(t.TempDir(), InitSettings{Port: 8081}); err != nil {
		t.Fatal(err)
	}
}

func TestSetProjectsAndPinApp(t *testing.T) {
	dir := write(t, "# top\nproxy: {port: 80}\nprojects: [old] # mine\napps:\n  blog: {path: ../blog, service: web, port: 80} # keep\n")
	if err := SetProjects(dir, []string{"new"}); err != nil {
		t.Fatal(err)
	}
	if err := PinApp(dir, "blog", "frontend", 3000); err != nil {
		t.Fatal(err)
	}
	if err := PinApp(dir, "nope", "x", 1); err == nil {
		t.Error("PinApp of a missing app should fail")
	}
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	for _, want := range []string{"# top", "# mine", "# keep"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("lost comment %q:\n%s", want, b)
		}
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a := c.Apps["blog"]; a.Service != "frontend" || a.Port != 3000 || a.Path != "../blog" {
		t.Errorf("blog = %+v", a)
	}
	if strings.Join(c.Projects, ",") != "new" {
		t.Errorf("projects = %q", c.Projects)
	}
}

func TestValidateProjects(t *testing.T) {
	for _, body := range []string{"ignore: ['[']", "scan_depth: -1", "projects: ['']", "strip_prefix: ['']"} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%q: expected an error", body)
		}
	}
}

func TestStripped(t *testing.T) {
	prefixes := []string{"project__", "app-"}
	for in, want := range map[string]string{
		"project__blog.io": "blog.io",
		"app-wiki":         "wiki",
		"project__":        "project__", // nothing would be left
		"blog":             "blog",
		"my-project__x":    "my-project__x",
	} {
		if got := Stripped(in, prefixes); got != want {
			t.Errorf("Stripped(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSetList(t *testing.T) {
	dir := write(t, "# top\nprojects: [code] # mine\napps: {}\n")
	if err := SetList(dir, "strip_prefix", []string{"project__"}); err != nil {
		t.Fatal(err)
	}
	if err := SetList(dir, "ignore", []string{"old", "scratch-*"}); err != nil {
		t.Fatal(err)
	}
	if err := SetList(dir, "ignore", []string{"["}); err == nil || !strings.Contains(err.Error(), "not changed") {
		t.Errorf("a bad pattern must leave hl.yml alone, got %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	text := string(b)
	for _, want := range []string{"# top", "# mine", "strip_prefix: [project__]", "ignore: [old, scratch-*]"} {
		if !strings.Contains(text, want) {
			t.Errorf("hl.yml lacks %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "strip_prefix:") > strings.Index(text, "apps:") {
		t.Errorf("new lists go before apps:\n%s", text)
	}
	if err := SetList(dir, "ignore", nil); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Ignore != nil || strings.Join(c.StripPrefix, ",") != "project__" {
		t.Errorf("ignore = %q, strip_prefix = %q", c.Ignore, c.StripPrefix)
	}
}
