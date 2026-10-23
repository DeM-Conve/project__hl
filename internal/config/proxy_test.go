package config

import (
	"os"
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
	if c.ProxyDynamicFile() != "" {
		t.Error("no dynamic file by default")
	}

	c, err = Load(write(t, "proxy:\n  config: proxy/traefik.yml\n  dynamic: routes/extra.yml\n  project: lab2-proxy\napps: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ProxyStaticFile(); got != filepath.Join(c.Dir, "proxy", "traefik.yml") {
		t.Errorf("proxy.config relative to the homelab: %s", got)
	}
	if got := c.ProxyDynamicFile(); got != filepath.Join(c.Dir, "routes", "extra.yml") {
		t.Errorf("proxy.dynamic relative to the homelab: %s", got)
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
	for _, body := range []string{"proxy: {log_level: debug, access_log: true}\n", "proxy: {config: t.yml, dynamic: proxy/d.yml}\n"} {
		if _, err := Load(write(t, body)); err != nil {
			t.Errorf("%q: %v", body, err)
		}
	}
}

func TestDynamicMustBeInASubfolder(t *testing.T) {
	for dyn, want := range map[string]string{
		"d.yml":              "in the homelab folder itself",
		"./d.yml":            "in the homelab folder itself",
		"/etc/traefik/d.yml": "outside the homelab folder",
		"../d.yml":           "outside the homelab folder",
		"data/d.yml":         "which hl manages",
		"data/my-app/d.yml":  "which hl manages",
		".hl/d.yml":          "which hl manages",
		".hl/proxy/d.yml":    "which hl manages",
	} {
		_, err := Load(write(t, "proxy: {dynamic: "+dyn+"}\n"))
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), defaults.ExampleDynamicFile) {
			t.Errorf("%s: error %v, want %q and the fix", dyn, err, want)
		}
	}
	for _, dyn := range []string{"proxy/dynamic.yml", "routes/nas/extra.yml", "data-routes/d.yml"} {
		if _, err := Load(write(t, "proxy: {dynamic: "+dyn+"}\n")); err != nil {
			t.Errorf("%s: %v", dyn, err)
		}
	}
	// An absolute path inside the homelab is fine too.
	dir := write(t, "apps: {}\n")
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("proxy: {dynamic: "+filepath.Join(dir, "proxy", "d.yml")+"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err != nil {
		t.Errorf("absolute path inside the homelab: %v", err)
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
