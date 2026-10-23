package cli

import (
	"fmt"
	"os"

	"github.com/DeM-Conve/project__hl/internal/config"
)

// upgradeConfig brings hl.yml to the current format before hl edits it, and says so: an older
// file keeps working as it is, but is never changed silently.
func upgradeConfig(dir string) error {
	from, err := config.Upgrade(dir)
	if err != nil {
		return err
	}
	if from != config.Version {
		fmt.Fprintf(os.Stderr, "%snote:%s upgraded %s to version %d (added `version: %d` at the top; nothing else changed)\n",
			yellow, reset, config.FileName, config.Version, config.Version)
	}
	return nil
}
