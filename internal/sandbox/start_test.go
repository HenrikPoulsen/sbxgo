package sandbox_test

import (
	"context"
	"testing"

	"github.com/HenrikPoulsen/sbxgo/internal/fsutil"
	"github.com/HenrikPoulsen/sbxgo/internal/prompt"
	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStart_ErrorWhenNoConfig verifies that Start returns an error when no config exists.
func TestStart_ErrorWhenNoConfig(t *testing.T) {
	t.Parallel()

	fs := fsutil.NewFakeFileSystem()
	r := runner.NewFakeRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sbxgo setup")
}

// TestStart_ResumesSandboxWhenExists verifies that an existing sandbox is resumed via sbx run.
func TestStart_ResumesSandboxWhenExists(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName),
		"expected sbx run <sandbox-name> to resume existing sandbox")
	assert.False(t, hasSbxCall(r.RunCalls, "create"),
		"expected no new sandbox creation when resuming")
}

// TestStart_CreatesSandboxWhenNotExists verifies that a new sandbox is created (sbx create)
// and then attached (sbx run) when no sandbox exists yet.
func TestStart_CreatesSandboxWhenNotExists(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasSbxCall(r.RunCalls, "create", "claude", "."),
		"expected sbx create with agent and workspace for new sandbox")
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName),
		"expected sbx run <sandbox-name> to attach after creation")

	_, ok := fs.Files[sandbox.CreateStateFile]
	assert.True(t, ok, "expected create-state file to be written after creating sandbox")
}

// TestStart_DryRun_ExistingSandbox verifies that dry-run prints the resume command without executing.
func TestStart_DryRun_ExistingSandbox(t *testing.T) {
	t.Parallel()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{DryRun: true}, r, fs, p)

	require.NoError(t, err)
	assert.False(t, hasSbxCall(r.RunCalls, "run"),
		"expected no actual sbx run in dry-run mode")
}

// TestStart_DryRun_NewSandbox verifies that dry-run prints the create command without executing.
func TestStart_DryRun_NewSandbox(t *testing.T) {
	t.Parallel()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{DryRun: true}, r, fs, p)

	require.NoError(t, err)
	assert.False(t, hasSbxCall(r.RunCalls, "run"),
		"expected no actual sbx run in dry-run mode")
}

// TestStart_WithKits verifies that kits are passed as --kit flags when creating a sandbox.
func TestStart_WithKits(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nkits = [\"https://github.com/docker/sbx-kits-contrib/go\"]\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasSbxCall(r.RunCalls, "--kit", "https://github.com/docker/sbx-kits-contrib/go"),
		"expected kit URL passed to sbx create")
}

// TestStart_ResumeAppliesKits verifies that kits configured in config.toml are re-applied
// via `sbx kit add` when resuming an existing sandbox.
func TestStart_ResumeAppliesKits(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nkits = [\".sbxgo/kits/go-tools\"]\n"
	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasSbxCall(r.RunCalls, "kit", "add", sandboxName, ".sbxgo/kits/go-tools"),
		"expected sbx kit add to apply configured kit on resume")
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName),
		"expected sbx run to resume existing sandbox after applying kits")
}

// TestStart_DryRun_ResumeSkipsKitAdd verifies that dry-run does not actually call sbx kit add.
func TestStart_DryRun_ResumeSkipsKitAdd(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nkits = [\".sbxgo/kits/go-tools\"]\n"
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{DryRun: true}, r, fs, p)

	require.NoError(t, err)
	assert.False(t, hasSbxCall(r.RunCalls, "kit", "add"),
		"expected no actual sbx kit add in dry-run mode")
}

// TestStart_WarnsMissingRequiredSecret verifies that a warning is printed but the command proceeds
// when a required secret is not set.
func TestStart_WarnsMissingRequiredSecret(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nrequired_secrets = [\"ANTHROPIC_API_KEY\"]\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	// Configure empty secrets list (ANTHROPIC_API_KEY is missing)
	r.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(""))

	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	// Should succeed despite missing secret — it only warns
	require.NoError(t, err)
	assert.True(t, hasSbxCall(r.RunCalls, "run"), "expected sandbox creation despite missing secret")
}

// TestStart_StatelessSandbox_DeclinePromptResumes verifies that an existing
// sandbox without a create-state file triggers a recreate prompt; declining
// it records state for next time and resumes normally.
func TestStart_StatelessSandbox_DeclinePromptResumes(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false) // user declines

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	require.Len(t, p.Calls, 1, "expected prompt about stateless sandbox")
	assert.False(t, p.Defaults[0], "prompt should default to no")

	_, hasState := fs.Files[sandbox.CreateStateFile]
	assert.True(t, hasState, "expected create-state file to be written so next run is silent")
	assert.False(t, hasSbxCall(r.RunCalls, "rm", "--force", sandboxName),
		"expected no removal when user declines")
	assert.False(t, hasSbxCall(r.RunCalls, "create"),
		"expected no creation when user declines")
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName),
		"expected resume to proceed after decline")
}

// TestStart_StatelessSandbox_ConfirmRecreates verifies that confirming the
// stateless-sandbox prompt removes the existing sandbox and creates a fresh
// one from the config.
func TestStart_StatelessSandbox_ConfirmRecreates(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(true) // user confirms

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	require.Len(t, p.Calls, 1)
	assert.True(t, hasSbxCall(r.RunCalls, "rm", "--force", sandboxName),
		"expected sandbox to be removed before recreate")
	assert.True(t, hasSbxCall(r.RunCalls, "create", "claude", "."),
		"expected fresh sandbox to be created from config")
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName),
		"expected agent to be started in the new sandbox")
}

// TestStart_DriftPromptsAndRecreatesOnConfirm verifies that a drifted config prompts the user
// and recreates the sandbox when confirmed.
func TestStart_DriftPromptsAndRecreatesOnConfirm(t *testing.T) {
	t.Parallel()

	cfgWithBranch := "[sandbox]\nagent = \"claude\"\nbranch = \"feature-x\"\n"
	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfgWithBranch)
	// Stale state hash that will not match the current config.
	fs.Files[sandbox.CreateStateFile] = []byte("stale-hash-from-previous-config\n")
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(true) // user confirms recreate

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	require.NotEmpty(t, p.Calls, "expected drift prompt")
	assert.Contains(t, p.Calls[0], "Recreate")
	assert.True(t, hasSbxCall(r.RunCalls, "rm", "--force", sandboxName),
		"expected sandbox to be removed before recreate")
	assert.True(t, hasSbxCall(r.RunCalls, "--branch", "feature-x"),
		"expected new sandbox to be created with the updated branch")
}

// TestStart_DriftDeclinedKeepsResuming verifies that declining the drift prompt resumes
// the existing sandbox without recreating it.
func TestStart_DriftDeclinedKeepsResuming(t *testing.T) {
	t.Parallel()

	cfgWithBranch := "[sandbox]\nagent = \"claude\"\nbranch = \"feature-x\"\n"
	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfgWithBranch)
	fs.Files[sandbox.CreateStateFile] = []byte("stale-hash\n")
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false) // user declines recreate

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	require.NotEmpty(t, p.Calls, "expected drift prompt")
	assert.False(t, hasSbxCall(r.RunCalls, "rm", "--force", sandboxName),
		"expected no sandbox removal when user declines recreate")
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName),
		"expected sandbox to be resumed after declining recreate")
}

// TestStart_DriftDryRunDoesNotPrompt verifies that drift in dry-run mode does not prompt
// or modify state.
func TestStart_DriftDryRunDoesNotPrompt(t *testing.T) {
	t.Parallel()

	cfgWithBranch := "[sandbox]\nagent = \"claude\"\nbranch = \"feature-x\"\n"
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfgWithBranch)
	staleState := []byte("stale-hash\n")
	fs.Files[sandbox.CreateStateFile] = staleState
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(true)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{DryRun: true}, r, fs, p)

	require.NoError(t, err)
	assert.Empty(t, p.Calls, "expected no prompt in dry-run mode")
	assert.Equal(t, staleState, fs.Files[sandbox.CreateStateFile],
		"expected create-state file to be left untouched in dry-run")
}

// TestStart_NoDriftWhenConfigUnchanged verifies that once state is recorded,
// subsequent runs with the same config don't prompt.
func TestStart_NoDriftWhenConfigUnchanged(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)

	// First call: existing sandbox, no state → prompts. Declining records state.
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)
	require.NoError(t, sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p))
	require.Len(t, p.Calls, 1, "first call should prompt about stateless sandbox")

	// Second call with the same config: state now matches, no prompt.
	r2 := newRunnerWithExistingSandbox()
	p2 := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r2, fs, p2)
	require.NoError(t, err)
	assert.Empty(t, p2.Calls, "expected no prompt when config matches recorded state")
	assert.True(t, hasSbxCall(r2.RunCalls, "run", sandboxName),
		"expected sandbox to be resumed normally")
}
