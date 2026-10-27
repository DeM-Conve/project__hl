package gen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// ProxyFiles are the config files the proxy container mounts.
type ProxyFiles struct {
	Static string // host path of the generated Traefik static config
	Hash   string // of the static config's content, so a change recreates the container
}

// Hash is a short content hash for StaticConfigLabel.
func Hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])[:16]
}

// Traefik is the static config hl generates from hl.yml: entrypoints, the Docker provider, ping
// for the healthcheck, and logs (INFO and an access log with proxy.debug).
func Traefik(p config.Proxy) string {
	var b strings.Builder
	b.WriteString("# Traefik static config. " + header[2:] + "\n")

	b.WriteString("entryPoints:\n")
	fmt.Fprintf(&b, "  %s:            # every app's router uses this entrypoint\n", defaults.EntryPointWeb)
	fmt.Fprintf(&b, "    address: %s\n", quote(":"+strconv.Itoa(defaults.ProxyContainerPort)))
	fmt.Fprintf(&b, "  %s:        # /ping for the container healthcheck; not published\n", defaults.EntryPointPing)
	fmt.Fprintf(&b, "    address: %s\n", quote(":"+strconv.Itoa(defaults.PingPort)))

	b.WriteString("\nproviders:\n  docker:             # apps, by the labels hl gives them\n")
	b.WriteString("    exposedByDefault: false\n")
	fmt.Fprintf(&b, "    network: %s\n", quote(p.Network))

	fmt.Fprintf(&b, "\nping:\n  entryPoint: %s\n", quote(defaults.EntryPointPing))

	level := defaults.ProxyLogLevel
	if p.Debug {
		level = defaults.DebugProxyLogLevel
	}
	fmt.Fprintf(&b, "\nlog:\n  level: %s\n", quote(level))
	if p.Debug {
		fmt.Fprintf(&b, "\naccessLog:            # proxy.debug; see hl logs proxy\n  format: %s\n", quote(defaults.AccessLogFormat))
	}
	return b.String()
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
