package config

import (
	"errors"
	"strings"
	"testing"
)

const commented = `version: 1
# the proxy
proxy:
  bind: 127.0.0.1  # this machine only

apps:
  # my main app
  my-app:
    path: ~/code/my-app  # the folder
    port: 80  # container port
    env:
      TZ: UTC  # time zone
  blog: {path: ~/code/blog}
`

func TestEditAppKeepsComments(t *testing.T) {
	dir := write(t, commented)
	err := EditApp(dir, "my-app", []Change{
		{Key: "port", Value: "8080"},
		{Key: "routing", Value: "host"},
		{Key: "compose", Value: "compose.yml, compose.prod.yml"},
		{Key: "env.BASE", Value: "${HL_PREFIX}"},
		{Key: "env.NUM", Value: "8080"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := read(t, dir)
	for _, want := range []string{"# the proxy", "# this machine only", "# my main app", "# the folder", "# container port", "# time zone",
		"port: 8080 # container port", "routing: host", "compose: [compose.yml, compose.prod.yml]", `NUM: "8080"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := c.Apps["my-app"]
	if a.Port != 8080 || a.Routing != RoutingHost || len(a.Compose) != 2 || a.Env["BASE"] != "${HL_PREFIX}" || a.Env["TZ"] != "UTC" {
		t.Errorf("after set: %+v", a)
	}

	// Flow-style app, env added from nothing.
	if err := EditApp(dir, "blog", []Change{{Key: "env.TZ", Value: "UTC"}, {Key: "service", Value: "web"}}); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(dir); c.Apps["blog"].Env["TZ"] != "UTC" || c.Apps["blog"].Service != "web" {
		t.Errorf("blog: %+v", c.Apps["blog"])
	}

	if err := EditApp(dir, "my-app", []Change{{Key: "port", Unset: true}, {Key: "env.TZ", Unset: true}, {Key: "env.BASE", Unset: true}, {Key: "env.NUM", Unset: true}, {Key: "host", Unset: true}}); err != nil {
		t.Fatal(err)
	}
	got = read(t, dir)
	if strings.Contains(got, "port: 8080") || strings.Contains(got, "BASE") {
		t.Errorf("after unset:\n%s", got)
	}
	if !strings.Contains(got, "# my main app") || !strings.Contains(got, "# the folder") {
		t.Errorf("comments lost on unset:\n%s", got)
	}
	if c, _ := Load(dir); c.Apps["my-app"].Port != 0 || c.Apps["my-app"].Env != nil {
		t.Errorf("after unset: %+v", c.Apps["my-app"])
	}
}

// withFix is err's message with a ValueError's hint after it.
func withFix(err error) string {
	var ve *ValueError
	if errors.As(err, &ve) {
		return ve.Error() + " | " + ve.Hint()
	}
	return err.Error()
}

// A wrong value is a ValueError with the full key and no place in hl.yml (it wasn't written);
// a problem hl.yml already had keeps its file:line.
func TestEditValueErrors(t *testing.T) {
	dir := write(t, commented)
	err := EditApp(dir, "my-app", []Change{{Key: "routing", Value: "hots"}})
	var ve *ValueError
	if !errors.As(err, &ve) || ve.Error() != `apps.my-app.routing: "hots" is not path, host or none` || ve.Hint() != `did you mean "host"?` {
		t.Errorf("routing=hots: %T %v", err, err)
	}
	if err := EditApp(dir, "my-app", []Change{{Key: "port", Value: "x"}}); !errors.As(err, &ve) || ve.Error() != `apps.my-app.port: "x" is not a whole number` {
		t.Errorf("port=x: %T %v", err, err)
	}
	if err := Edit(dir, ProxyScope, []Change{{Key: "debug", Value: "x"}}); !errors.As(err, &ve) || ve.Error() != `proxy.debug: "x" is not true or false` {
		t.Errorf("debug=x: %T %v", err, err)
	}

	broken := write(t, "version: 1\napps:\n  a: {path: x, routing: hots}\n")
	err = EditApp(broken, "a", []Change{{Key: "port", Value: "80"}})
	var pe *ProblemsError
	if errors.As(err, &ve) || !errors.As(err, &pe) || !strings.Contains(err.Error(), "hl.yml:3:") {
		t.Errorf("broken hl.yml: %T %v", err, err)
	}
}

func TestEditAppRefusesBadResults(t *testing.T) {
	dir := write(t, commented)
	for _, tc := range []struct {
		changes []Change
		has     string
	}{
		{[]Change{{Key: "routing", Value: "hots"}}, `did you mean "host"?`},
		{[]Change{{Key: "port", Value: "eighty"}}, "not a whole number"},
		{[]Change{{Key: "port", Value: "0"}}, "not a port"},
		{[]Change{{Key: "prot", Value: "80"}}, `did you mean "port"?`},
		{[]Change{{Key: "path", Unset: true}}, "path is required"},
		{[]Change{{Key: "env", Value: "x"}}, "env.NAME=value"},
		{[]Change{{Key: "port.x", Value: "1"}}, "no parts"},
	} {
		err := EditApp(dir, "my-app", tc.changes)
		if err == nil || !strings.Contains(withFix(err), tc.has) {
			t.Errorf("%+v: err = %v, want %q", tc.changes, err, tc.has)
		}
		if read(t, dir) != commented {
			t.Fatalf("%+v: hl.yml changed although the result was refused", tc.changes)
		}
	}
	if err := EditApp(dir, "my-ap", []Change{{Key: "port", Value: "1"}}); err == nil || !strings.Contains(err.Error(), `did you mean "my-app"?`) {
		t.Errorf("unknown app: %v", err)
	}
}

func TestAppKeys(t *testing.T) {
	got := strings.Join(AppKeys(), " ")
	for _, k := range []string{"routing", "prefix", "host", "port", "service", "env.", "compose", "path"} {
		if !strings.Contains(" "+got+" ", " "+k+" ") {
			t.Errorf("AppKeys() = %s, missing %s", got, k)
		}
	}
}

func TestEditProxyAndSharedEnv(t *testing.T) {
	dir := write(t, commented)
	if err := Edit(dir, ProxyScope, []Change{
		{Key: "port", Value: "8082"}, {Key: "subnet", Value: "172.31.0.0/16"}, {Key: "bind", Value: "0.0.0.0"}, {Key: "debug", Value: "true"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := Edit(dir, HomelabScope, []Change{{Key: "env.TZ", Value: "Europe/Berlin"}}); err != nil {
		t.Fatal(err)
	}
	got := read(t, dir)
	for _, want := range []string{"bind: 0.0.0.0 # this machine only", "port: 8082", "subnet: 172.31.0.0/16", "debug: true", "# the proxy"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "TZ: Europe/Berlin") > strings.Index(got, "apps:") {
		t.Errorf("the shared env should come before apps:\n%s", got)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Proxy.Port != 8082 || c.Proxy.Bind != "0.0.0.0" || !c.Proxy.Debug || c.Env["TZ"] != "Europe/Berlin" || c.Apps["my-app"].Env["TZ"] != "UTC" {
		t.Errorf("after set: proxy %+v, env %v", c.Proxy, c.Env)
	}

	if err := Edit(dir, ProxyScope, []Change{{Key: "port", Unset: true}, {Key: "subnet", Unset: true}}); err != nil {
		t.Fatal(err)
	}
	if err := Edit(dir, HomelabScope, []Change{{Key: "env.TZ", Unset: true}}); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(dir)
	if c.Proxy.Port != 80 || c.Proxy.Subnet != "" || c.Env != nil {
		t.Errorf("after unset: proxy %+v, env %v", c.Proxy, c.Env)
	}

	// No proxy: section yet.
	dir = write(t, "apps: {}\n")
	if err := Edit(dir, ProxyScope, []Change{{Key: "port", Value: "8080"}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir); !strings.HasPrefix(got, "proxy:\n  port: 8080\n") {
		t.Errorf("new proxy section:\n%s", got)
	}
}

func TestEditProxyRefusesBadResults(t *testing.T) {
	dir := write(t, commented)
	for _, tc := range []struct {
		scope   string
		changes []Change
		has     string
	}{
		{ProxyScope, []Change{{Key: "port", Value: "70000"}}, "not a port"},
		{ProxyScope, []Change{{Key: "subnet", Value: "nope"}}, "not a subnet"},
		{ProxyScope, []Change{{Key: "bnd", Value: "0.0.0.0"}}, `did you mean "bind"?`},
		{ProxyScope, []Change{{Key: "debug", Value: "yes please"}}, "not true or false"},
		{ProxyScope, []Change{{Key: "dashboard", Value: "true"}}, "removed from hl"},
		{HomelabScope, []Change{{Key: "env.1X", Value: "a"}}, "not a valid variable name"},
		{HomelabScope, []Change{{Key: "name", Value: "Lab 2"}}, "lowercase"},
		{HomelabScope, []Change{{Key: "name", Value: "x", Op: OpAdd}}, "not a list"},
		{HomelabScope, []Change{{Key: "ignore", Value: "nope", Op: OpRemove}}, `"nope" is not in it`},
		{HomelabScope, []Change{{Key: "scan_depth", Value: "-1"}}, "scan_depth"},
		{HomelabScope, []Change{{Key: "apps", Value: "x"}}, "unknown setting"},
	} {
		err := Edit(dir, tc.scope, tc.changes)
		if err == nil || !strings.Contains(withFix(err), tc.has) {
			t.Errorf("%q %+v: err = %v, want %q", tc.scope, tc.changes, err, tc.has)
		}
		if read(t, dir) != commented {
			t.Fatalf("%+v: hl.yml changed although the result was refused", tc.changes)
		}
	}
	if !HasApp(dir, "blog") || HasApp(dir, "nope") {
		t.Error("HasApp")
	}
	if !IsHomelabKey("env.TZ") || !IsHomelabKey("env") || !IsHomelabKey("ignore") || IsHomelabKey("port") {
		t.Error("IsHomelabKey")
	}
	if got := strings.Join(ProxyKeys(), " "); !strings.Contains(got, "port") || !strings.Contains(got, "debug") || strings.Contains(got, "dashboard") {
		t.Errorf("ProxyKeys() = %s", got)
	}
}

func TestEditLists(t *testing.T) {
	dir := write(t, "version: 1\napps:\n  blog: {path: blog, compose: [a.yml]}\n")
	steps := []struct {
		changes []Change
		want    string
	}{
		{[]Change{{Key: "ignore", Value: "old,scratch-*"}}, "ignore: [old, scratch-*]\napps:"},
		{[]Change{{Key: "ignore", Value: "new,old", Op: OpAdd}}, "ignore: [old, scratch-*, new]"},
		{[]Change{{Key: "ignore", Value: "old", Op: OpRemove}}, "ignore: [scratch-*, new]"},
		{[]Change{{Key: "scan_depth", Value: "2"}, {Key: "name", Value: "lab2"}}, "scan_depth: 2\nname: lab2\napps:"},
		{[]Change{{Key: "ignore", Value: "scratch-*,new", Op: OpRemove}}, "version: 1\nscan_depth: 2"},
	}
	for _, st := range steps {
		if err := Edit(dir, HomelabScope, st.changes); err != nil {
			t.Fatalf("%+v: %v", st.changes, err)
		}
		if got := read(t, dir); !strings.Contains(got, st.want) {
			t.Errorf("%+v: want %q in:\n%s", st.changes, st.want, got)
		}
	}
	if strings.Contains(read(t, dir), "ignore") {
		t.Errorf("an empty list stays: %s", read(t, dir))
	}
	if err := Edit(dir, "blog", []Change{{Key: "compose", Value: "b.yml", Op: OpAdd}}); err != nil || !strings.Contains(read(t, dir), "compose: [a.yml, b.yml]") {
		t.Errorf("app list: %v\n%s", err, read(t, dir))
	}
}

// A key removed from hl can still be unset, so hl.yml can be fixed without hl edit.
func TestUnsetRemovedProxyKey(t *testing.T) {
	dir := write(t, "proxy:\n  port: 8080\n  dashboard: true\n  log_level: debug\napps: {}\n")
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "removed from hl") {
		t.Fatalf("Load: %v", err)
	}
	if err := Edit(dir, ProxyScope, []Change{{Key: "dashboard", Unset: true}, {Key: "log_level", Unset: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err != nil {
		t.Errorf("after unset: %v", err)
	}
}

func TestUnsetLastProxyKeyDropsTheSection(t *testing.T) {
	dir := write(t, "version: 1\nproxy:\n  port: 8080\napps: {}\n")
	if err := Edit(dir, ProxyScope, []Change{{Key: "port", Unset: true}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir); strings.Contains(got, "proxy") {
		t.Errorf("an empty proxy: is left:\n%s", got)
	}
	if err := Edit(dir, ProxyScope, []Change{{Key: "port", Unset: true}}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir); strings.Contains(got, "proxy") {
		t.Errorf("unset on no proxy: adds one:\n%s", got)
	}
	var u *UnknownError
	if err := Edit(dir, ProxyScope, []Change{{Key: "prot", Value: "1"}}); !errors.As(err, &u) {
		t.Errorf("unknown key: %T %v", err, err)
	}
	if err := Edit(dir, "nope", []Change{{Key: "port", Value: "1"}}); !errors.As(err, &u) {
		t.Errorf("unknown app: %T %v", err, err)
	}
}

func TestEditKeepsBlankLines(t *testing.T) {
	const spaced = `version: 1

# Shared by every app.
env:
  TZ: UTC

# The proxy: this machine only.
proxy:
  bind: 127.0.0.1 # loopback

  port: 8080

apps:
  # The blog.
  blog:
    path: ~/code/blog
    port: 80

  # The wiki, host-routed.
  wiki:
    path: ~/code/wiki # the folder
    routing: host

  notes: {path: ~/code/notes}
`
	dir := write(t, spaced)
	if err := EditApp(dir, "blog", []Change{{Key: "routing", Value: "host"}}); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(spaced, "    port: 80\n", "    port: 80\n    routing: host\n", 1)
	if got := read(t, dir); got != want {
		t.Errorf("set changed more than one line:\n--- got\n%s--- want\n%s", got, want)
	}

	// A changed and a removed line keep the spacing around them.
	if err := EditApp(dir, "wiki", []Change{{Key: "routing", Unset: true}, {Key: "path", Value: "~/src/wiki"}}); err != nil {
		t.Fatal(err)
	}
	if err := Edit(dir, ProxyScope, []Change{{Key: "port", Value: "9090"}}); err != nil {
		t.Fatal(err)
	}
	want = strings.Replace(want, "    path: ~/code/wiki # the folder\n    routing: host\n", "    path: ~/src/wiki # the folder\n", 1)
	want = strings.Replace(want, "  port: 8080\n", "  port: 9090\n", 1)
	if got := read(t, dir); got != want {
		t.Errorf("after unset:\n--- got\n%s--- want\n%s", got, want)
	}

	// A blank line inside a block scalar is the value's, not spacing: the file stays valid.
	dir = write(t, "version: 1\nenv:\n  MOTD: |\n    one\n\n    two\n\napps: {}\n")
	if err := Edit(dir, HomelabScope, []Change{{Key: "env.TZ", Value: "UTC"}}); err != nil {
		t.Fatal(err)
	}
	if c, err := Load(dir); err != nil || c.Env["MOTD"] != "one\n\ntwo\n" || c.Env["TZ"] != "UTC" {
		t.Errorf("block scalar: %+v, %v\n%s", c, err, read(t, dir))
	}
}
