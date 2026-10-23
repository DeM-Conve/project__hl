package gen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/DeM-Conve/project__hl/internal/config"
)

// ProxyFiles are the config files the proxy container mounts.
type ProxyFiles struct {
	Static  string // host path of the Traefik static config (generated or proxy.config)
	Hash    string // of the static config's content, so a change recreates the container
	Dynamic string // host path of proxy.dynamic, "" when unset
	Ping    bool   // the static config enables ping, so the container gets a healthcheck
}

// Dashboard reports whether the Traefik dashboard is on. It is the one switch
// for both the api section of traefik.yml and the dashboard router's labels.
func Dashboard(p config.Proxy) bool { return true }

// DynamicTarget is where proxy.dynamic is seen inside the container: its
// folder is mounted read-only, so Traefik's watch survives editors that
// replace the file.
func DynamicTarget(hostFile string) string {
	return path.Join(config.TraefikDynamicDir, filepath.Base(hostFile))
}

// Hash is a short content hash for StaticConfigLabel.
func Hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])[:16]
}

// Traefik is the static config hl generates from hl.yml: entrypoints, the
// Docker provider, the file provider for proxy.dynamic, the dashboard, ping
// for the healthcheck, and logs. dynamic is the host path of proxy.dynamic.
func Traefik(p config.Proxy, dynamic string) string {
	var b strings.Builder
	b.WriteString("# Traefik static config. " + header[2:])
	b.WriteString("# To run Traefik with a file of your own instead, set proxy.config in hl.yml.\n\n")

	b.WriteString("entryPoints:\n")
	fmt.Fprintf(&b, "  %s:            # every app's router uses this entrypoint\n", config.EntryPointWeb)
	fmt.Fprintf(&b, "    address: %s\n", quote(":"+strconv.Itoa(config.ProxyContainerPort)))
	fmt.Fprintf(&b, "  %s:        # /ping for the container healthcheck; not published\n", config.EntryPointPing)
	fmt.Fprintf(&b, "    address: %s\n", quote(":"+strconv.Itoa(config.PingPort)))

	b.WriteString("\nproviders:\n  docker:             # apps, by the labels hl gives them\n")
	b.WriteString("    exposedByDefault: false\n")
	fmt.Fprintf(&b, "    network: %s\n", quote(p.Network))
	if dynamic != "" {
		b.WriteString("  file:               # proxy.dynamic: routes that aren't hl apps\n")
		fmt.Fprintf(&b, "    filename: %s\n    watch: true\n", quote(DynamicTarget(dynamic)))
	}

	if Dashboard(p) {
		fmt.Fprintf(&b, "\napi:                  # the dashboard, at http://%s%s\n  dashboard: true\n", config.DashboardHostPrefix, p.Domain)
	}

	fmt.Fprintf(&b, "\nping:\n  entryPoint: %s\n", quote(config.EntryPointPing))

	level := p.LogLevel
	if level == "" {
		level = config.DefaultProxyLogLevel
	}
	fmt.Fprintf(&b, "\nlog:\n  level: %s\n", quote(strings.ToUpper(level)))
	if p.AccessLog {
		fmt.Fprintf(&b, "\naccessLog:            # proxy.access_log; see hl proxy logs\n  format: %s\n", quote(config.AccessLogFormat))
	}
	return b.String()
}

// CheckTraefik looks at a static config the user supplies (proxy.config) for
// what hl needs. problems keep the proxy from working with hl; warnings don't.
// ping reports whether the file enables ping (needed for the healthcheck).
func CheckTraefik(data []byte, p config.Proxy, dynamic string) (ping bool, problems, warnings []string) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, []string{"not valid YAML: " + err.Error()}, nil
	}
	port := strconv.Itoa(config.ProxyContainerPort)
	web := config.EntryPointWeb

	addr, ok := lookup(doc, "entryPoints", web, "address")
	switch {
	case !ok:
		problems = append(problems, fmt.Sprintf("no entryPoints.%s - add:  entryPoints: {%s: {address: \":%s\"}}", web, web, port))
	case !strings.HasSuffix(strings.TrimSuffix(fmt.Sprint(addr), "/tcp"), ":"+port):
		problems = append(problems, fmt.Sprintf("entryPoints.%s.address is %q - hl publishes the container's port %s, use \":%s\"", web, fmt.Sprint(addr), port, port))
	}

	if _, ok := lookup(doc, "providers", "docker"); !ok {
		problems = append(problems, fmt.Sprintf("no providers.docker - add:  providers: {docker: {exposedByDefault: false, network: %s}}", p.Network))
	} else {
		if n, ok := lookup(doc, "providers", "docker", "network"); !ok || fmt.Sprint(n) != p.Network {
			got := "not set"
			if ok {
				got = fmt.Sprintf("%q", fmt.Sprint(n))
			}
			problems = append(problems, fmt.Sprintf("providers.docker.network is %s - it must be %q (proxy.network)", got, p.Network))
		}
		if e, ok := lookup(doc, "providers", "docker", "exposedByDefault"); !ok || fmt.Sprint(e) != "false" {
			warnings = append(warnings, "providers.docker.exposedByDefault is not false - Traefik will also route containers hl didn't label")
		}
	}

	if dynamic != "" {
		want := DynamicTarget(dynamic)
		f, _ := lookup(doc, "providers", "file", "filename")
		d, _ := lookup(doc, "providers", "file", "directory")
		if fmt.Sprint(f) != want && strings.TrimSuffix(fmt.Sprint(d), "/") != config.TraefikDynamicDir {
			problems = append(problems, fmt.Sprintf("proxy.dynamic is set but providers.file doesn't read it - add:  providers: {file: {filename: %s, watch: true}}", want))
		}
	}

	if Dashboard(p) {
		if v, ok := lookup(doc, "api", "dashboard"); !ok || fmt.Sprint(v) != "true" {
			warnings = append(warnings, fmt.Sprintf("the dashboard at %s%s needs  api: {dashboard: true}", config.DashboardHostPrefix, p.Domain))
		}
	}

	_, ping = lookup(doc, "ping")
	if !ping {
		warnings = append(warnings, "no ping section - the proxy gets no healthcheck; add  ping: {}")
	}
	return ping, problems, warnings
}

// lookup finds a nested key the way Traefik does: case-insensitively. A key
// present with an empty value (`ping:`) counts as present.
func lookup(m map[string]any, keys ...string) (any, bool) {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		found := false
		for mk, v := range mm {
			if strings.EqualFold(mk, k) {
				cur, found = v, true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return cur, true
}

// ImageOlder reports whether a Traefik image's tag is a version older than
// tested. known is false when the tag isn't a version (latest, a digest, another
// image); a tag without a patch (v3.6) is compared on the parts it has.
func ImageOlder(image, tested string) (older, known bool) {
	ref := image
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	name, tag := ref, ""
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		name, tag = ref[:i], ref[i+1:]
	}
	if name != "traefik" && !strings.HasSuffix(name, "/traefik") {
		return false, false
	}
	have, ok := versionParts(tag)
	if !ok {
		return false, false
	}
	want, ok := versionParts(tested)
	if !ok {
		return false, false
	}
	for i := range have {
		if i >= len(want) || have[i] != want[i] {
			return i < len(want) && have[i] < want[i], true
		}
	}
	return false, true
}

func versionParts(v string) ([]int, bool) {
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return nil, false
	}
	var parts []int
	for _, s := range strings.Split(v, ".") {
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, false
		}
		parts = append(parts, n)
	}
	return parts, len(parts) <= 3
}
