package scan

import (
	"sort"
	"strings"

	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// AutoEnv is the env hl fills in for an app from the variables its compose files use, by the
// rules in internal/defaults: <X>_BASE_PATH -> ${HL_PREFIX} (path routing only), <X>_URL ->
// ${HL_URL} when <X>_BASE_PATH exists too (not with routing none), and the names in
// defaults.EnvByName. Nil when nothing matches.
func AutoEnv(vars []string, routing string) map[string]string {
	have := map[string]bool{}
	for _, v := range vars {
		have[v] = true
	}
	out := map[string]string{}
	for _, v := range vars {
		if val, ok := defaults.EnvByName[v]; ok {
			out[v] = val
			continue
		}
		x, ok := strings.CutSuffix(v, defaults.EnvBasePathSuffix)
		if !ok || x == "" {
			continue
		}
		if routing == config.RoutingPath {
			out[v] = defaults.EnvBasePathValue
		}
		if u := x + defaults.EnvURLSuffix; have[u] && routing != config.RoutingNone {
			out[u] = defaults.EnvURLValue
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EnvList is env as sorted KEY=VALUE pairs, for display.
func EnvList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
