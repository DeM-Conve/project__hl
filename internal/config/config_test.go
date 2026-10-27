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

func TestHostTimeZone(t *testing.T) {
	t.Setenv("TZ", "Asia/Kolkata")
	if z, src := hostTimeZone(); z != "Asia/Kolkata" || src != "host ($TZ)" {
		t.Errorf("from $TZ: %q %q", z, src)
	}
	t.Setenv("TZ", ":/etc/localtime") // a file, not a zone: read the system's instead
	if z, _ := hostTimeZone(); strings.HasPrefix(z, ":") {
		t.Errorf("a TZ naming a file was used: %q", z)
	}
}

func TestHostTZOnlyWhenNothingSetsIt(t *testing.T) {
	old := HostTimeZone
	HostTimeZone = func() (string, string) { return "Europe/Berlin", "host (/etc/localtime)" }
	t.Cleanup(func() { HostTimeZone = old })
	app := t.TempDir()
	dir := write(t, "apps:\n  a: {path: '"+app+"'}\n")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	tz := func() (string, string) {
		for _, v := range c.EnvVars("a", c.Apps["a"]) {
			if v.Name == "TZ" {
				return v.Value, v.Source
			}
		}
		return "", ""
	}
	if v, src := tz(); v != "Europe/Berlin" || src != "host (/etc/localtime)" {
		t.Errorf("nothing sets TZ: %q from %q", v, src)
	}
	if env := strings.Join(c.AppEnv("a", c.Apps["a"]), "\n"); !strings.Contains(env, "\nTZ=Europe/Berlin") {
		t.Errorf("AppEnv: %s", env)
	}

	c.Env = map[string]string{"TZ": "UTC"}
	if v, src := tz(); v != "UTC" || src != SourceSharedEnv {
		t.Errorf("shared env: %q from %q", v, src)
	}
	c.Env = nil
	c.Apps["a"].Env = map[string]string{"TZ": "America/Lima"}
	if v, src := tz(); v != "America/Lima" || src != SourceAppEnv {
		t.Errorf("app env: %q from %q", v, src)
	}
	c.Apps["a"].Env = nil

	// The app's own .env, or an env file, setting TZ: hl adds nothing, so theirs applies.
	if err := os.WriteFile(filepath.Join(app, ".env"), []byte("# zone\n  TZ = Asia/Tokyo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, _ := tz(); v != "" {
		t.Errorf(".env sets TZ, hl added %q", v)
	}
	_ = os.Remove(filepath.Join(app, ".env"))

	HostTimeZone = func() (string, string) { return "", "" }
	if env := strings.Join(c.AppEnv("a", c.Apps["a"]), "\n"); strings.Contains(env, "TZ=") {
		t.Errorf("host zone unknown, TZ set anyway: %s", env)
	}
}

func TestDefaultsAndURLs(t *testing.T) {
	dir := write(t, `
proxy: {port: 8080}
apps:
  my-app.io: {path: ../my-app}
  blog: {path: /srv/blog, routing: host}
  wiki: {path: wiki, routing: host, host: wiki.example.com}
  api: {path: api}
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
		{"api", "http://localhost:8080/api", "/api"},
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
		"proxy: {routing: host}", // a removed key
		"apps: {Bad: {path: x}}",
		"apps: {a: {}}",
		"apps: {a: {path: x, routing: sideways}}",
		"apps: {a: {path: x, prefix: /a}}", // a removed key
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
	if !strings.Contains(string(b), "# hl homelab.") {
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
		{"name: Lab_2", "name"},
		{"apps: {a: {path: x, host: 'bad host'}}", "apps.a.host"},
		{"apps: {a: {path: x, service: '-it'}}", "apps.a.service"},
		{"apps: {a: {path: x, env: {'1BAD': v}}}", "apps.a.env.1BAD"},
		{"apps: {a.b: {path: x}, a-b: {path: y}}", "runs as the same Docker project"},
		{"apps: {a: {path: x, routing: host, host: h.example}, b: {path: y, routing: host, host: h.example}}", "same host h.example"},
	} {
		_, err := Load(write(t, c.body))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %q", c.body, err, c.want)
		}
	}
	for _, body := range []string{
		"proxy: {bind: 0.0.0.0, debug: true}",
		"proxy: {bind: '::1'}",
		"name: lab-2\nproxy: {subnet: 172.30.0.0/16}",
	} {
		if _, err := Load(write(t, body)); err != nil {
			t.Errorf("%q: %v", body, err)
		}
	}
	// Validate's problems carry the line of their key.
	_, err := Load(write(t, "proxy:\n  port: 80\n  bind: localhost\n"))
	if err == nil || !strings.Contains(err.Error(), "hl.yml:3:9: proxy.bind") {
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
}

func TestNameFor(t *testing.T) {
	for in, want := range map[string]string{"My App": "my-app", "blog": "blog", "_x  y!": "x-y", "a.b_c": "a.b_c", "  ": ""} {
		if got := NameFor(in); got != want {
			t.Errorf("NameFor(%q) = %q, want %q", in, got, want)
		}
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
