package gen

import (
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

func proxySettings() config.Proxy {
	return config.Proxy{Bind: "127.0.0.1", Port: 80, Domain: "localhost", Routing: "path",
		Network: defaults.DefaultProxyNetwork, Image: defaults.DefaultProxyImage, Project: defaults.DefaultProxyProject}
}

func TestTraefikGenerated(t *testing.T) {
	out := Traefik(proxySettings())
	for _, want := range []string{
		"entryPoints:\n  web:", "address: ':80'",
		"providers:\n  docker:", "exposedByDefault: false", "network: 'hl-proxy'",
		"ping:", "level: 'ERROR'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("traefik.yml is missing %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"accessLog", "file:", "api:", "proxy.config"} {
		if strings.Contains(out, not) {
			t.Errorf("traefik.yml has %q by default:\n%s", not, out)
		}
	}

	p := proxySettings()
	p.Debug = true
	out = Traefik(p)
	for _, want := range []string{"accessLog:", "format: 'common'", "level: 'INFO'"} {
		if !strings.Contains(out, want) {
			t.Errorf("traefik.yml with debug is missing %q:\n%s", want, out)
		}
	}
}

// A setting changes every place that uses it.
func TestProxySettingsReachEveryFile(t *testing.T) {
	p := proxySettings()
	p.Network, p.Project, p.Domain, p.Port, p.Bind = "lab2-net", "lab2-proxy", "lab2.example", 8081, "0.0.0.0"
	static := Traefik(p)
	comp := Proxy(p, ProxyFiles{Static: "/lab2/.hl/proxy/traefik.yml", Hash: Hash([]byte(static))})
	for _, c := range []struct{ file, text, want string }{
		{"traefik.yml", static, "network: 'lab2-net'"},
		{"compose.yml", comp, "name: lab2-proxy\n"},
		{"compose.yml", comp, "name: 'lab2-net'"},
		{"compose.yml", comp, "'0.0.0.0:8081:80'"},
	} {
		if !strings.Contains(c.text, c.want) {
			t.Errorf("%s is missing %q:\n%s", c.file, c.want, c.text)
		}
	}
	if strings.Contains(static+comp, "hl-proxy") {
		t.Errorf("default network or project left in:\n%s\n%s", static, comp)
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
