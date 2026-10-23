package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDefaultsAndURLs(t *testing.T) {
	dir := write(t, `
proxy: {port: 8080}
apps:
  my-app.io: {path: ../my-app}
  blog: {path: /srv/blog, routing: host}
  wiki: {path: wiki, routing: host, host: wiki.example.com}
  api: {path: api, prefix: /v1/api}
  db: {path: db, routing: none}
`)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ app, url, prefix string }{
		{"my-app.io", "http://localhost:8080/my-app.io", "/my-app.io"},
		{"blog", "http://blog.localhost:8080", ""},
		{"wiki", "http://wiki.example.com:8080", ""},
		{"api", "http://localhost:8080/v1/api", "/v1/api"},
		{"db", "", ""},
	}
	for _, tc := range cases {
		a := c.Apps[tc.app]
		if got := c.URL(tc.app, a); got != tc.url {
			t.Errorf("URL(%s) = %q, want %q", tc.app, got, tc.url)
		}
		if got := c.Prefix(tc.app, a); got != tc.prefix {
			t.Errorf("Prefix(%s) = %q, want %q", tc.app, got, tc.prefix)
		}
	}
	if got, want := c.AppDir(c.Apps["my-app.io"]), filepath.Join(filepath.Dir(dir), "my-app"); got != want {
		t.Errorf("AppDir = %q, want %q", got, want)
	}
	if got := ProjectName("my-app.io"); got != "hl-my-app-io" {
		t.Errorf("ProjectName = %q", got)
	}
}

func TestValidate(t *testing.T) {
	for _, body := range []string{
		"proxy: {routing: subdomain}",
		"apps: {Bad: {path: x}}",
		"apps: {a: {}}",
		"apps: {a: {path: x, routing: sideways}}",
		"apps: {a: {path: x, prefix: /trailing/}}",
	} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%q: expected an error", body)
		}
	}
}

func TestAddAndRemoveKeepComments(t *testing.T) {
	dir := write(t, Template)
	if err := AddApp(dir, "blog", &App{Path: "../blog", Service: "web", Port: 80}); err != nil {
		t.Fatal(err)
	}
	if err := AddApp(dir, "wiki", &App{Path: "../wiki", Routing: RoutingHost}); err != nil {
		t.Fatal(err)
	}
	if err := AddApp(dir, "blog", &App{Path: "x"}); err == nil {
		t.Error("adding blog twice should fail")
	}
	b, _ := os.ReadFile(filepath.Join(dir, FileName))
	if !strings.Contains(string(b), "# 127.0.0.1 = this machine only") {
		t.Errorf("comments lost:\n%s", b)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Apps) != 2 || c.Apps["blog"].Service != "web" || c.Apps["wiki"].Routing != RoutingHost {
		t.Fatalf("apps after add: %+v", c.Apps)
	}
	if err := RemoveApp(dir, "blog"); err != nil {
		t.Fatal(err)
	}
	if c, _ = Load(dir); len(c.Apps) != 1 || c.Apps["wiki"] == nil {
		t.Fatalf("apps after remove: %+v", c.Apps)
	}
}

func TestEnv(t *testing.T) {
	t.Setenv("HL_TEST_SHELL", "from-shell")
	dir := write(t, `
env: {TZ: UTC, SHARED: base}
apps:
  blog:
    path: blog
    env:
      SHARED: own
      BASE_PATH: ${HL_PREFIX}
      URL: ${HL_URL}/
      DATA: ${HL_DATA}/db
      FROM_SHELL: ${HL_TEST_SHELL}
`)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Later entries win, as in exec.Cmd.Env.
	got := map[string]string{}
	for _, kv := range c.AppEnv("blog", c.Apps["blog"]) {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	want := map[string]string{
		"HL_APP": "blog", "HL_PREFIX": "/blog", "HL_URL": "http://localhost/blog",
		"TZ": "UTC", "SHARED": "own", "BASE_PATH": "/blog", "URL": "http://localhost/blog/",
		"DATA": filepath.Join(dir, "data", "blog", "db"), "FROM_SHELL": "from-shell",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}
