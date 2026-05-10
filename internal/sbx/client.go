// Package sbx provides wrappers around the sbx CLI tool.
package sbx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rotisserie/eris"

	"github.com/HenrikPoulsen/sbxgo/internal/runner"
)

// Sandbox represents a single entry from `sbx ls --json`.
type Sandbox struct {
	Name       string   `json:"name"`
	Agent      string   `json:"agent"`
	Status     string   `json:"status"`
	Workspaces []string `json:"workspaces"`
}

// listResponse is the top-level JSON object from `sbx ls --json`.
type listResponse struct {
	Sandboxes []Sandbox `json:"sandboxes"`
}

// Client wraps the sbx CLI.
type Client struct {
	runner  runner.CommandRunner
	debug   bool
	verbose bool
	logOut  io.Writer
}

// NewClient creates a new sbx Client.
func NewClient(r runner.CommandRunner) *Client {
	return &Client{runner: r, logOut: os.Stderr}
}

// SetDebug toggles the global --debug flag on every sbx invocation.
func (c *Client) SetDebug(debug bool) *Client {
	c.debug = debug
	return c
}

// SetVerbose toggles logging of every sbx command before it is executed.
func (c *Client) SetVerbose(verbose bool) *Client {
	c.verbose = verbose
	return c
}

// SetLogOutput overrides the destination for verbose command logs (defaults to stderr).
func (c *Client) SetLogOutput(w io.Writer) *Client {
	c.logOut = w
	return c
}

// List returns all sandboxes reported by `sbx ls --json`.
func (c *Client) List(ctx context.Context) ([]Sandbox, error) {
	out, err := c.outputCmd(ctx, "ls", "--json")
	if err != nil {
		return nil, eris.Wrap(err, "sbx ls")
	}

	return parseList(out)
}

// Exists returns true if a sandbox with the given name is in the list.
func (c *Client) Exists(ctx context.Context, name string) (bool, error) {
	sandboxes, err := c.List(ctx)
	if err != nil {
		return false, err
	}

	for _, s := range sandboxes {
		if s.Name == name {
			return true, nil
		}
	}

	return false, nil
}

// Run resumes an existing sandbox by name.
func (c *Client) Run(ctx context.Context, name string) error {
	if err := c.runCmd(ctx, "run", name); err != nil {
		return eris.Wrapf(err, "sbx run %q", name)
	}

	return nil
}

// Create creates a new sandbox with the provided arguments via `sbx create`.
// Unlike `sbx run`, this does not attach to the agent; use Run afterwards to attach.
func (c *Client) Create(ctx context.Context, args []string) error {
	cmdArgs := append([]string{"create"}, args...)
	if err := c.runCmd(ctx, cmdArgs...); err != nil {
		return eris.Wrapf(err, "sbx create %s", strings.Join(args, " "))
	}

	return nil
}

// Remove deletes a sandbox by name (uses --force).
func (c *Client) Remove(ctx context.Context, name string) error {
	if err := c.runCmd(ctx, "rm", "--force", name); err != nil {
		return eris.Wrapf(err, "sbx rm %q", name)
	}

	return nil
}

// AddKit applies a kit to an existing sandbox via `sbx kit add`.
// This is idempotent for mixin kits — re-adding the same kit just re-records metadata.
func (c *Client) AddKit(ctx context.Context, sandboxName, kitPath string) error {
	if err := c.runCmd(ctx, "kit", "add", sandboxName, kitPath); err != nil {
		return eris.Wrapf(err, "sbx kit add %q %q", sandboxName, kitPath)
	}

	return nil
}

// LoadTemplate loads a tar file into the sbx template store.
func (c *Client) LoadTemplate(ctx context.Context, tarPath string) error {
	if err := c.runCmd(ctx, "template", "load", tarPath); err != nil {
		return eris.Wrapf(err, "sbx template load %q", tarPath)
	}

	return nil
}

// CurrentPolicy returns the active default network policy by parsing
// `sbx policy ls --type network`, or "" if it cannot be determined.
// "" can occur even when a default is set; see
// https://github.com/docker/sbx-releases/issues/126.
func (c *Client) CurrentPolicy(ctx context.Context) (string, error) {
	out, err := c.outputCmd(ctx, "policy", "ls", "--type", "network")
	if err != nil {
		return "", eris.Wrap(err, "sbx policy ls")
	}

	return parsePolicy(string(out)), nil
}

// SetDefaultPolicy sets the default network policy.
func (c *Client) SetDefaultPolicy(ctx context.Context, policy string) error {
	if err := c.runCmd(ctx, "policy", "set-default", policy); err != nil {
		return eris.Wrapf(err, "sbx policy set-default %q", policy)
	}

	return nil
}

// AllowNetwork adds an allow rule for one or more domains in a single call.
// `sbx policy allow network` accepts a comma-separated RESOURCES list.
// Empty lists are a no-op.
func (c *Client) AllowNetwork(ctx context.Context, domains ...string) error {
	resources := strings.Join(domains, ",")
	if resources == "" {
		return nil
	}

	if err := c.runCmd(ctx, "policy", "allow", "network", resources); err != nil {
		return eris.Wrapf(err, "sbx policy allow network %q", resources)
	}

	return nil
}

// DenyNetwork adds a deny rule for one or more domains in a single call.
// Empty lists are a no-op.
func (c *Client) DenyNetwork(ctx context.Context, domains ...string) error {
	resources := strings.Join(domains, ",")
	if resources == "" {
		return nil
	}

	if err := c.runCmd(ctx, "policy", "deny", "network", resources); err != nil {
		return eris.Wrapf(err, "sbx policy deny network %q", resources)
	}

	return nil
}

// SecretEntry is one row of `sbx secret ls`.
type SecretEntry struct {
	// Scope is "(global)" for global secrets or the sandbox name for per-sandbox secrets.
	Scope string
	// Service is the sbx secret service name (see `sbx secret set --help`).
	Service string
}

// GlobalScope is the literal scope string sbx prints for global secrets.
const GlobalScope = "(global)"

// ListSecrets returns the rows of `sbx secret ls`, including scope so callers
// can distinguish global secrets from per-sandbox ones.
func (c *Client) ListSecrets(ctx context.Context) ([]SecretEntry, error) {
	out, err := c.outputCmd(ctx, "secret", "ls")
	if err != nil {
		return nil, eris.Wrap(err, "sbx secret ls")
	}

	return parseSecretList(string(out)), nil
}

// SetSecret stores a per-sandbox secret value via `sbx secret set`. The value
// is piped via stdin so it never appears in argv.
func (c *Client) SetSecret(ctx context.Context, sandboxName, service, value string) error {
	args := c.args("secret", "set", sandboxName, service)
	c.logCmd(args)

	if err := c.runner.RunWithStdin(ctx, "sbx", value, args...); err != nil {
		return eris.Wrapf(err, "sbx secret set %q", service)
	}

	return nil
}

// args prepends the global --debug flag (when enabled) to the provided sbx args.
func (c *Client) args(rest ...string) []string {
	if !c.debug {
		return rest
	}

	out := make([]string, 0, len(rest)+1)
	out = append(out, "--debug")
	out = append(out, rest...)

	return out
}

// logCmd writes the command line to the verbose log when verbose is enabled.
// A failed write to the log sink is intentionally ignored — best-effort logging
// must never break the underlying command.
func (c *Client) logCmd(args []string) {
	if !c.verbose || c.logOut == nil {
		return
	}

	if _, err := fmt.Fprintf(c.logOut, "+ sbx %s\n", strings.Join(args, " ")); err != nil {
		return
	}
}

// runCmd logs (when verbose) and runs an sbx command. The runner error is
// returned as-is; callers add their own context with eris.Wrap.
func (c *Client) runCmd(ctx context.Context, sbxArgs ...string) error {
	args := c.args(sbxArgs...)
	c.logCmd(args)

	return c.runner.Run(ctx, "sbx", args...) //nolint:wrapcheck // wrapped by callers with command-specific context
}

// outputCmd logs (when verbose) and runs an sbx command, returning its stdout.
// The runner error is returned as-is; callers add their own context with eris.Wrap.
func (c *Client) outputCmd(ctx context.Context, sbxArgs ...string) ([]byte, error) {
	args := c.args(sbxArgs...)
	c.logCmd(args)

	return c.runner.Output(ctx, "sbx", args...) //nolint:wrapcheck // wrapped by callers with command-specific context
}

// parseList parses the JSON output of `sbx ls --json`.
func parseList(data []byte) ([]Sandbox, error) {
	var resp listResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, eris.Wrap(err, "parsing sbx ls output")
	}

	return resp.Sandboxes, nil
}

// parsePolicy returns the first whitespace-separated token that exactly
// matches a known policy name, or "" if none is found. Tokenizing avoids
// false matches against user rule names that happen to contain a policy
// keyword as a substring (e.g. "default-balanced-corp").
func parsePolicy(output string) string {
	for token := range strings.FieldsSeq(output) {
		switch token {
		case "allow-all", "balanced", "deny-all":
			return token
		}
	}

	return ""
}

// parseSecretList parses the SCOPE and SERVICE columns from `sbx secret ls`.
// The CLI prints a tabular listing with columns SCOPE / SERVICE / SECRET.
// When no secrets exist, sbx prints a sentence with no header — that yields nil.
func parseSecretList(output string) []SecretEntry {
	var (
		entries    []SecretEntry
		scopeIdx   = -1
		serviceIdx = -1
	)

	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)

		if scopeIdx < 0 || serviceIdx < 0 {
			for i, f := range fields {
				switch strings.ToUpper(f) {
				case "SCOPE":
					scopeIdx = i
				case "SERVICE":
					serviceIdx = i
				}
			}

			// Header not found on the first non-empty line — likely the
			// "No secrets found." sentence. Bail out quietly.
			if scopeIdx < 0 || serviceIdx < 0 {
				return nil
			}

			continue
		}

		if scopeIdx < len(fields) && serviceIdx < len(fields) {
			entries = append(entries, SecretEntry{
				Scope:   fields[scopeIdx],
				Service: fields[serviceIdx],
			})
		}
	}

	return entries
}
