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

func TestValidateValuesAndClashes(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{"proxy: {bind: localhost}", "proxy.bind"},
		{"proxy: {domain: 'not a host'}", "proxy.domain"},
		{"proxy: {subnet: 10.0.0.0}", "proxy.subnet"},
		{"proxy: {network: '-x'}", "proxy.network"},
		{"name: Lab_2", "name"},
		{"proxy: {bind: 0.0.0.0, dashboard: true}", "proxy.dashboard: the Traefik dashboard has no password"},
		{"apps: {a: {path: x, host: 'bad host'}}", "apps.a.host"},
		{"apps: {a: {path: x, service: '-it'}}", "apps.a.service"},
		{"apps: {a: {path: x, env: {'1BAD': v}}}", "apps.a.env.1BAD"},
		{"apps: {a.b: {path: x}, a-b: {path: y}}", "runs as the same Docker project"},
		{"apps: {a: {path: x, prefix: /p}, b: {path: y, prefix: /p}}", "same URL prefix /p"},
		{"apps: {a: {path: x, routing: host, host: h.example}, b: {path: y, routing: host, host: h.example}}", "same host h.example"},
		{"proxy: {dashboard: true}\napps: {traefik: {path: x, routing: host}}", "the Traefik dashboard"},
	} {
		_, err := Load(write(t, c.body))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.body, err, c.want)
		}
	}
	for _, body := range []string{
		"proxy: {bind: 0.0.0.0, dashboard: true, dashboard_public: true}",
		"proxy: {bind: '::1', dashboard: true}",
		"name: lab-2\nproxy: {subnet: 172.30.0.0/16}",
	} {
		if _, err := Load(write(t, body)); err != nil {
			t.Errorf("%q: %v", body, err)
		}
	}
	// Validate's problems carry the line of their key.
	_, err := Load(write(t, "proxy:\n  bind: 0.0.0.0\n  dashboard: true\n"))
	if err == nil || !strings.Contains(err.Error(), "hl.yml:3:3: proxy.dashboard") {
		t.Errorf("line of a Validate problem: %v", err)
	}
	if _, err := Load(t.TempDir()); err == nil || !strings.Contains(err.Error(), "hl init") {
		t.Errorf("no hl.yml: %v", err)
	}
}

// A homelab's name changes every Docker name it uses; without one, the names
// existing homelabs already have stay as they are.
func TestNameChangesEveryDockerName(t *testing.T) {
	c, err := Load(write(t, "apps: {my-app.io: {path: x}}"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ProjectName("my-app.io") != "hl-my-app-io" || c.ProxyProject() != "hl-proxy" || c.Proxy.Network != "hl-proxy" {
		t.Errorf("default names: %s %s %s", c.ProjectName("my-app.io"), c.ProxyProject(), c.Proxy.Network)
	}
	c, err = Load(write(t, "name: lab2\napps: {my-app.io: {path: x}}"))
	if err != nil {
		t.Fatal(err)
	}
	if c.ProjectName("my-app.io") != "hl-lab2-my-app-io" || c.ProxyProject() != "hl-lab2-proxy" || c.Proxy.Network != "hl-lab2-proxy" {
		t.Errorf("named: %s %s %s", c.ProjectName("my-app.io"), c.ProxyProject(), c.Proxy.Network)
	}
	// Set explicitly, proxy.project and proxy.network win over the name.
	c, _ = Load(write(t, "name: lab2\nproxy: {project: p, network: n}"))
	if c.ProxyProject() != "p" || c.Proxy.Network != "n" {
		t.Errorf("explicit: %s %s", c.ProxyProject(), c.Proxy.Network)
	}
}

func TestNameFor(t *testing.T) {
	for in, want := range map[string]string{"My App": "my-app", "blog": "blog", "_x  y!": "x-y", "a.b_c": "a.b_c", "  ": ""} {
		if got := NameFor(in); got != want {
			t.Errorf("NameFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDashboardURL(t *testing.T) {
	c, _ := Load(write(t, "proxy: {port: 8080, dashboard: true, domain: lab.example}"))
	if got := c.DashboardURL(); got != "http://traefik.lab.example:8080/" {
		t.Errorf("DashboardURL = %q", got)
	}
	c, _ = Load(write(t, "proxy: {}"))
	if c.DashboardURL() != "" {
		t.Error("the dashboard is off by default")
	}
}

func TestHomeAndDefaultHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config dir"))
	t.Setenv("HL_HOME", "")
	wd, _ := os.Getwd()
	if err := os.Chdir(home); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if _, err := Home(""); err == nil {
		t.Error("no homelab anywhere should be an error")
	}
	lab := filepath.Join(home, "my lab")
	if err := SetDefaultHome(lab); err != nil {
		t.Fatal(err)
	}
	if got, _ := Home(""); got != lab {
		t.Errorf("default = %q", got)
	}
	t.Setenv("HL_HOME", "~/other")
	if got, _ := Home(""); got != filepath.Join(home, "other") {
		t.Errorf("HL_HOME = %q", got)
	}
	if got, _ := Home("~"); got != home {
		t.Errorf("--home ~ = %q", got)
	}
}
