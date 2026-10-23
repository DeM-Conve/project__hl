package gen

import (
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

func proxySettings() config.Proxy {
	return config.Proxy{Bind: "127.0.0.1", Port: 80, Domain: "localhost", Routing: "path",
		Network: defaults.DefaultProxyNetwork, Image: defaults.DefaultProxyImage, Project: defaults.DefaultProxyProject,
		Dashboard: true}
}

func TestTraefikGenerated(t *testing.T) {
	out := Traefik(proxySettings(), "")
	for _, want := range []string{
		"entryPoints:\n  web:", "address: ':80'",
		"providers:\n  docker:", "exposedByDefault: false", "network: 'hl-proxy'",
		"api:", "dashboard: true", "ping:", "level: 'ERROR'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("traefik.yml is missing %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"accessLog", "file:"} {
		if strings.Contains(out, not) {
			t.Errorf("traefik.yml has %q by default:\n%s", not, out)
		}
	}

	p := proxySettings()
	p.AccessLog, p.LogLevel = true, "debug"
	out = Traefik(p, "/lab/proxy/dynamic.yml")
	for _, want := range []string{"accessLog:", "format: 'common'", "level: 'DEBUG'",
		"file:", "filename: '/etc/traefik/dynamic/dynamic.yml'", "watch: true"} {
		if !strings.Contains(out, want) {
			t.Errorf("traefik.yml is missing %q:\n%s", want, out)
		}
	}
	// The generated file passes hl's own check of a user's file.
	if _, problems, warnings := CheckTraefik([]byte(out), p, "/lab/proxy/dynamic.yml"); len(problems)+len(warnings) > 0 {
		t.Errorf("generated config fails the check: %v %v", problems, warnings)
	}
}

// A setting changes every place that uses it.
func TestProxySettingsReachEveryFile(t *testing.T) {
	p := proxySettings()
	p.Network, p.Project, p.Domain, p.Port, p.Bind = "lab2-net", "lab2-proxy", "lab2.example", 8081, "0.0.0.0"
	static := Traefik(p, "")
	comp := Proxy(p, ProxyFiles{Static: "/lab2/.hl/proxy/traefik.yml", Hash: Hash([]byte(static)), Ping: true})
	for _, c := range []struct{ file, text, want string }{
		{"traefik.yml", static, "network: 'lab2-net'"},
		{"traefik.yml", static, "traefik.lab2.example"},
		{"compose.yml", comp, "name: lab2-proxy\n"},
		{"compose.yml", comp, "name: 'lab2-net'"},
		{"compose.yml", comp, "'0.0.0.0:8081:80'"},
		{"compose.yml", comp, "Host(`traefik.lab2.example`)"},
	} {
		if !strings.Contains(c.text, c.want) {
			t.Errorf("%s is missing %q:\n%s", c.file, c.want, c.text)
		}
	}
	if strings.Contains(static+comp, "hl-proxy") {
		t.Errorf("default network or project left in:\n%s\n%s", static, comp)
	}
}

func TestCheckTraefik(t *testing.T) {
	p := proxySettings()
	good := "entrypoints:\n  web:\n    address: \":80\"\nproviders:\n  docker:\n    exposedbydefault: false\n    network: hl-proxy\napi:\n  dashboard: true\nping: {}\n"
	ping, problems, warnings := CheckTraefik([]byte(good), p, "")
	if !ping || len(problems) > 0 || len(warnings) > 0 {
		t.Errorf("a complete file (keys in any case): ping=%v problems=%v warnings=%v", ping, problems, warnings)
	}

	for _, c := range []struct{ name, file, dynamic, want string }{
		{"no web entrypoint", "providers: {docker: {network: hl-proxy}}", "", "no entryPoints.web"},
		{"wrong port", "entryPoints: {web: {address: ':8000'}}\nproviders: {docker: {network: hl-proxy}}", "", "use \":80\""},
		{"no docker provider", "entryPoints: {web: {address: ':80'}}", "", "no providers.docker"},
		{"wrong network", "entryPoints: {web: {address: ':80'}}\nproviders: {docker: {network: other}}", "", `must be "hl-proxy"`},
		{"no network", "entryPoints: {web: {address: ':80'}}\nproviders: {docker: {}}", "", "is not set"},
		{"dynamic not read", good, "/lab/dyn/routes.yml", "filename: /etc/traefik/dynamic/routes.yml"},
		{"not yaml", "entryPoints: [", "", "not valid YAML"},
	} {
		_, problems, _ := CheckTraefik([]byte(c.file), p, c.dynamic)
		if !strings.Contains(strings.Join(problems, "\n"), c.want) {
			t.Errorf("%s: problems %v should mention %q", c.name, problems, c.want)
		}
	}
	withDir := strings.Replace(good, "providers:\n  docker:\n    exposedbydefault: false\n    network: hl-proxy\n", "", 1) + "providers:\n  docker: {network: hl-proxy, exposedByDefault: false}\n  file: {directory: /etc/traefik/dynamic/}\n"
	if _, problems, _ := CheckTraefik([]byte(withDir), p, "/lab/dyn/routes.yml"); len(problems) > 0 {
		t.Errorf("a file provider reading the folder is enough: %v", problems)
	}

	_, _, warnings = CheckTraefik([]byte("entryPoints: {web: {address: ':80'}}\nproviders: {docker: {network: hl-proxy}}"), p, "")
	all := strings.Join(warnings, "\n")
	for _, want := range []string{"exposedByDefault", "api: {dashboard: true}", "ping"} {
		if !strings.Contains(all, want) {
			t.Errorf("warnings %v should mention %q", warnings, want)
		}
	}
}

func TestImageOlder(t *testing.T) {
	for _, c := range []struct {
		image        string
		older, known bool
	}{
		{"traefik:v3.6.25", false, true},
		{"traefik:v3.6.3", true, true},
		{"traefik:v3.5", true, true},
		{"traefik:v2.11.0", true, true},
		{"traefik:v3.6", false, true}, // floating tag: latest v3.6.x
		{"traefik:v3", false, true},
		{"traefik:3.7.0", false, true},
		{"docker.io/library/traefik:v3.6.1", true, true},
		{"registry.example:5000/traefik:v3.6.1@sha256:abc", true, true},
		{"traefik:latest", false, false},
		{"traefik", false, false},
		{"my-proxy:v1.0.0", false, false},
	} {
		older, known := ImageOlder(c.image, "3.6.25")
		if older != c.older || known != c.known {
			t.Errorf("ImageOlder(%q) = %v, %v; want %v, %v", c.image, older, known, c.older, c.known)
		}
	}
}
