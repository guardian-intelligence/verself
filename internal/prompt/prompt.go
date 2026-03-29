// Package prompt provides a shared Prompter interface and TTY implementation
// for interactive CLI wizards.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Prompter abstracts stdin/stdout for testability.
type Prompter interface {
	Ask(prompt string) string
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

func (p *TTYPrompter) AskSecret(prompt string) string {
	return p.Ask(prompt)
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
