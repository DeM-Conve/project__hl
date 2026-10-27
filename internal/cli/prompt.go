package cli

// Questions on a terminal, with a default on Enter. Plain bufio, so a test
// can drive it with a string; when stdin isn't a terminal hl doesn't ask at
// all and uses flags and defaults instead.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// stdinTerminal reports whether r is a terminal; tests replace it.
var stdinTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	// Not a character-device check: /dev/null is one too.
	return term.IsTerminal(int(f.Fd()))
}

type prompter struct {
	in  *bufio.Reader
	out io.Writer
	eof bool
}

func newPrompter(in io.Reader, out io.Writer) *prompter {
	return &prompter{in: bufio.NewReader(in), out: out}
}

// line reads one answer; at end of input it returns "" (take the default).
func (p *prompter) line() string {
	if p.eof {
		return ""
	}
	s, err := p.in.ReadString('\n')
	if err != nil {
		p.eof = true
	}
	return strings.TrimSpace(s)
}

// choose asks for one of opts by number; def is the default's index.
func (p *prompter) choose(q string, opts []string, def int) int {
	fmt.Fprintf(p.out, "%s%s%s\n", bold, q, reset)
	for i, o := range opts {
		fmt.Fprintf(p.out, "  %d) %s\n", i+1, o)
	}
	for {
		fmt.Fprintf(p.out, "choice [%d]: ", def+1)
		s := p.line()
		if s == "" {
			return def
		}
		if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(opts) {
			return n - 1
		}
		fmt.Fprintf(p.out, "type a number from 1 to %d\n", len(opts))
	}
}

// confirm asks a yes/no question.
func (p *prompter) confirm(q string, def bool) bool {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	for {
		fmt.Fprintf(p.out, "%s%s%s [%s]: ", bold, q, reset, hint)
		switch strings.ToLower(p.line()) {
		case "":
			return def
		case "y", "yes":
			return true
		case "n", "no":
			return false
		}
		fmt.Fprintln(p.out, "answer y or n")
	}
}

// pickNone is the --apps value that adds no app.
const pickNone = "none"
