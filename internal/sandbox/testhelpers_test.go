package sandbox_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sandbox"
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
)

// minimalConfig is the simplest valid .sbxgo/config.toml content.
const minimalConfig = "[sandbox]\nagent = \"claude\"\n"

// emptyListJSON is the response for `sbx ls --json` when no sandboxes exist.
const emptyListJSON = `{"sandboxes":[]}`

// versionOK is the `sbx version` response that satisfies CheckMinVersion.
// `var` rather than `const` because sbx.MinVersion is embedded at build time.
var versionOK = "Client Version:  v" + sbx.MinVersion + " testsha\nServer Version: Unavailable\n"

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
// that Setup and Start call for a minimal config with no existing sandbox.
func newHappyRunner() *runner.FakeRunner {
	r := runner.NewFakeRunner()
	r.SetOutputResponse("sbx", []string{"version"}, []byte(versionOK))
	r.SetOutputResponse("sbx", []string{"ls", "--json"}, []byte(emptyListJSON))
	r.SetOutputResponse("sbx", []string{"policy", "ls", "--type", "network"}, []byte("balanced"))

	return r
}

// newRunnerWithExistingSandbox creates a FakeRunner configured with the current sandbox listed.
func newRunnerWithExistingSandbox() *runner.FakeRunner {
	r := runner.NewFakeRunner()
	r.SetOutputResponse("sbx", []string{"version"}, []byte(versionOK))
	r.SetOutputResponse("sbx", []string{"ls", "--json"}, []byte(currentSandboxListJSON()))
	r.SetOutputResponse("sbx", []string{"policy", "ls", "--type", "network"}, []byte("balanced"))

	return r
}

// hasSbxCall returns true if any recorded Run call to "sbx" contains all the given args.
func hasSbxCall(calls []runner.Call, argsSubset ...string) bool {
	for _, call := range calls {
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
			return true
		}
	}

	return false
}
