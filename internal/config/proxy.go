package config

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/DeM-Conve/project__hl/internal/defaults"
)

var projectRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// validateProxy checks the proxy settings that shape the Traefik config.
func (c *Config) validateProxy() error {
	if !projectRE.MatchString(c.Proxy.Project) {
		return fmt.Errorf("proxy.project: %q - use lowercase letters, digits, '_' and '-'", c.Proxy.Project)
	}
	return nil
}

// removedKeys are hl.yml keys earlier builds took, by path, with what to do instead. Check
// reports each with this message rather than as an unknown key, and `hl unset` removes them.
var removedKeys = map[string]string{
	"proxy.config":           "hl always generates the Traefik config (.hl/proxy/traefik.yml); remove it: `hl unset proxy config`",
	"proxy.dynamic":          "hl no longer loads a Traefik dynamic config file; remove it: `hl unset proxy dynamic`",
	"proxy.dashboard":        "the Traefik dashboard (it had no password) is gone; remove it: `hl unset proxy dashboard`",
	"proxy.dashboard_public": "the Traefik dashboard is gone; remove it: `hl unset proxy dashboard_public`",
	"proxy.access_log":       "`hl set proxy debug=true` turns on Traefik's access log and INFO log; remove it: `hl unset proxy access_log`",
	"proxy.log_level":        "`hl set proxy debug=true` turns on Traefik's INFO log and access log; remove it: `hl unset proxy log_level`",
}

// HomePath resolves a path from hl.yml: absolute, ~, or relative to the homelab folder.
func (c *Config) HomePath(p string) string {
	p = expandHome(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(c.Dir, p)
}

// ProxyStaticFile is the Traefik static config hl generates and the proxy runs with.
func (c *Config) ProxyStaticFile() string {
	return filepath.Join(c.ProxyDir(), defaults.TraefikStaticFile)
}
