// Package runner provides the CommandRunner interface and its real implementation.
package runner

import (
	"context"
	"os"
	"os/exec"

	"github.com/rotisserie/eris"
)

// CommandRunner is the interface for executing external commands.
type CommandRunner interface {
	// Run executes a command, streaming stdout/stderr/stdin to the user's terminal.
	Run(ctx context.Context, name string, args ...string) error
	// Output executes a command and returns its stdout. Stderr is passed to os.Stderr.
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Real is the real implementation of CommandRunner that uses os/exec.
type Real struct{}

// NewReal returns a Real CommandRunner.
func NewReal() *Real {
	return &Real{}
}

// Run executes a command, streaming output to the user's terminal.
func (r *Real) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return eris.Wrapf(err, "running %q", name)
	}

	return nil
}

// Output executes a command and returns its stdout. Stderr is forwarded to os.Stderr.
func (r *Real) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, eris.Wrapf(err, "running %q", name)
	}

	return out, nil
}
