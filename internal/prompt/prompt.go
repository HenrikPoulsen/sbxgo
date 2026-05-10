// Package prompt provides the Prompter interface and its terminal implementation.
package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/rotisserie/eris"
)

// Prompter is the interface for interactive user prompts.
type Prompter interface {
	// Confirm asks the user a yes/no question. The defaultYes flag selects
	// which answer is returned when the user just presses enter, and controls
	// the [Y/n] vs [y/N] hint shown alongside the question.
	Confirm(question string, defaultYes bool) (bool, error)
}

// Terminal is the real Prompter that reads from stdin.
type Terminal struct{}

// NewTerminal returns a Terminal Prompter.
func NewTerminal() *Terminal {
	return &Terminal{}
}

// Confirm asks a yes/no question and returns the user's answer. An empty
// response falls back to defaultYes.
func (t *Terminal) Confirm(question string, defaultYes bool) (bool, error) {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}

	fmt.Printf("%s %s ", question, hint)

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, eris.Wrap(err, "reading confirmation")
		}

		return defaultYes, nil
	}

	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))

	if answer == "" {
		return defaultYes, nil
	}

	return answer == "y" || answer == "yes", nil
}
