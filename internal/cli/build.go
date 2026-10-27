package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/DeM-Conve/project__hl/internal/compose"
	"github.com/DeM-Conve/project__hl/internal/config"
	"github.com/DeM-Conve/project__hl/internal/defaults"
	"github.com/DeM-Conve/project__hl/internal/fsutil"
)

// Build settings: many apps bake their base path or URL into the image as a build arg (fed from
// ${HL_PREFIX} or ${HL_URL}), and `docker compose up` doesn't rebuild an image that exists. hl
// records a hash of each service's resolved build section after `up` and rebuilds the services
// whose hash changed since.

func buildStateFile(cfg *config.Config, name string) string {
	return filepath.Join(cfg.StateDir(name), defaults.BuildStateFile)
}

// staleBuilds are the services of the app whose build settings differ from the ones recorded
// when it last came up, sorted. Nothing is stale before anything was recorded: the first `up`
// builds what is missing, as compose does.
func staleBuilds(cfg *config.Config, name string, m *compose.Model) []string {
	b, err := os.ReadFile(buildStateFile(cfg, name))
	if err != nil {
		return nil
	}
	var old map[string]string
	if json.Unmarshal(b, &old) != nil {
		return nil
	}
	var stale []string
	for svc, h := range m.BuildHashes() {
		if was, ok := old[svc]; ok && was != h {
			stale = append(stale, svc)
		}
	}
	sort.Strings(stale)
	return stale
}

// recordBuilds remembers the build settings the app's images now have. An app without a
// build section has nothing to record.
func recordBuilds(cfg *config.Config, name string, m *compose.Model) error {
	h := m.BuildHashes()
	f := buildStateFile(cfg, name)
	if len(h) == 0 {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFile(f, append(b, '\n'), defaults.FileMode)
}
