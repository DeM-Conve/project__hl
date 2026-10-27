package config

import (
	"path/filepath"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// removedKeys are hl.yml keys earlier builds took, by path ("*" for an app's name), with what
// to do instead. Check reports each with this message rather than as an unknown key, and
// `hl unset` removes them.
var removedKeys = map[string]string{
	"proxy.network": "the homelab's name gives the proxy network its own name (" + defaults.Prefix + "<name>-" + defaults.NetworkSuffix +
		"): `hl unset proxy network`, then `hl set name=<x>` if another homelab uses " + defaults.DefaultProxyNetwork,
	"proxy.project": "the homelab's name gives the proxy project its own name (" + defaults.Prefix + "<name>-" + defaults.ProxyProjectSuffix +
		"): `hl unset proxy project`, then `hl set name=<x>` if another homelab uses " + defaults.DefaultProxyProject,
	"proxy.config":           "hl always generates the Traefik config (.hl/proxy/traefik.yml); remove it: `hl unset proxy config`",
	"proxy.dynamic":          "hl no longer loads a Traefik dynamic config file; remove it: `hl unset proxy dynamic`",
	"proxy.dashboard":        "the Traefik dashboard (it had no password) is gone; remove it: `hl unset proxy dashboard`",
	"proxy.dashboard_public": "the Traefik dashboard is gone; remove it: `hl unset proxy dashboard_public`",
	"proxy.access_log":       "`hl set proxy debug=true` turns on Traefik's access log and INFO log; remove it: `hl unset proxy access_log`",
	"proxy.log_level":        "`hl set proxy debug=true` turns on Traefik's INFO log and access log; remove it: `hl unset proxy log_level`",
	"scan_depth": "hl init and hl sync look one level into each projects folder; list a nested group folder in projects instead " +
		"(`hl set projects+=<folder>`); remove it: `hl unset scan_depth`",
	"apps.*.prefix": "path routing serves an app at /<app>; `hl set <app> name=<new>` changes it (or `hl set <app> routing=host`); " +
		"remove it: `hl unset <app> prefix`",
	"apps.*.env_files": "keep secrets in the app's own " + defaults.AppEnvFile + " (hl reads it) and the rest in its env " +
		"(`hl set <app> env.NAME=value`); remove it: `hl unset <app> env_files`",
}

// removedAppKeys are the removedKeys of an app, without the "apps.*." in front.
func removedAppKeys() []string {
	var out []string
	for k := range removedKeys {
		if f, ok := strings.CutPrefix(k, "apps.*."); ok {
			out = append(out, f)
		}
	}
	return out
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
