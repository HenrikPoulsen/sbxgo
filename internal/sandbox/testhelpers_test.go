package sandbox_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sandbox"
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
)

// minimalConfig is the simplest valid .sbxgo/config.toml content.
const minimalConfig = "[sandbox]\nagent = \"claude\"\n"

// emptyListJSON is the response for `sbx ls --json` when no sandboxes exist.
const emptyListJSON = `{"sandboxes":[]}`

// currentSandboxName returns the sandbox name for the claude agent in the current working directory.
func currentSandboxName() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("could not get working directory: %v", err))
	}

	name, err := sandbox.Name("claude", wd)
	if err != nil {
		panic(fmt.Sprintf("could not derive sandbox name: %v", err))
	}

	return name
}

// currentSandboxListJSON returns a `sbx ls --json` response listing the current sandbox.
func currentSandboxListJSON() string {
	name := currentSandboxName()

	return fmt.Sprintf(`{"sandboxes":[{"name":%q,"agent":"claude","status":"stopped","workspaces":[%q]}]}`,
		name, filepath.Dir(name))
}

// newHappyRunner creates a FakeRunner pre-configured to respond to the sbx commands
// that Setup and Start call for a minimal config with no existing sandbox. The
// default `sbx policy ls <sandbox>` response is empty, so every configured
// allow/deny domain looks "missing" and applyPolicy will emit it. Tests that
// want to exercise the "already in place" path override with
// configureExistingRules.
func newHappyRunner() *runner.FakeRunner {
	r := runner.NewFakeRunner()
	r.SetOutputResponse("sbx", []string{"ls", "--json"}, []byte(emptyListJSON))
	configureExistingRules(r, currentSandboxName(), nil)

	return r
}

// newRunnerWithExistingSandbox creates a FakeRunner configured with the current sandbox listed.
func newRunnerWithExistingSandbox() *runner.FakeRunner {
	r := runner.NewFakeRunner()
	r.SetOutputResponse("sbx", []string{"ls", "--json"}, []byte(currentSandboxListJSON()))
	configureExistingRules(r, currentSandboxName(), nil)

	return r
}

// configureExistingRules sets up a canned `sbx policy ls <sandboxName>`
// response containing the given rules (rendered in the same tabular shape
// the real sbx CLI uses). Pass nil to indicate "no rules apply".
func configureExistingRules(r *runner.FakeRunner, sandboxName string, rules []sbx.PolicyRule) {
	var b strings.Builder

	b.WriteString("PROVENANCE   APPLIES_TO   POLICY/RULE                            TYPE      " +
		"DECISION   RESOURCES\n")

	for i, rule := range rules {
		fmt.Fprintf(&b, "local        sandbox:%s   %08d-fake-4a73-4e05-bc9d-f2f9a4b50d67   network   %-9s  %s\n",
			sandboxName, i, rule.Decision, rule.Resource)
	}

	r.SetOutputResponse("sbx", []string{"policy", "ls", sandboxName}, []byte(b.String()))
}

// hasSbxCall returns true if any recorded Run call to "sbx" contains all the given args.
func hasSbxCall(calls []runner.Call, argsSubset ...string) bool {
	return indexOfSbxCall(calls, argsSubset...) >= 0
}

// indexOfSbxCall returns the index of the first sbx call whose args contain
// every entry in argsSubset, or -1 if none match. Used by ordering assertions.
func indexOfSbxCall(calls []runner.Call, argsSubset ...string) int {
	for i, call := range calls {
		if call.Name != "sbx" {
			continue
		}

		matched := true

		for _, want := range argsSubset {
			if !slices.Contains(call.Args, want) {
				matched = false
				break
			}
		}

		if matched {
			return i
		}
	}

	return -1
}
