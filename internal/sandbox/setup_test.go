package sandbox_test

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/HenrikPoulsen/sbxgo/internal/fsutil"
	"github.com/HenrikPoulsen/sbxgo/internal/prompt"
	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetup_ScaffoldsWhenNoConfig verifies that Setup creates config.toml and .gitignore
// from embedded templates on first run, then exits without contacting sbx.
func TestSetup_ScaffoldsWhenNoConfig(t *testing.T) {
	t.Parallel()

	fs := fsutil.NewFakeFileSystem()
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{Agent: "claude"}, r, fs, p)

	require.NoError(t, err)
	assert.Contains(t, string(fs.Files[sandbox.DefaultConfigPath]), "claude")
	assert.Contains(t, string(fs.Files[".sbxgo/.gitignore"]), ".image-id")
	assert.Empty(t, r.RunCalls, "no sbx commands run when scaffolding")
	assert.Empty(t, r.OutputCalls, "no sbx commands run when scaffolding")
}

// TestSetup_ScaffoldDefaultAgent verifies "claude" is used when no agent is specified.
func TestSetup_ScaffoldDefaultAgent(t *testing.T) {
	t.Parallel()

	fs := fsutil.NewFakeFileSystem()
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.Contains(t, string(fs.Files[sandbox.DefaultConfigPath]), "claude")
}

// TestSetup_CreatesNewSandbox verifies the happy path: config exists, no sandbox yet → create + attach.
func TestSetup_CreatesNewSandbox(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(true) // user confirms agent start

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasSbxCall(r.RunCalls, "create", "claude", "."),
		"expected sbx create with agent and workspace")
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName),
		"expected sbx run <sandbox-name> after user confirms start")
}

// TestSetup_DeclineAgentStartCreatesButDoesNotAttach verifies that declining the
// start-agent prompt still creates the sandbox via sbx create but skips sbx run.
func TestSetup_DeclineAgentStartCreatesButDoesNotAttach(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false) // user declines agent start

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	require.Len(t, p.Calls, 1, "expected exactly the start-agent prompt")
	assert.True(t, p.Defaults[0], "start-agent prompt should default to yes")
	assert.True(t, hasSbxCall(r.RunCalls, "create", "claude", "."),
		"expected sandbox to be created even when user declines starting the agent")
	assert.False(t, hasSbxCall(r.RunCalls, "run", sandboxName),
		"expected no sbx run when user declines starting the agent")
}

// TestSetup_ForceRecreatesSandbox verifies that --force removes an existing sandbox
// without prompting the user.
func TestSetup_ForceRecreatesSandbox(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{Force: true}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasSbxCall(r.RunCalls, "rm", "--force", sandboxName),
		"expected sbx rm --force %s", sandboxName)
	assert.True(t, hasSbxCall(r.RunCalls, "create", "claude", "."),
		"expected sandbox to be recreated after removal")
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName),
		"expected agent to be started after recreation when --force is set")
	assert.Empty(t, p.Calls, "expected no prompt when --force is set")
}

// TestSetup_UserConfirmsRecreation verifies that confirming the prompt leads to removal and recreation.
func TestSetup_UserConfirmsRecreation(t *testing.T) {
	t.Parallel()

	sandboxName := currentSandboxName()
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(true) // user says yes to both prompts

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	require.Len(t, p.Calls, 2, "expected recreate prompt followed by start-agent prompt")
	assert.Contains(t, p.Calls[0], sandboxName)
	assert.True(t, hasSbxCall(r.RunCalls, "rm", "--force", sandboxName))
	assert.True(t, hasSbxCall(r.RunCalls, "create", "claude", "."))
	assert.True(t, hasSbxCall(r.RunCalls, "run", sandboxName))
}

// TestSetup_UserAborts verifies that declining the prompt leaves the sandbox untouched.
func TestSetup_UserAborts(t *testing.T) {
	t.Parallel()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false) // user says no

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.Len(t, p.Calls, 1, "expected confirmation prompt")
	assert.False(t, hasSbxCall(r.RunCalls, "rm"), "expected no removal when user aborts")
	assert.False(t, hasSbxCall(r.RunCalls, "create"), "expected no creation when user aborts")
	assert.False(t, hasSbxCall(r.RunCalls, "run"), "expected no agent start when user aborts")
}

// TestSetup_DryRunPrintsCommand verifies that dry-run mode does not execute any sbx run/rm commands.
func TestSetup_DryRunPrintsCommand(t *testing.T) {
	t.Parallel()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{DryRun: true}, r, fs, p)

	require.NoError(t, err)
	assert.False(t, hasSbxCall(r.RunCalls, "create"), "expected no sbx create in dry-run mode")
	assert.False(t, hasSbxCall(r.RunCalls, "run"), "expected no sbx run in dry-run mode")
	assert.False(t, hasSbxCall(r.RunCalls, "rm"), "expected no sbx rm in dry-run mode")
}

// TestSetup_DryRun_ExistingSandbox_PreviewsRemoval is a regression test for
// the bug where setup --dry-run hid the destructive removal of an existing
// sandbox. Dry-run must (1) check whether a sandbox exists, (2) refrain from
// actually removing it, and (3) not prompt the user.
func TestSetup_DryRun_ExistingSandbox_PreviewsRemoval(t *testing.T) {
	t.Parallel()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{DryRun: true}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasSbxCall(r.OutputCalls, "ls", "--json"),
		"expected sbx ls --json so dry-run can surface the planned removal")
	assert.False(t, hasSbxCall(r.RunCalls, "rm"),
		"expected no sbx rm in dry-run mode")
	assert.Empty(t, p.Calls, "expected no prompt in dry-run mode")
}

// TestSetup_DryRun_ExistingSandbox_ForceStillPreviews verifies the --force
// dry-run path also shows the planned removal (without prompting).
func TestSetup_DryRun_ExistingSandbox_ForceStillPreviews(t *testing.T) {
	t.Parallel()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(minimalConfig)
	r := newRunnerWithExistingSandbox()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{DryRun: true, Force: true}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasSbxCall(r.OutputCalls, "ls", "--json"))
	assert.False(t, hasSbxCall(r.RunCalls, "rm"),
		"expected no sbx rm in dry-run mode even with --force")
	assert.Empty(t, p.Calls, "--force already skips prompts; dry-run shouldn't either")
}

func TestSetup_DoesNotFlipNetworkPolicy(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nnetwork_policy = \"deny-all\"\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner() // returns "balanced" for policy ls
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.False(t, hasSbxCall(r.RunCalls, "policy", "set-default"),
		"expected sbxgo to never call set-default; user must change the host-wide default manually")
}

// https://github.com/docker/sbx-releases/issues/126: policy ls can hide the default-* row when user rules exist.
func TestSetup_TolerantOfHiddenDefault(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nnetwork_policy = \"deny-all\"\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := runner.NewFakeRunner()
	r.SetOutputResponse("sbx", []string{"version"}, []byte(versionOK))
	r.SetOutputResponse("sbx", []string{"ls", "--json"}, []byte(emptyListJSON))
	// Simulate the #126 case: only user allow rules visible, no default-* row.
	r.SetOutputResponse("sbx", []string{"policy", "ls", "--type", "network"},
		[]byte("local:abc  network  local  allow  active  example.com\n"))

	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.False(t, hasSbxCall(r.RunCalls, "policy", "set-default"),
		"expected sbxgo to skip set-default when current policy is unknown")
}

// TestSetup_SkipsPolicyChangeWhenAlreadySet verifies no set-default call when policy matches.
func TestSetup_SkipsPolicyChangeWhenAlreadySet(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nnetwork_policy = \"balanced\"\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner() // already returns "balanced"
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.False(t, hasSbxCall(r.RunCalls, "policy", "set-default"),
		"expected no policy change when already at desired policy")
}

// hasDockerCall returns true if any recorded Run call to "docker" contains all the given args.
func hasDockerCall(calls []runner.Call, argsSubset ...string) bool {
	for _, call := range calls {
		if call.Name != "docker" {
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

// TestSetup_DockerImagePullsInsteadOfBuilding verifies that an image: source triggers
// `docker pull` and `docker image inspect`, not `docker build`.
func TestSetup_DockerImagePullsInsteadOfBuilding(t *testing.T) {
	t.Parallel()

	cfg := `
[sandbox]
agent = "claude"

[sandbox.docker]
image = "ghcr.io/acme/dev:1.4.0"
`
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	// docker image inspect is invoked via Output and must have a configured response.
	r.SetOutputResponse("docker",
		[]string{"image", "inspect", "--format", "{{.Id}}", mustSandboxName("claude", mustWD())},
		[]byte("sha256:abc123\n"))

	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasDockerCall(r.RunCalls, "pull", "ghcr.io/acme/dev:1.4.0"),
		"expected docker pull for the configured image")
	assert.True(t, hasDockerCall(r.RunCalls, "tag"),
		"expected docker tag to retag pulled image as the template name")
	assert.False(t, hasDockerCall(r.RunCalls, "build"),
		"expected no docker build for an image: source")
	assert.True(t, hasSbxCall(r.RunCalls, "template", "load"),
		"expected sbx template load after pulling a fresh image")
}

// TestSetup_DockerBuildHonorsContextAndDockerfile verifies the build branch invokes
// `docker build` with the configured -f and context.
func TestSetup_DockerBuildHonorsContextAndDockerfile(t *testing.T) {
	t.Parallel()

	cfg := `
[sandbox]
agent = "claude"

[sandbox.docker.build]
context    = "./build"
dockerfile = "build/Dockerfile.dev"
`
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	// Simulate the iidfile written by docker build.
	fs.Files[sandbox.ImageIDNewFile] = []byte("sha256:built\n")
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasDockerCall(r.RunCalls, "build", "-f", "build/Dockerfile.dev", "./build"),
		"expected docker build with configured dockerfile and context")
	assert.False(t, hasDockerCall(r.RunCalls, "pull"),
		"expected no docker pull for a build: source")
}

// TestSetup_DockerBuild_SkipsTemplateReloadWhenImageUnchanged verifies that
// when the freshly-built image ID matches the stored ID from a previous run,
// prepareTemplate skips the docker save / sbx template load roundtrip.
func TestSetup_DockerBuild_SkipsTemplateReloadWhenImageUnchanged(t *testing.T) {
	t.Parallel()

	cfg := `
[sandbox]
agent = "claude"

[sandbox.docker.build]
dockerfile = ".sbxgo/Dockerfile"
`
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	// Same ID in both files: a previous setup left ImageIDFile, and the new
	// build produced an identical ImageIDNewFile (Docker layer cache hit).
	same := []byte("sha256:abc123\n")
	fs.Files[sandbox.ImageIDFile] = same
	fs.Files[sandbox.ImageIDNewFile] = same
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasDockerCall(r.RunCalls, "build"),
		"docker build still runs — caching is up to docker, not sbxgo")
	assert.False(t, hasDockerCall(r.RunCalls, "save"),
		"expected no docker save when image ID is unchanged")
	assert.False(t, hasSbxCall(r.RunCalls, "template", "load"),
		"expected no sbx template load when image ID is unchanged")
}

// TestSetup_DockerBuild_LoadsTemplateWhenImageChanged covers the complementary
// path: when the freshly-built image ID differs from the stored one, we save
// the image, load it as an sbx template, and update the stored ID.
func TestSetup_DockerBuild_LoadsTemplateWhenImageChanged(t *testing.T) {
	t.Parallel()

	cfg := `
[sandbox]
agent = "claude"

[sandbox.docker.build]
dockerfile = ".sbxgo/Dockerfile"
`
	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	fs.Files[sandbox.ImageIDFile] = []byte("sha256:OLD\n")
	fs.Files[sandbox.ImageIDNewFile] = []byte("sha256:NEW\n")
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t, hasDockerCall(r.RunCalls, "save"),
		"expected docker save when image ID changed")
	assert.True(t, hasSbxCall(r.RunCalls, "template", "load"),
		"expected sbx template load when image ID changed")
	assert.Equal(t, "sha256:NEW\n", string(fs.Files[sandbox.ImageIDFile]),
		"expected stored image ID to be updated to the new value")
}

// mustWD returns the current working directory or panics.
func mustWD() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	return wd
}

// mustSandboxName returns sandbox.Name(agent, workdir) or panics on error.
func mustSandboxName(agent, workdir string) string {
	name, err := sandbox.Name(agent, workdir)
	if err != nil {
		panic(err)
	}

	return name
}

// TestSetup_AllowedDomainsApplied verifies that allowed_domains triggers policy allow calls.
func TestSetup_AllowedDomainsApplied(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nallowed_domains = [\"github.com\", \"proxy.golang.org\"]\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t,
		hasSbxCall(r.RunCalls, "policy", "allow", "network", "github.com,proxy.golang.org"),
		"expected a single batched policy allow call with both domains")
}
