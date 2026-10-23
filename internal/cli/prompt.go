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
)

// stdinTerminal reports whether r is a terminal; tests replace it.
var stdinTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
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

// ask asks a free-text question.
func (p *prompter) ask(q, def string) string {
	if def != "" {
		fmt.Fprintf(p.out, "%s%s%s [%s]: ", bold, q, reset, def)
	} else {
		fmt.Fprintf(p.out, "%s%s%s: ", bold, q, reset)
	}
	if s := p.line(); s != "" {
		return s
	}
	return def
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

// pickNone is the answer to pick that selects nothing.
const pickNone = "none"

// pick is a checklist: every item is selected unless the answer lists the
// numbers to keep ("1 3" or "1,3") or says none. It returns the indexes.
func (p *prompter) pick(q string, items []string) []int {
	fmt.Fprintf(p.out, "%s%s%s\n", bold, q, reset)
	for i, it := range items {
		fmt.Fprintf(p.out, "  %d) %s\n", i+1, it)
	}
	all := make([]int, len(items))
	for i := range items {
		all[i] = i
	}
	for {
		fmt.Fprintf(p.out, "numbers to add, or %s [all]: ", pickNone)
		s := p.line()
		if s == "" || strings.EqualFold(s, "all") {
			return all
		}
		if strings.EqualFold(s, pickNone) {
			return nil
		}
		var out []int
		seen := map[int]bool{}
		ok := true
		for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
			n, err := strconv.Atoi(f)
			if err != nil || n < 1 || n > len(items) {
				ok = false
				break
			}
			if !seen[n] {
				seen[n] = true
				out = append(out, n-1)
			}
		}
		if ok {
			return out
		}
		fmt.Fprintf(p.out, "type numbers from 1 to %d, all, or %s\n", len(items), pickNone)
	}
}
