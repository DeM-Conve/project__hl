package gen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
)

var model = &compose.Model{
	Services: map[string]compose.Service{
		"frontend": {Ports: []compose.Port{{Target: 80}}, Networks: map[string]json.RawMessage{"default": nil, "internal": nil}},
		"db":       {},
	},
	Volumes: map[string]compose.Volume{
		"pgdata": {},
		"shared": {External: true},
		"nfs":    {Driver: "local", DriverOpts: map[string]string{"type": "nfs"}},
	},
}

func TestOverridePath(t *testing.T) {
	out, dirs, err := Override(App{Name: "my-app.io", Routing: config.RoutingPath, Prefix: "/my-app.io",
		Host: "localhost", Service: "frontend", Port: 80, Network: "hl-proxy", DataDir: "/lab/data/my-app.io"}, model)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ports: !reset []",
		"      default: {}\n      internal: {}\n      hl-proxy: {}\n",
		"traefik.http.routers.my-app-io.rule: 'Path(`/my-app.io`) || PathPrefix(`/my-app.io/`)'",
		`redirectregex.regex: '^(https?://[^/]+/my-app\.io)$$'`,
		"redirectregex.replacement: '$${1}/'",
		"stripprefix.prefixes: '/my-app.io'",
		"device: '/lab/data/my-app.io/pgdata'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("override is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "shared:") || strings.Contains(out, "nfs:") {
		t.Errorf("external and custom-driver volumes must be left alone:\n%s", out)
	}
	if len(dirs) != 1 || dirs[0] != "/lab/data/my-app.io/pgdata" {
		t.Errorf("dirs = %v", dirs)
	}
}

func TestOverrideHostAndNone(t *testing.T) {
	out, _, err := Override(App{Name: "blog", Routing: config.RoutingHost, Host: "blog.localhost",
		Service: "frontend", Port: 8080, Network: "hl-proxy", DataDir: "/d"}, model)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rule: 'Host(`blog.localhost`)'") || strings.Contains(out, "stripprefix") {
		t.Errorf("host routing:\n%s", out)
	}
	if !strings.Contains(out, "loadbalancer.server.port: '8080'") {
		t.Errorf("port label:\n%s", out)
	}
	out, _, _ = Override(App{Name: "db", Routing: config.RoutingNone, DataDir: "/d"}, model)
	if strings.Contains(out, "services:") || strings.Contains(out, "traefik") {
		t.Errorf("routing none must not touch services:\n%s", out)
	}
	if _, _, err := Override(App{Name: "x", Routing: config.RoutingHost, Service: "missing"}, model); err == nil {
		t.Error("a missing service should be an error")
	}
}

func TestProxy(t *testing.T) {
	out := Proxy(config.Proxy{Bind: "0.0.0.0", Port: 8080, Domain: "lab.example", Network: "hl-proxy", Subnet: "10.123.0.0/16", Image: "traefik:v3.6", Project: "hl-proxy", Dashboard: true},
		ProxyFiles{Static: "/lab/.hl/proxy/traefik.yml", Hash: "abc", Ping: true})
	for _, want := range []string{"'0.0.0.0:8080:80'", "'/lab/.hl/proxy/traefik.yml:/etc/traefik/traefik.yml:ro'", "Host(`traefik.lab.example`)", "subnet: '10.123.0.0/16'", "healthcheck:", "hl.static-config: 'abc'"} {
		if !strings.Contains(out, want) {
			t.Errorf("proxy is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "command:") {
		t.Errorf("Traefik's settings belong in the static config file, not flags:\n%s", out)
	}
	p := config.Proxy{Bind: "::1", Port: 8080, Domain: "lab.example", Network: "hl-proxy", Image: "traefik:v3.6", Project: "hl-proxy"}
	if out := Proxy(p, ProxyFiles{Static: "/lab/traefik.yml"}); !strings.Contains(out, "'[::1]:8080:80'") {
		t.Errorf("IPv6 bind needs brackets:\n%s", out)
	}
}

func TestDashboardOffByDefault(t *testing.T) {
	p := config.Proxy{Bind: "127.0.0.1", Port: 80, Domain: "localhost", Network: "hl-proxy", Image: "traefik:v3.6", Project: "hl-proxy"}
	f := ProxyFiles{Static: "/lab/traefik.yml"}
	if out := Proxy(p, f) + Traefik(p, ""); strings.Contains(out, "api@internal") || strings.Contains(out, "dashboard: true") {
		t.Errorf("dashboard should be off:\n%s", out)
	}
	p.Dashboard = true
	if out := Proxy(p, f) + Traefik(p, ""); !strings.Contains(out, "api@internal") || !strings.Contains(out, "dashboard: true") {
		t.Errorf("dashboard should be on:\n%s", out)
	}
}
