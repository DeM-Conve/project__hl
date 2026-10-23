package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var projectRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// validateProxy checks the proxy settings that shape the Traefik config.
func (c *Config) validateProxy() error {
	p := c.Proxy
	if !projectRE.MatchString(p.Project) {
		return fmt.Errorf("proxy.project: %q - use lowercase letters, digits, '_' and '-'", p.Project)
	}
	if p.LogLevel != "" {
		ok := false
		for _, l := range ProxyLogLevels {
			if strings.EqualFold(p.LogLevel, l) {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("proxy.log_level: %q is not one of %s", p.LogLevel, strings.Join(ProxyLogLevels, ", "))
		}
	}
	if p.Dynamic != "" {
		if err := c.checkDynamicPath(); err != nil {
			return err
		}
	}
	if p.Config != "" {
		// These only shape the file hl generates; with your own file, set them there.
		if p.AccessLog {
			return fmt.Errorf("proxy.access_log has no effect with proxy.config - set accessLog in %s instead", p.Config)
		}
		if p.LogLevel != "" {
			return fmt.Errorf("proxy.log_level has no effect with proxy.config - set log.level in %s instead", p.Config)
		}
	}
	return nil
}

// HomePath resolves a path from hl.yml: absolute, ~, or relative to the homelab folder.
func (c *Config) HomePath(p string) string {
	p = expandHome(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(c.Dir, p)
}

// ProxyStaticFile is the Traefik static config the proxy runs with: the
// user's proxy.config, or the one hl generates.
func (c *Config) ProxyStaticFile() string {
	if c.Proxy.Config != "" {
		return c.HomePath(c.Proxy.Config)
	}
	return filepath.Join(c.ProxyDir(), TraefikStaticFile)
}

// ProxyDynamicFile is proxy.dynamic as an absolute path, "" when unset.
func (c *Config) ProxyDynamicFile() string {
	if c.Proxy.Dynamic == "" {
		return ""
	}
	return c.HomePath(c.Proxy.Dynamic)
}

// checkDynamicPath requires proxy.dynamic in a subfolder of the homelab other
// than data/ and the generated folder: hl mounts the file's whole folder into
// the proxy, which must not be the homelab root or app data.
func (c *Config) checkDynamicPath() error {
	f := c.ProxyDynamicFile()
	dir := filepath.Dir(f)
	fix := fmt.Sprintf("hl mounts the file's folder into the proxy, so keep it in a folder of its own, e.g. proxy.dynamic: %s", ExampleDynamicFile)
	rel, err := filepath.Rel(c.Dir, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("proxy.dynamic: %s is outside the homelab folder %s - %s", f, c.Dir, fix)
	}
	if rel == "." {
		return fmt.Errorf("proxy.dynamic: %s is in the homelab folder itself - %s", f, fix)
	}
	for _, d := range []string{filepath.Dir(c.DataDir(ReservedAppName)), filepath.Dir(c.ProxyDir())} {
		if r, err := filepath.Rel(d, dir); err == nil && (r == "." || !strings.HasPrefix(r, "..")) {
			return fmt.Errorf("proxy.dynamic: %s is under %s, which hl manages - %s", f, d, fix)
		}
	}
	return nil
}
