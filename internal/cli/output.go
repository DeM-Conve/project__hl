package cli

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/DeM-Conve/project__hl/internal/defaults"
)

// Colours only on a terminal, and never with NO_COLOR set.
var bold, dim, red, green, yellow, cyan, reset string

func setColors(w io.Writer) {
	bold, dim, red, green, yellow, cyan, reset = "", "", "", "", "", "", ""
	if os.Getenv(defaults.NoColorEnv) != "" {
		return
	}
	f, ok := w.(*os.File)
	// Not a character-device check: /dev/null is one too.
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return
	}
	bold, dim, red, green, yellow, cyan, reset = "\x1b[1m", "\x1b[2m", "\x1b[31m", "\x1b[32m", "\x1b[33m", "\x1b[36m", "\x1b[0m"
}

func say(format string, a ...any) { fmt.Fprintf(stdout, format+"\n", a...) }

// warn is a non-fatal problem, on stderr.
func warn(format string, a ...any) {
	fmt.Fprintf(stderr, "%swarning:%s %s\n", yellow, reset, fmt.Sprintf(format, a...))
}

// step heads the output of one app's compose command.
func step(what string) { fmt.Fprintf(stdout, "%s==>%s %s%s%s\n", cyan, reset, bold, what, reset) }
