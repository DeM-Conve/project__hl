package config

import (
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
		if err == nil || !strings.Contains(err.Error(), tc.has) {
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
	for _, k := range []string{"routing", "prefix", "host", "port", "service", "env.", "env_files", "compose", "path"} {
		if !strings.Contains(" "+got+" ", " "+k+" ") {
			t.Errorf("AppKeys() = %s, missing %s", got, k)
		}
	}
}
