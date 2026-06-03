package sandbox_test

import (
	"context"
	"testing"

	"github.com/HenrikPoulsen/sbxgo/internal/fsutil"
	"github.com/HenrikPoulsen/sbxgo/internal/prompt"
	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sandbox"
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
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

// TestStart_ResumeDoesNotReApplyKits is a regression guard: resume must
// never call `sbx kit add`. Kits are applied at sandbox creation only;
// content changes flow through the drift recreate prompt instead.
func TestStart_ResumeDoesNotReApplyKits(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nkits = [\".sbxgo/kits/go-tools\"]\n"
	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	// Seed kit content so the drift hash matches once written.
	fs.Files[".sbxgo/kits/go-tools/spec.yaml"] = []byte("schemaVersion: \"1\"\n")
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	require.NoError(t, sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p))

	assert.False(t, hasSbxCall(r.RunCalls, "kit", "add"),
		"resume must never re-apply kits; that runs apt installs against a live sandbox")
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName),
		"expected sbx run to resume the existing sandbox")
}

// TestStart_KitContentChangeTriggersDriftPrompt verifies the central behaviour
// of this design: when the contents of a configured kit change between runs,
// the drift detector treats it the same as a docker source or branch change
// and prompts the user to recreate.
func TestStart_KitContentChangeTriggersDriftPrompt(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nkits = [\".sbxgo/kits/tools\"]\n"
	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	fs.Files[".sbxgo/kits/tools/spec.yaml"] = []byte("schemaVersion: \"1\"\nname: tools\n")
	// Stale create-state hash that won't match the current kit content.
	fs.Files[sandbox.CreateStateFile] = []byte("stale-hash\n")

	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(true) // user confirms recreate

	require.NoError(t, sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p))

	require.NotEmpty(t, p.Calls, "expected drift recreate prompt")
	assert.Contains(t, p.Calls[0], "Recreate")
	assert.True(t, hasSbxCall(r.RunCalls, "rm", "--force", sandboxName),
		"expected sandbox to be removed before recreate")
	assert.True(t, hasSbxCall(r.RunCalls, "--kit", ".sbxgo/kits/tools"),
		"expected new sandbox to be created with the kit, applying its current contents")
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

	// Should succeed despite missing secret; it only warns
	require.NoError(t, err)
	assert.True(t, hasSbxCall(r.RunCalls, "run"), "expected sandbox creation despite missing secret")
}

// TestStart_DriftRecordsStateForLegacySandbox verifies that resuming a sandbox without a
// create-state file silently records the current state and proceeds (graceful upgrade path).
func TestStart_DriftRecordsStateForLegacySandbox(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.Empty(t, p.Calls, "expected no prompt for sandbox without prior state")

	_, hasState := fs.Files[sandbox.CreateStateFile]
	assert.True(t, hasState, "expected create-state file to be written for legacy sandbox")
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName), "expected resume to proceed")
}

// TestStart_DriftPromptsAndRecreatesOnConfirm verifies that a drifted config prompts the user
// and recreates the sandbox when confirmed.
func TestStart_DriftPromptsAndRecreatesOnConfirm(t *testing.T) {
	t.Parallel()

	cfgWithClone := "[sandbox]\nagent = \"claude\"\nclone = true\n"
	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfgWithClone)
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
	assert.True(t, hasSbxCall(r.RunCalls, "--clone"),
		"expected new sandbox to be created with --clone enabled")
}

// TestStart_DriftDeclinedKeepsResuming verifies that declining the drift prompt resumes
// the existing sandbox without recreating it.
func TestStart_DriftDeclinedKeepsResuming(t *testing.T) {
	t.Parallel()

	cfgWithClone := "[sandbox]\nagent = \"claude\"\nclone = true\n"
	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfgWithClone)
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

	cfgWithClone := "[sandbox]\nagent = \"claude\"\nclone = true\n"
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfgWithClone)
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

// TestStart_ResumeAppliesPolicy verifies that allowed_domains in config triggers
// a sandbox-scoped policy allow call on the resume path (not only on first create).
func TestStart_ResumeAppliesPolicy(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nallowed_domains = [\"github.com\"]\n"
	sandboxName := currentSandboxName()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	require.NoError(t, sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p))

	assert.True(t,
		hasSbxCall(r.RunCalls, "policy", "allow", "network", sandboxName, "github.com"),
		"expected sandbox-scoped policy allow on resume, not only on create")
}

// TestStart_ResumeSkipsAllowWhenAlreadyInPlace verifies that on resume, if the
// configured allowed_domain is already an active rule for this sandbox, we
// skip the `policy allow network` call (quiet no-op).
func TestStart_ResumeSkipsAllowWhenAlreadyInPlace(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nallowed_domains = [\"github.com\"]\n"
	sandboxName := currentSandboxName()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newRunnerWithExistingSandbox()
	configureExistingRules(r, sandboxName, []sbx.PolicyRule{
		{Decision: "allow", Resource: "github.com"},
	})

	p := prompt.NewFakePrompter(false)

	require.NoError(t, sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p))

	assert.False(t, hasSbxCall(r.RunCalls, "policy", "allow", "network"),
		"resume must skip policy allow when the rule is already in place")
}

// TestStart_DryRunSkipsPolicyCalls confirms dry-run on the resume path does
// not actually call sbx policy allow/deny.
func TestStart_DryRunSkipsPolicyCalls(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nallowed_domains = [\"github.com\"]\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	require.NoError(t, sandbox.Start(context.Background(), sandbox.StartOptions{DryRun: true}, r, fs, p))

	assert.False(t, hasSbxCall(r.RunCalls, "policy", "allow"),
		"dry-run must not call sbx policy allow on resume")
}

// TestStart_NoDriftWhenConfigUnchanged verifies that a matching state hash skips the prompt.
func TestStart_NoDriftWhenConfigUnchanged(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(true)

	// First call writes the state for this config.
	require.NoError(t, sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p))
	require.Empty(t, p.Calls, "first call should record state without prompting")

	// Second call with the same config should not prompt.
	r2 := newRunnerWithExistingSandbox()
	p2 := prompt.NewFakePrompter(true)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r2, fs, p2)
	require.NoError(t, err)
	assert.Empty(t, p2.Calls, "expected no drift prompt when config unchanged")
	assert.True(t, hasSbxCall(r2.RunCalls, "run", sandboxName),
		"expected sandbox to be resumed normally")
}
