package cli

import (
	"fmt"
	"os"
)

// Colours only on a terminal, and never with NO_COLOR set.
var bold, dim, red, green, yellow, cyan, reset = "", "", "", "", "", "", ""

func init() {
	if os.Getenv("NO_COLOR") != "" {
		return
	}
	if fi, err := os.Stdout.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return
	}
	bold, dim, red, green, yellow, cyan, reset = "\x1b[1m", "\x1b[2m", "\x1b[31m", "\x1b[32m", "\x1b[33m", "\x1b[36m", "\x1b[0m"
}

func say(format string, a ...any) { fmt.Printf(format+"\n", a...) }

// step heads the output of one app's compose command.
func step(what string) { fmt.Printf("%s==>%s %s%s%s\n", cyan, reset, bold, what, reset) }
