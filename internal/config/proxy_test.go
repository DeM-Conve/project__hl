package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeM-Conve/project__hl/internal/defaults"
)

func TestProxyDefaultsAndFiles(t *testing.T) {
	dir := write(t, "apps: {}\n")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := c.Proxy
	if p.Image != defaults.DefaultProxyImage || p.Project != defaults.DefaultProxyProject || p.Network != defaults.DefaultProxyNetwork {
		t.Errorf("defaults: %+v", p)
	}
	if p.Image != "traefik:v"+defaults.TestedTraefikVersion {
		t.Errorf("the default image is the tested version: %s", p.Image)
	}
	if got := c.ProxyStaticFile(); got != filepath.Join(dir, ".hl", defaults.ProxyStateDir, defaults.TraefikStaticFile) {
		t.Errorf("generated static file: %s", got)
	}

	c, err = Load(write(t, "proxy:\n  project: lab2-proxy\napps: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Proxy.Project != "lab2-proxy" {
		t.Errorf("project: %s", c.Proxy.Project)
	}
}

func TestValidateProxy(t *testing.T) {
	for body, want := range map[string]string{
		"proxy: {project: My_Proxy}\n":      "proxy.project",
		"proxy: {access_log: true}\n":       "proxy.access_log: removed from hl",
		"proxy: {dynamic: proxy/d.yml}\n":   "proxy.dynamic: removed from hl",
		"proxy: {dashboard_public: true}\n": "`hl unset proxy dashboard_public`",
	} {
		_, err := Load(write(t, body))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want %q", body, err, want)
		}
	}
	if _, err := Load(write(t, "proxy: {debug: true}\n")); err != nil {
		t.Errorf("debug: %v", err)
	}
}

func TestReservedAppName(t *testing.T) {
	for _, name := range []string{"proxy"} {
		if err := ValidName(name); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("ValidName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"proxy.io", "my-proxy", "proxy2", "web"} {
		if err := ValidName(name); err != nil {
			t.Errorf("ValidName(%q) = %v", name, err)
		}
	}
	_, err := Load(write(t, "apps:\n  proxy:\n    path: x\n"))
	if err == nil || !strings.Contains(err.Error(), "apps.proxy") || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("an app named proxy in hl.yml: %v", err)
	}
	// With another proxy.project, an app whose project would be it clashes instead.
	_, err = Load(write(t, "proxy: {project: hl-blog}\napps:\n  blog:\n    path: x\n"))
	if err == nil || !strings.Contains(err.Error(), "proxy.project") {
		t.Errorf("app project = proxy.project: %v", err)
	}
}
