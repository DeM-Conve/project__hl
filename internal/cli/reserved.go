package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DeM-Conve/project__hl/internal/config"
)

// `hl config <app>` shares its first argument with the config subcommands and `proxy`, so
// none of those can be an app's name. They are taken from the command as registered, never
// from a second list, and recorded in config's one list of reserved names.
func init() { reserveConfigArgs(configCmd()) }

func reserveConfigArgs(c *cobra.Command) {
	config.ReserveAppName(proxyArg, fmt.Sprintf("`hl config %s` shows the proxy's config", proxyArg))
	for _, sub := range c.Commands() {
		for _, n := range append([]string{sub.Name()}, sub.Aliases...) {
			config.ReserveAppName(n, fmt.Sprintf("`hl config %s` is a command, so `hl config <app>` couldn't show this app", n))
		}
	}
}
