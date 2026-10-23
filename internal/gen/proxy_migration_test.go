package gen

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// legacyProxy is the proxy compose file hl generated before the static config
// file (Traefik configured by command-line flags), kept to prove the new
// setup is the same proxy for an existing homelab.
func legacyProxy(p config.Proxy) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("name: hl-proxy\n\nservices:\n  traefik:\n")
	fmt.Fprintf(&b, "    image: %s\n", quote(p.Image))
	b.WriteString("    restart: unless-stopped\n    command:\n")
	for _, c := range []string{
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--providers.docker.network=" + p.Network,
		"--entrypoints.web.address=:80",
		"--api.dashboard=true",
	} {
		fmt.Fprintf(&b, "      - %s\n", quote(c))
	}
	fmt.Fprintf(&b, "    ports:\n      - %s\n", quote(fmt.Sprintf("%s:%d:80", p.Bind, p.Port)))
	b.WriteString("    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock:ro\n")
	b.WriteString("    networks: [proxy]\n    labels:\n")
	b.WriteString("      traefik.enable: 'true'\n")
	fmt.Fprintf(&b, "      traefik.http.routers.hl-dashboard.rule: %s\n", quote("Host(`traefik."+p.Domain+"`)"))
	b.WriteString("      traefik.http.routers.hl-dashboard.service: api@internal\n")
	fmt.Fprintf(&b, "\nnetworks:\n  proxy:\n    name: %s\n", quote(p.Network))
	if p.Subnet != "" {
		fmt.Fprintf(&b, "    ipam:\n      config:\n        - subnet: %s\n", quote(p.Subnet))
	}
	return b.String()
}

// flatten turns a YAML tree into lowercased dotted keys -> values, the shape
// of Traefik's command-line flags. An empty section (ping: {}) is "key=true".
func flatten(prefix string, v any, out map[string]string) {
	m, ok := v.(map[string]any)
	if !ok {
		out[prefix] = strings.ToLower(fmt.Sprint(v))
		return
	}
	if prefix != "" {
		out[prefix] = "true" // a section that is present is enabled
	}
	for k, sub := range m {
		key := strings.ToLower(k)
		if prefix != "" {
			key = prefix + "." + key
		}
		flatten(key, sub, out)
	}
}

func parse(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("%v\n%s", err, s)
	}
	return m
}

// An existing homelab gets the same proxy after upgrading: the same network
// (byte for byte, so compose keeps it and its subnet), port, restart policy,
// dashboard router, and every Traefik setting the old flags made, with only
// ping (healthcheck) and the explicit default log level added.
func TestGeneratedProxyEquivalentToLegacy(t *testing.T) {
	for _, p := range []config.Proxy{
		proxySettings(),
		{Bind: "0.0.0.0", Port: 8080, Domain: "lab.example", Network: "hl-proxy", Subnet: "10.123.0.0/16", Image: "traefik:v3.6", Project: defaults.DefaultProxyProject},
		{Bind: "127.0.0.1", Port: 80, Domain: "localhost", Network: "my-net", Image: "traefik:v3.6", Project: defaults.DefaultProxyProject},
	} {
		// The legacy proxy always had the dashboard; it is now proxy.dashboard.
		p.Dashboard = true
		static := Traefik(p, "")
		newC := Proxy(p, ProxyFiles{Static: "/lab/.hl/proxy/traefik.yml", Hash: Hash([]byte(static)), Ping: true})
		oldC := legacyProxy(p)

		// Networks: the exact same text, so compose sees the same network config.
		if a, b := oldC[strings.Index(oldC, "\nnetworks:"):], newC[strings.Index(newC, "\nnetworks:"):]; a != b {
			t.Errorf("networks section changed:\nold:%s\nnew:%s", a, b)
		}

		o, n := parse(t, oldC), parse(t, newC)
		if o["name"] != n["name"] {
			t.Errorf("project name %v -> %v", o["name"], n["name"])
		}
		os, ns := o["services"].(map[string]any)["traefik"].(map[string]any), n["services"].(map[string]any)["traefik"].(map[string]any)
		for _, k := range []string{"image", "restart", "ports", "networks"} {
			if !reflect.DeepEqual(os[k], ns[k]) {
				t.Errorf("%s: %v -> %v", k, os[k], ns[k])
			}
		}
		ol, nl := os["labels"].(map[string]any), ns["labels"].(map[string]any)
		for k, v := range ol {
			if nl[k] != v {
				t.Errorf("label %s: %v -> %v", k, v, nl[k])
			}
		}
		if len(nl) != len(ol)+1 || nl[defaults.StaticConfigLabel] == nil {
			t.Errorf("labels: only %s may be added: %v", defaults.StaticConfigLabel, nl)
		}
		vols := ns["volumes"].([]any)
		if !reflect.DeepEqual(vols[0], os["volumes"].([]any)[0]) || len(vols) != 2 {
			t.Errorf("volumes: the socket plus the static config only: %v", vols)
		}

		// Traefik settings: every old flag is in the file with the same value.
		flags := map[string]string{}
		for _, c := range os["command"].([]any) {
			kv := strings.SplitN(strings.TrimPrefix(c.(string), "--"), "=", 2)
			flags[strings.ToLower(kv[0])] = strings.ToLower(kv[1])
		}
		file := map[string]string{}
		flatten("", parse(t, static), file)
		for k, v := range flags {
			if file[k] != v {
				t.Errorf("flag --%s=%s is %q in traefik.yml", k, v, file[k])
			}
		}
		// Anything else may only be a section that holds an old flag, or ping
		// (healthcheck) and log (Traefik's default level, made explicit).
		allowed := map[string]bool{"entrypoints.traefik": true, "entrypoints.traefik.address": true,
			"ping": true, "ping.entrypoint": true, "log": true, "log.level": true}
		for k := range file {
			if _, ok := flags[k]; ok || allowed[k] {
				continue
			}
			holds := false
			for f := range flags {
				holds = holds || strings.HasPrefix(f, k+".")
			}
			if !holds || file[k] != "true" {
				t.Errorf("traefik.yml sets %s=%s, which the old flags didn't", k, file[k])
			}
		}
		if file["log.level"] != "error" {
			t.Errorf("log level %q; Traefik's default (what the old flags gave) is error", file["log.level"])
		}
	}
}
