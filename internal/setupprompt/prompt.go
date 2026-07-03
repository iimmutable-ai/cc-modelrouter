// Package setupprompt provides minimal interactive helpers for ccrouter's
// non-TUI setup commands. Output goes to stdout; input is read from stdin
// (or an injected reader for tests). Passwords are read via golang.org/x/term
// so they never echo to the terminal or scrollback.
package setupprompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// Prompt drives a sequence of questions. All input is line-oriented except
// for AskPassword, which reads from the terminal FD in raw mode.
//
// The reader is held in a single *bufio.Reader across all calls. Creating
// a fresh scanner-per-call would lose data whenever the underlying io.Reader
// returned more than one line per Read() — which is exactly what happens
// when stdin is piped, or in tests using strings.NewReader. The kernel
// hides this on a real TTY by line-buffering, but that's no excuse.
type Prompt struct {
	In  io.Reader
	Out io.Writer

	br      *bufio.Reader
	seenEOF bool
}

// New returns a Prompt bound to stdin/stdout.
func New() *Prompt {
	return &Prompt{In: os.Stdin, Out: os.Stdout}
}

// EOF reports whether the underlying input has hit end-of-file. Once true,
// every subsequent Ask* call returns its default without prompting. Loops
// in callers should check this to avoid spinning forever on EOF.
func (p *Prompt) EOF() bool {
	return p.seenEOF
}

// reader lazily attaches a buffered reader to In. Once created, the same
// reader is reused for every subsequent prompt so buffered-but-unconsumed
// bytes are not lost between calls.
func (p *Prompt) reader() *bufio.Reader {
	if p.br == nil {
		p.br = bufio.NewReader(p.In)
	}
	return p.br
}

// readLine returns one line from the input without the trailing newline.
// On EOF with no data, returns ("", false) and marks the Prompt as EOF.
func (p *Prompt) readLine() (string, bool) {
	if p.seenEOF {
		return "", false
	}
	line, err := p.reader().ReadString('\n')
	if err != nil {
		// io.EOF with no remaining bytes: real EOF. With bytes: the final
		// line lacked a trailing newline; treat that as a valid line and
		// only mark EOF when ReadString returns an error AND no bytes.
		if line == "" {
			p.seenEOF = true
			return "", false
		}
	}
	return strings.TrimRight(line, "\r\n"), true
}

func (p *Prompt) writef(format string, args ...interface{}) {
	fmt.Fprintf(p.Out, format, args...)
}

// AskString shows label + default, returns the user's input (default on empty).
func (p *Prompt) AskString(label, def string) string {
	if def != "" {
		p.writef("%s [%s]: ", label, def)
	} else {
		p.writef("%s: ", label)
	}
	line, ok := p.readLine()
	if !ok {
		return def
	}
	if strings.TrimSpace(line) == "" {
		return def
	}
	return line
}

// AskPassword reads a secret from the terminal with echo disabled. If input
// is not a TTY (e.g. piped for tests), it falls back to a plain line read so
// the prompt remains testable. The returned value is never logged by this
// package — callers must avoid %v on header maps and use logging.Sanitize*.
func (p *Prompt) AskPassword(label string) string {
	p.writef("%s: ", label)
	if f, ok := p.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		bytes, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(p.Out)
		if err == nil {
			return strings.TrimRight(string(bytes), "\r")
		}
	}
	line, _ := p.readLine()
	return line
}

// AskChoice shows a numbered menu and returns the selected index (0-based).
// Empty input selects defaultIdx. Out-of-range input re-prompts.
func (p *Prompt) AskChoice(label string, options []string, defaultIdx int) int {
	p.writef("%s\n", label)
	for i, opt := range options {
		marker := "  "
		if i == defaultIdx {
			marker = "> "
		}
		p.writef("%s%d) %s\n", marker, i+1, opt)
	}
	for {
		p.writef("Choice [%d]: ", defaultIdx+1)
		line, ok := p.readLine()
		if !ok {
			return defaultIdx
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return defaultIdx
		}
		if n, err := strconv.Atoi(line); err == nil {
			if n >= 1 && n <= len(options) {
				return n - 1
			}
		}
		lower := strings.ToLower(line)
		for i, opt := range options {
			if strings.HasPrefix(strings.ToLower(opt), lower) {
				return i
			}
		}
		p.writef("Invalid choice %q; please enter 1-%d\n", line, len(options))
	}
}

// AskYesNo shows a yes/no question. Empty input selects the default.
func (p *Prompt) AskYesNo(label string, defaultYes bool) bool {
	if defaultYes {
		p.writef("%s [Y/n]: ", label)
	} else {
		p.writef("%s [y/N]: ", label)
	}
	line, ok := p.readLine()
	if !ok {
		return defaultYes
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	}
	return defaultYes
}
