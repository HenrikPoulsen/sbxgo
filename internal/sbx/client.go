// Package sbx provides wrappers around the sbx CLI tool.
package sbx

import (
	"bytes"
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

// Run attaches to sandbox via `sbx run --name <name>`.
func (c *Client) Run(ctx context.Context, name string) error {
	if err := c.runCmd(ctx, "run", "--name", name); err != nil {
		return eris.Wrapf(err, "sbx run --name %q", name)
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

// LoadTemplate loads a tar file into the sbx template store.
func (c *Client) LoadTemplate(ctx context.Context, tarPath string) error {
	if err := c.runCmd(ctx, "template", "load", tarPath); err != nil {
		return eris.Wrapf(err, "sbx template load %q", tarPath)
	}

	return nil
}

// PolicyRule is a single allow/deny rule that applies to a sandbox. Returned
// by ListSandboxRules, used by callers to diff against configured rules.
type PolicyRule struct {
	Decision string // "allow" or "deny"
	Resource string
}

// ListSandboxRules returns every network policy rule that applies to the
// given sandbox: global (`origin: local`) rules and sandbox-scoped
// (`origin: sandbox:<name>`) rules alike. Callers use this to diff against
// configured allow/deny lists and avoid re-emitting rules that are already
// in place.
func (c *Client) ListSandboxRules(ctx context.Context, sandboxName string) ([]PolicyRule, error) {
	out, err := c.outputCmd(ctx, "policy", "ls", sandboxName)
	if err != nil {
		return nil, eris.Wrapf(err, "sbx policy ls %q", sandboxName)
	}

	return parseSandboxRules(string(out)), nil
}

// AllowNetwork adds allow rules scoped to a sandbox via --sandbox. Empty domains is no-op.
func (c *Client) AllowNetwork(ctx context.Context, sandboxName string, domains ...string) error {
	resources := strings.Join(domains, ",")
	if resources == "" {
		return nil
	}

	if err := c.runCmd(ctx, "policy", "allow", "network", "--sandbox", sandboxName, resources); err != nil {
		return eris.Wrapf(err, "sbx policy allow network --sandbox %q %q", sandboxName, resources)
	}

	return nil
}

// DenyNetwork adds deny rules scoped to a sandbox. Empty domains is no-op. See AllowNetwork.
func (c *Client) DenyNetwork(ctx context.Context, sandboxName string, domains ...string) error {
	resources := strings.Join(domains, ",")
	if resources == "" {
		return nil
	}

	if err := c.runCmd(ctx, "policy", "deny", "network", "--sandbox", sandboxName, resources); err != nil {
		return eris.Wrapf(err, "sbx policy deny network --sandbox %q %q", sandboxName, resources)
	}

	return nil
}

// ListSecrets returns a list of secret names from `sbx secret ls`.
func (c *Client) ListSecrets(ctx context.Context) ([]string, error) {
	out, err := c.outputCmd(ctx, "secret", "ls")
	if err != nil {
		return nil, eris.Wrap(err, "sbx secret ls")
	}

	return parseSecretList(string(out)), nil
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
// A failed write to the log sink is intentionally ignored: best-effort logging
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
	if err := json.Unmarshal(extractJSONObject(data), &resp); err != nil {
		return nil, eris.Wrapf(err, "parsing sbx ls output (raw stdout: %q)", truncateForError(data))
	}

	return resp.Sandboxes, nil
}

// extractJSONObject returns the slice of data starting at the first '{'. On the
// first invocation after boot, sbx auto-starts its daemon and prepends startup
// lines ("Starting sandboxd daemon...", etc.) to stdout before the JSON object.
// Trimming everything before the opening brace makes parsing tolerant of that
// preamble. If no '{' is present the data is returned unchanged so the JSON
// decoder produces the original error.
func extractJSONObject(data []byte) []byte {
	if i := bytes.IndexByte(data, '{'); i >= 0 {
		return data[i:]
	}

	return data
}

// truncateForError caps data at 500 chars for use in error messages.
func truncateForError(data []byte) string {
	const maxLen = 500

	s := strings.TrimSpace(string(data))
	if len(s) > maxLen {
		return s[:maxLen] + "…(truncated)"
	}

	return s
}

// ruleRowColumns: `sbx policy ls` column count.
// Layout: PROVENANCE APPLIES_TO POLICY/RULE TYPE DECISION RESOURCES.
const ruleRowColumns = 6

const (
	ruleTypeIdx     = 3 // TYPE column ("network", "filesystem:read", ...)
	ruleDecisionIdx = 4 // DECISION column ("allow"/"deny")
	ruleResourceIdx = 5 // first RESOURCE column; continuation resources follow
)

// parseSandboxRules parses `sbx policy ls <sandbox>` output.
// Full rows (≥6 fields): new rule. Continuation (1 field): extra resource.
// Skips non-network (filesystem:*) rows. One PolicyRule per resource.
func parseSandboxRules(output string) []PolicyRule {
	var (
		rules        []PolicyRule
		lastDecision string // decision of the current network rule, "" when the current rule is non-network
		seenHeader   bool
	)

	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		if !seenHeader && fields[0] == "PROVENANCE" {
			seenHeader = true
			continue
		}

		// Full row: column count used; early columns can contain colons.
		if len(fields) >= ruleRowColumns {
			if fields[ruleTypeIdx] != "network" {
				// Non-network rule (e.g. filesystem:read/write): ignore it and
				// any continuation lines it may have.
				lastDecision = ""

				continue
			}

			lastDecision = fields[ruleDecisionIdx]
			for _, res := range fields[ruleResourceIdx:] {
				rules = append(rules, PolicyRule{Decision: lastDecision, Resource: res})
			}

			continue
		}

		// Continuation line: a single additional resource for the current rule.
		if len(fields) == 1 && lastDecision != "" {
			rules = append(rules, PolicyRule{Decision: lastDecision, Resource: fields[0]})
		}
	}

	return rules
}

// parseSecretList extracts the SERVICE column from `sbx secret ls` output.
// The CLI prints a tabular listing with columns SCOPE / SERVICE / SECRET.
// When no secrets exist, sbx prints a sentence with no header, which yields nil.
func parseSecretList(output string) []string {
	var (
		secrets    []string
		serviceIdx = -1
	)

	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)

		if serviceIdx < 0 {
			for i, f := range fields {
				if strings.EqualFold(f, "SERVICE") {
					serviceIdx = i
					break
				}
			}

			// Header not found on the first non-empty line, likely the
			// "No secrets found." sentence. Bail out quietly.
			if serviceIdx < 0 {
				return nil
			}

			continue
		}

		if serviceIdx < len(fields) {
			secrets = append(secrets, fields[serviceIdx])
		}
	}

	return secrets
}
