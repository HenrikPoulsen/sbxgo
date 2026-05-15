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
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
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

// TestSetup_NeverChangesHostWideDefault is a regression guard: sbxgo manages
// per-sandbox rules only and must never call `sbx policy set-default`, even
// when the configured network_policy differs from the host-wide default.
func TestSetup_NeverChangesHostWideDefault(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nnetwork_policy = \"deny-all\"\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	// Simulate a mismatch: host default is "balanced", config asks for "deny-all".
	r.SetOutputResponse("sbx", []string{"policy", "ls", "--type", "network"}, []byte("balanced"))

	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err, "mismatch should warn, not fail")
	assert.False(t, hasSbxCall(r.RunCalls, "policy", "set-default"),
		"sbxgo must never call set-default; the host-wide default is a user choice")
}

// TestSetup_HiddenDefaultTolerated covers the issue-#126 case: `policy ls`
// returns no recognizable token. We should proceed without warning rather
// than fail.
func TestSetup_HiddenDefaultTolerated(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nnetwork_policy = \"deny-all\"\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	r.SetOutputResponse("sbx", []string{"policy", "ls", "--type", "network"},
		[]byte("local:abc  network  local  allow  active  example.com\n"))

	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
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
		"docker build still runs; caching is up to docker, not sbxgo")
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

// TestSetup_AllowedDomainsApplied verifies that allowed_domains triggers a
// sandbox-scoped policy allow call (sbx 0.29.0+).
func TestSetup_AllowedDomainsApplied(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nallowed_domains = [\"github.com\", \"proxy.golang.org\"]\n"
	sandboxName := currentSandboxName()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	err := sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p)

	require.NoError(t, err)
	assert.True(t,
		hasSbxCall(r.RunCalls, "policy", "allow", "network", sandboxName, "github.com,proxy.golang.org"),
		"expected a single batched policy allow call scoped to the sandbox")
}

// TestSetup_DeniedDomainsApplied is the deny-side symmetry of
// TestSetup_AllowedDomainsApplied.
func TestSetup_DeniedDomainsApplied(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\ndenied_domains = [\"ads.example.com\"]\n"
	sandboxName := currentSandboxName()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	require.NoError(t, sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p))

	assert.True(t,
		hasSbxCall(r.RunCalls, "policy", "deny", "network", sandboxName, "ads.example.com"),
		"expected a sandbox-scoped policy deny call")
}

// TestSetup_BothAllowAndDenyApplied verifies both lists are emitted in a
// single setup, with allow before deny (deny wins per sbx semantics, but
// emission order is allow-first for predictable logs).
func TestSetup_BothAllowAndDenyApplied(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\n" +
		"allowed_domains = [\"github.com\"]\ndenied_domains = [\"ads.example.com\"]\n"
	sandboxName := currentSandboxName()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	require.NoError(t, sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p))

	allowIdx := indexOfSbxCall(r.RunCalls, "policy", "allow", "network", sandboxName)
	denyIdx := indexOfSbxCall(r.RunCalls, "policy", "deny", "network", sandboxName)

	require.GreaterOrEqual(t, allowIdx, 0, "expected a policy allow call")
	require.GreaterOrEqual(t, denyIdx, 0, "expected a policy deny call")
	assert.Less(t, allowIdx, denyIdx, "allow should be emitted before deny")
}

// TestSetup_SkipsAllowWhenAllAlreadyInPlace verifies the list-then-diff path:
// when every configured allowed_domain already appears in the sandbox's
// existing rules, sbxgo skips the `policy allow network` call entirely.
func TestSetup_SkipsAllowWhenAllAlreadyInPlace(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nallowed_domains = [\"github.com\", \"proxy.golang.org\"]\n"
	sandboxName := currentSandboxName()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	configureExistingRules(r, sandboxName, []sbx.PolicyRule{
		{Decision: "allow", Resource: "github.com"},
		{Decision: "allow", Resource: "proxy.golang.org"},
	})

	p := prompt.NewFakePrompter(false)

	require.NoError(t, sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p))

	assert.False(t, hasSbxCall(r.RunCalls, "policy", "allow", "network"),
		"expected no policy allow call when all configured rules are already present")
}

// TestSetup_AllowOnlyAddsTheDiff verifies that when some configured rules are
// already in place but others are new, the allow call is invoked with only
// the missing entries.
func TestSetup_AllowOnlyAddsTheDiff(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\n" +
		"allowed_domains = [\"github.com\", \"proxy.golang.org\", \"new.example.com\"]\n"
	sandboxName := currentSandboxName()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	configureExistingRules(r, sandboxName, []sbx.PolicyRule{
		{Decision: "allow", Resource: "github.com"},
		{Decision: "allow", Resource: "proxy.golang.org"},
	})

	p := prompt.NewFakePrompter(false)

	require.NoError(t, sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p))

	assert.True(t, hasSbxCall(r.RunCalls, "policy", "allow", "network", sandboxName, "new.example.com"),
		"expected policy allow to be called with only the new entry")
	assert.False(t, hasSbxCall(r.RunCalls, "policy", "allow", "network", sandboxName,
		"github.com,proxy.golang.org,new.example.com"),
		"expected the full configured list NOT to be re-applied")
}

// TestSetup_DryRunSkipsPolicyCalls is a regression guard: pre-migration the
// policy path ignored opts.DryRun and would call `sbx policy allow network ...`
// for real. Dry-run must not mutate any policy state.
func TestSetup_DryRunSkipsPolicyCalls(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\n" +
		"allowed_domains = [\"github.com\"]\ndenied_domains = [\"ads.example.com\"]\n"

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	require.NoError(t, sandbox.Setup(context.Background(), sandbox.SetupOptions{DryRun: true}, r, fs, p))

	assert.False(t, hasSbxCall(r.RunCalls, "policy", "allow"),
		"dry-run must not call sbx policy allow")
	assert.False(t, hasSbxCall(r.RunCalls, "policy", "deny"),
		"dry-run must not call sbx policy deny")
}

// TestSetup_PolicyAppliedAfterCreate is a regression guard: sbx 0.29.0
// rejects `policy allow network <sandbox> ...` if the sandbox does not yet
// exist, so applyPolicy must run *after* `sbx create`.
func TestSetup_PolicyAppliedAfterCreate(t *testing.T) {
	t.Parallel()

	cfg := "[sandbox]\nagent = \"claude\"\nallowed_domains = [\"github.com\"]\n"
	sandboxName := currentSandboxName()

	fs := fsutil.NewFakeFileSystem()
	fs.Files[sandbox.DefaultConfigPath] = []byte(cfg)
	r := newHappyRunner()
	p := prompt.NewFakePrompter(false)

	require.NoError(t, sandbox.Setup(context.Background(), sandbox.SetupOptions{}, r, fs, p))

	createIdx := indexOfSbxCall(r.RunCalls, "create")
	allowIdx := indexOfSbxCall(r.RunCalls, "policy", "allow", "network", sandboxName)

	require.GreaterOrEqual(t, createIdx, 0, "expected an sbx create call")
	require.GreaterOrEqual(t, allowIdx, 0, "expected a policy allow call")
	assert.Greater(t, allowIdx, createIdx,
		"policy allow must run after sbx create (sbx rejects rules for unknown sandboxes)")
}
