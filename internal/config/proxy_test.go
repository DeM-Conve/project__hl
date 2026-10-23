package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProxyDefaultsAndFiles(t *testing.T) {
	dir := write(t, "apps: {}\n")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := c.Proxy
	if p.Image != DefaultProxyImage || p.Project != DefaultProxyProject || p.Network != DefaultProxyNetwork {
		t.Errorf("defaults: %+v", p)
	}
	if p.Image != "traefik:v"+TestedTraefikVersion {
		t.Errorf("the default image is the tested version: %s", p.Image)
	}
	if got := c.ProxyStaticFile(); got != filepath.Join(dir, ".hl", ProxyStateDir, TraefikStaticFile) {
		t.Errorf("generated static file: %s", got)
	}
	if c.ProxyDynamicFile() != "" {
		t.Error("no dynamic file by default")
	}

	c, err = Load(write(t, "proxy:\n  config: proxy/traefik.yml\n  dynamic: /etc/lab/routes.yml\n  project: lab2-proxy\napps: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ProxyStaticFile(); got != filepath.Join(c.Dir, "proxy", "traefik.yml") {
		t.Errorf("proxy.config relative to the homelab: %s", got)
	}
	if got := c.ProxyDynamicFile(); got != "/etc/lab/routes.yml" {
		t.Errorf("absolute proxy.dynamic: %s", got)
	}
	if c.Proxy.Project != "lab2-proxy" {
		t.Errorf("project: %s", c.Proxy.Project)
	}
}

func TestValidateProxy(t *testing.T) {
	for body, want := range map[string]string{
		"proxy: {project: My_Proxy}\n":               "proxy.project",
		"proxy: {log_level: loud}\n":                 "proxy.log_level",
		"proxy: {config: t.yml, access_log: true}\n": "proxy.access_log has no effect",
		"proxy: {config: t.yml, log_level: debug}\n": "proxy.log_level has no effect",
	} {
		_, err := Load(write(t, body))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want %q", body, err, want)
		}
	}
	for _, body := range []string{"proxy: {log_level: debug, access_log: true}\n", "proxy: {config: t.yml, dynamic: d.yml}\n"} {
		if _, err := Load(write(t, body)); err != nil {
			t.Errorf("%q: %v", body, err)
		}
	}
}
