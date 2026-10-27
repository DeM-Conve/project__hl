package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// HostTimeZone is this machine's time zone (an IANA name like Europe/Berlin) and where hl
// read it, or "" when it can't tell. Tests replace it.
var HostTimeZone = hostTimeZone

// hostTimeZone reads $TZ, else the target of /etc/localtime (Linux and macOS link it into a
// zoneinfo folder), else /etc/timezone (Debian and Ubuntu).
func hostTimeZone() (zone, source string) {
	// A TZ starting with ':' names a file, not a zone; a POSIX rule (CET-1CEST) has no '/'
	// but is still something the C library in a container understands.
	if tz := os.Getenv(defaults.TZEnv); tz != "" && !strings.HasPrefix(tz, ":") {
		return tz, "host ($" + defaults.TZEnv + ")"
	}
	if target, err := filepath.EvalSymlinks(defaults.LocalTimeFile); err == nil {
		if _, zone, ok := strings.Cut(filepath.ToSlash(target), defaults.ZoneInfoDir+"/"); ok && zone != "" {
			return zone, "host (" + defaults.LocalTimeFile + ")"
		}
	}
	if b, err := os.ReadFile(defaults.TimeZoneFile); err == nil {
		if zone := strings.TrimSpace(string(b)); zone != "" && !strings.ContainsAny(zone, " \t\n") {
			return zone, "host (" + defaults.TimeZoneFile + ")"
		}
	}
	return "", ""
}

// hostTZ is the TZ hl adds for an app, and its source: the host's time zone, unless the
// shared env, the app's env or the app's own .env already sets TZ.
func (c *Config) hostTZ(name string, a *App) (zone, source string) {
	if _, ok := c.Env[defaults.TZEnv]; ok {
		return "", ""
	}
	if _, ok := a.Env[defaults.TZEnv]; ok {
		return "", ""
	}
	if setsVar(filepath.Join(c.AppDir(a), defaults.AppEnvFile), defaults.TZEnv) {
		return "", ""
	}
	return HostTimeZone()
}

// setsVar reports whether the env file at path has a line setting key (KEY=..., or
// export KEY=...). A file that can't be read sets nothing.
func setsVar(path, key string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		if k, _, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == key {
			return true
		}
	}
	return false
}
