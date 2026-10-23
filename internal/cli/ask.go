package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// input is where questions are answered from; tests replace it.
var input io.Reader = os.Stdin

// stdinIsTerminal reports whether someone can answer a question; tests replace it.
// (A character-device check isn't enough: /dev/null is one too.)
var stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// errNoAnswer is returned when the input ends before a question is answered.
var errNoAnswer = errors.New("no answer")

func readLine() (string, error) {
	// One byte at a time, so nothing after the line is consumed from a shared input.
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := input.Read(buf)
		if n == 1 {
			if buf[0] == '\n' {
				return strings.TrimSpace(b.String()), nil
			}
			b.WriteByte(buf[0])
			continue
		}
		if err != nil {
			if b.Len() > 0 {
				return strings.TrimSpace(b.String()), nil
			}
			return "", errNoAnswer
		}
	}
}

// choose asks to pick one of options by number; Enter picks def (an index into options).
func choose(question string, options []string, def int) (int, error) {
	say("%s", question)
	for i, o := range options {
		mark := ""
		if i == def {
			mark = dim + "  (default)" + reset
		}
		say("  %d) %s%s", i+1, o, mark)
	}
	for {
		fmt.Printf("choose [%d]: ", def+1)
		line, err := readLine()
		if err != nil {
			return 0, err
		}
		if line == "" {
			return def, nil
		}
		if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		for i, o := range options {
			if line == o || strings.HasPrefix(o, line+" ") {
				return i, nil
			}
		}
		say("pick a number from 1 to %d", len(options))
	}
}

// confirm asks a yes/no question; Enter answers def.
func confirm(question string, def bool) (bool, error) {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for {
		fmt.Printf("%s %s ", question, hint)
		line, err := readLine()
		if err != nil {
			return false, err
		}
		switch strings.ToLower(line) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}
