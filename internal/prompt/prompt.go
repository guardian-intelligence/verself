// Package prompt provides a shared Prompter interface and TTY implementation
// for interactive CLI wizards.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// Prompter abstracts stdin/stdout for testability.
type Prompter interface {
	Ask(prompt string) string
	AskWithDefault(prompt, current string) string
	AskSecret(prompt string) string
	Confirm(prompt string) bool
	Select(prompt string, options []string) (int, string)
}

// TTYPrompter reads from stdin/stdout.
type TTYPrompter struct {
	In  io.Reader
	Out io.Writer
}

func (p *TTYPrompter) Ask(prompt string) string {
	fmt.Fprint(p.Out, prompt)
	scanner := bufio.NewScanner(p.In)
	if scanner.Scan() {
		return scanner.Text()
	}
	return ""
}

// AskWithDefault shows the current value in brackets. Enter keeps it.
func (p *TTYPrompter) AskWithDefault(prompt, current string) string {
	var input string
	if current != "" {
		input = p.Ask(fmt.Sprintf("%s [%s]: ", prompt, current))
	} else {
		input = p.Ask(prompt + ": ")
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return current
	}
	return input
}

// AskSecret prompts for input with terminal echo disabled.
// Falls back to plain Ask if stdin is not a terminal.
func (p *TTYPrompter) AskSecret(prompt string) string {
	fmt.Fprint(p.Out, prompt)

	if f, ok := p.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(p.Out) // newline after hidden input
		if err != nil {
			return ""
		}
		return string(b)
	}

	// Not a terminal (pipe, test) — read normally.
	scanner := bufio.NewScanner(p.In)
	if scanner.Scan() {
		return scanner.Text()
	}
	return ""
}

func (p *TTYPrompter) Confirm(prompt string) bool {
	answer := p.Ask(prompt + " [Y/n]: ")
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "" || answer == "y" || answer == "yes"
}

// Select presents a numbered list and returns the chosen index and value.
func (p *TTYPrompter) Select(prompt string, options []string) (int, string) {
	fmt.Fprintln(p.Out, prompt)
	for i, opt := range options {
		fmt.Fprintf(p.Out, "  %d) %s\n", i+1, opt)
	}
	fmt.Fprintln(p.Out)

	for {
		input := p.Ask(fmt.Sprintf("Enter choice [1-%d]: ", len(options)))
		input = strings.TrimSpace(input)

		if input == "" {
			return 0, options[0]
		}

		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(options) {
			fmt.Fprintf(p.Out, "  Please enter a number between 1 and %d.\n", len(options))
			continue
		}
		return n - 1, options[n-1]
	}
}
