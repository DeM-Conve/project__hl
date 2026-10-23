package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

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

// homeDir is the homelab folder without loading hl.yml, for commands that must work on a
// broken hl.yml (checking or fixing it).
func homeDir() (string, error) { return config.Home(homeFlag) }

func configCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Check hl.yml: every problem with its line number and how to fix it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := homeDir()
			if err != nil {
				return err
			}
			path := filepath.Join(home, config.FileName)
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if ps := config.Check(b); len(ps) > 0 {
				for _, p := range ps {
					say("%s", p.In(path))
				}
				return fmt.Errorf("%s in %s", plural(len(ps), "problem", "problems"), path)
			}
			say("%s✓%s %s is fine", green, reset, path)
			if config.FileVersion(b) == 0 {
				say("%snote:%s it has no `version:` line (the format before versions) - hl adds `version: %d` the next time it changes the file",
					yellow, reset, config.Version)
			}
			return nil
		},
	}
}
