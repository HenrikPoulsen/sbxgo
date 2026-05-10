package sandbox_test

import (
	"context"
	"testing"

	"github.com/HenrikPoulsen/sbxgo/internal/fsutil"
	"github.com/HenrikPoulsen/sbxgo/internal/prompt"
	"github.com/HenrikPoulsen/sbxgo/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// emptySecretLs is the sbx output when no secrets are stored yet.
	emptySecretLs = "No secrets found.\n"
	// configWithFooSecret declares "foo" as a required secret and maps it to
	// $FOO_API_KEY for env-var auto-sync.
	configWithFooSecret = "[sandbox]\nagent = \"claude\"\n" +
		"[sandbox.secrets]\nfoo = \"FOO_API_KEY\"\n"
	// configWithFooSecretNoEnv declares the requirement but with an empty
	// env-var name, so sync is a no-op even if $FOO_API_KEY is populated.
	// (Used to exercise the warn-only branch.)
	configWithFooSecretNoEnv = "[sandbox]\nagent = \"claude\"\n" +
		"[sandbox.secrets]\nfoo = \"\"\n"
)

// TestSetup_SyncsSecretFromEnvPerSandbox verifies that an env var matching a
// required service is piped into `sbx secret set <sandbox> <service>` after
// the sandbox has been created.
func TestSetup_SyncsSecretFromEnvPerSandbox(t *testing.T) {
	t.Setenv("FOO_API_KEY", "sk-test-from-env")

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(configWithFooSecret)
	r := newHappyRunner()
	r.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(emptySecretLs))

	p := prompt.NewFakePrompter(true)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	require.Len(t, r.RunStdinCalls, 1, "expected exactly one stdin-piped call (the secret set)")
	assert.Equal(t, "sbx", r.RunStdinCalls[0].Name)
	assert.Equal(t, []string{"secret", "set", sandboxName, "foo"}, r.RunStdinCalls[0].Args,
		"secret should be set per-sandbox, not globally")
	assert.Equal(t, "sk-test-from-env", r.RunStdinCalls[0].Stdin,
		"value must be piped via stdin, not present in argv")
}

// TestSetup_DoesNotSyncWhenAlreadyGlobal verifies that we don't overwrite a
// secret that's already available globally (sbx merges global into every
// sandbox automatically).
func TestSetup_DoesNotSyncWhenAlreadyGlobal(t *testing.T) {
	t.Setenv("FOO_API_KEY", "sk-from-env")

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(configWithFooSecret)
	r := newHappyRunner()
	r.SetOutputResponse("sbx", []string{"secret", "ls"},
		[]byte("SCOPE      SERVICE   SECRET\n(global)   foo       sk-12****\n"))

	p := prompt.NewFakePrompter(true)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.Empty(t, r.RunStdinCalls,
		"global secret already covers this sandbox; no sbx secret set should fire")
}

// TestSetup_DoesNotSyncWhenAlreadyPerSandbox verifies that a per-sandbox
// secret already bound to *this* sandbox is left alone.
func TestSetup_DoesNotSyncWhenAlreadyPerSandbox(t *testing.T) {
	t.Setenv("FOO_API_KEY", "sk-from-env")

	sandboxName := currentSandboxName()
	tabular := "SCOPE       SERVICE   SECRET\n" +
		sandboxName + "   foo       sk-12****\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(configWithFooSecret)
	r := newHappyRunner()
	r.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(tabular))

	p := prompt.NewFakePrompter(true)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.Empty(t, r.RunStdinCalls)
}

// TestSetup_SyncsWhenSecretBoundToOtherSandbox verifies that a per-sandbox
// secret bound to a *different* sandbox does NOT count as already-set, so
// sync still fires for our sandbox.
func TestSetup_SyncsWhenSecretBoundToOtherSandbox(t *testing.T) {
	t.Setenv("FOO_API_KEY", "sk-from-env")

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(configWithFooSecret)
	r := newHappyRunner()
	r.SetOutputResponse("sbx", []string{"secret", "ls"},
		[]byte("SCOPE             SERVICE   SECRET\nsome-other-sbx    foo       sk-12****\n"))

	p := prompt.NewFakePrompter(true)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	require.Len(t, r.RunStdinCalls, 1)
	assert.Equal(t, []string{"secret", "set", sandboxName, "foo"}, r.RunStdinCalls[0].Args)
}

// TestSetup_NoSyncWithoutEnvVarMapping verifies that even with the env var
// populated, sync is skipped when the [sandbox.secrets] entry has an empty
// env-var value. The early checkSecrets prints a warning instead.
func TestSetup_NoSyncWithoutEnvVarMapping(t *testing.T) {
	t.Setenv("FOO_API_KEY", "sk-from-env")

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(configWithFooSecretNoEnv)
	r := newHappyRunner()
	r.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(emptySecretLs))

	p := prompt.NewFakePrompter(true)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.Empty(t, r.RunStdinCalls,
		"empty env-var value in [sandbox.secrets] = warn-only; sync must not fire")
}

// TestSetup_NoSyncWhenEnvUnset verifies the no-op case: env var isn't
// populated, so sync skips silently and the sandbox is created without it.
// The early checkSecrets prints a warning instead.
func TestSetup_NoSyncWhenEnvUnset(t *testing.T) {
	// Explicitly clear in case the parent process has it set.
	t.Setenv("FOO_API_KEY", "")

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(configWithFooSecret)
	r := newHappyRunner()
	r.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(emptySecretLs))

	p := prompt.NewFakePrompter(true)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.Empty(t, r.RunStdinCalls, "no env var → no sync")
}

// TestSetup_DryRunDoesNotActuallySet verifies that --dry-run logs the planned
// secret-set step but never invokes it.
func TestSetup_DryRunDoesNotActuallySet(t *testing.T) {
	t.Setenv("FOO_API_KEY", "sk-from-env")

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(configWithFooSecret)
	r := newHappyRunner()
	r.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(emptySecretLs))

	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{DryRun: true}, r, fs, p)

	require.NoError(t, err)
	assert.Empty(t, r.RunStdinCalls,
		"dry-run must not actually invoke sbx secret set")
}

// TestStart_SyncsSecretAfterFreshCreate verifies that the same env-var-to-
// per-sandbox-secret behaviour applies when `sbxgo run` creates a sandbox
// (no existing sandbox).
func TestStart_SyncsSecretAfterFreshCreate(t *testing.T) {
	t.Setenv("BAR_API_KEY", "tok-test")

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(
		"[sandbox]\nagent = \"claude\"\n" +
			"[sandbox.secrets]\nbar = \"BAR_API_KEY\"\n",
	)
	r := newHappyRunner()
	r.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(emptySecretLs))

	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	require.Len(t, r.RunStdinCalls, 1)
	assert.Equal(t, []string{"secret", "set", sandboxName, "bar"}, r.RunStdinCalls[0].Args)
	assert.Equal(t, "tok-test", r.RunStdinCalls[0].Stdin)
}

// TestStart_ResumeDoesNotSync verifies that resuming an existing sandbox
// does not re-set secrets — they persist with the sandbox.
func TestStart_ResumeDoesNotSync(t *testing.T) {
	t.Setenv("FOO_API_KEY", "sk-from-env")

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(configWithFooSecret)
	r := newRunnerWithExistingSandbox()
	r.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(emptySecretLs))

	p := prompt.NewFakePrompter(false)

	err := sandbox.Start(context.Background(), sandbox.StartOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.Empty(t, r.RunStdinCalls,
		"on resume, secrets persist with the sandbox; no sync should fire")
}
