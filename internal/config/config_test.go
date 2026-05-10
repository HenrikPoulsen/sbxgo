package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HenrikPoulsen/sbxgo/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validTOML = `
[sandbox]
agent          = "claude"
network_policy = "deny-all"
branch         = "auto"

allowed_domains  = ["proxy.golang.org"]
denied_domains   = ["evil.com"]
kits             = [".sbxgo/kits/go"]
required_secrets = ["ANTHROPIC_API_KEY"]
extra_workspaces = ["/extra"]

[sandbox.docker.build]
dockerfile = ".sbxgo/Dockerfile"
`

func writeTempTOML(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func TestLoadConfig_Valid(t *testing.T) {
	t.Parallel()

	path := writeTempTOML(t, validTOML)

	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "claude", cfg.Sandbox.Agent)
	require.NotNil(t, cfg.Sandbox.Docker)
	require.NotNil(t, cfg.Sandbox.Docker.Build)
	assert.Equal(t, ".sbxgo/Dockerfile", cfg.Sandbox.Docker.Build.Dockerfile)
	assert.Equal(t, ".", cfg.Sandbox.Docker.Build.Context)
	assert.Equal(t, config.PolicyDenyAll, cfg.Sandbox.NetworkPolicy)
	assert.Equal(t, "auto", cfg.Sandbox.Branch)
	assert.Equal(t, []string{"proxy.golang.org"}, cfg.Sandbox.AllowedDomains)
	assert.Equal(t, []string{"evil.com"}, cfg.Sandbox.DeniedDomains)
	assert.Equal(t, []string{".sbxgo/kits/go"}, cfg.Sandbox.Kits)
	assert.Equal(t, []string{"ANTHROPIC_API_KEY"}, cfg.Sandbox.RequiredSecrets)
	assert.Equal(t, []string{"/extra"}, cfg.Sandbox.ExtraWorkspaces)
}

func TestLoadConfig_MissingAgent(t *testing.T) {
	t.Parallel()

	toml := `
[sandbox]
network_policy = "balanced"
`
	path := writeTempTOML(t, toml)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent")
}

func TestLoadConfig_InvalidNetworkPolicy(t *testing.T) {
	t.Parallel()

	toml := `
[sandbox]
agent          = "claude"
network_policy = "super-strict"
`
	path := writeTempTOML(t, toml)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network_policy")
}

func TestValidate_ValidPolicies(t *testing.T) {
	t.Parallel()

	for _, policy := range []string{"allow-all", "balanced", "deny-all"} {
		tomlStr := "[sandbox]\nagent = \"claude\"\nnetwork_policy = \"" + policy + "\"\n"
		path := writeTempTOML(t, tomlStr)
		_, err := config.Load(path)
		require.NoError(t, err, "policy %q should be valid", policy)
	}
}

func TestValidate_InvalidPolicy(t *testing.T) {
	t.Parallel()

	tomlStr := "[sandbox]\nagent = \"claude\"\nnetwork_policy = \"unknown\"\n"
	path := writeTempTOML(t, tomlStr)
	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network_policy")
}

func TestParse_ValidBytes(t *testing.T) {
	t.Parallel()

	data := []byte(`
[sandbox]
agent          = "claude"
network_policy = "allow-all"
kits           = [".sbxgo/kits/go"]
`)

	cfg, err := config.Parse(data, "in-memory")

	require.NoError(t, err)
	assert.Equal(t, "claude", cfg.Sandbox.Agent)
	assert.Equal(t, config.PolicyAllowAll, cfg.Sandbox.NetworkPolicy)
	assert.Equal(t, []string{".sbxgo/kits/go"}, cfg.Sandbox.Kits)
	assert.Nil(t, cfg.Sandbox.Docker, "docker section should be nil when omitted")
}

func TestParse_DefaultsNetworkPolicy(t *testing.T) {
	t.Parallel()

	data := []byte("[sandbox]\nagent = \"claude\"\n")

	cfg, err := config.Parse(data, "in-memory")

	require.NoError(t, err)
	assert.Equal(t, config.PolicyDenyAll, cfg.Sandbox.NetworkPolicy)
}

func TestParse_InvalidTOML(t *testing.T) {
	t.Parallel()

	data := []byte("not valid toml %%%")

	_, err := config.Parse(data, "in-memory")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "in-memory")
}

func TestParse_MissingAgent(t *testing.T) {
	t.Parallel()

	data := []byte("[sandbox]\nnetwork_policy = \"balanced\"\n")

	_, err := config.Parse(data, "in-memory")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent")
}

func TestParse_DockerImage(t *testing.T) {
	t.Parallel()

	data := []byte(`
[sandbox]
agent = "claude"

[sandbox.docker]
image = "ghcr.io/acme/dev:1.4.0"
`)

	cfg, err := config.Parse(data, "in-memory")
	require.NoError(t, err)
	require.NotNil(t, cfg.Sandbox.Docker)
	assert.Equal(t, "ghcr.io/acme/dev:1.4.0", cfg.Sandbox.Docker.Image)
	assert.Nil(t, cfg.Sandbox.Docker.Build)
}

func TestParse_DockerBuildDefaults(t *testing.T) {
	t.Parallel()

	data := []byte(`
[sandbox]
agent = "claude"

[sandbox.docker.build]
`)

	cfg, err := config.Parse(data, "in-memory")
	require.NoError(t, err)
	require.NotNil(t, cfg.Sandbox.Docker)
	require.NotNil(t, cfg.Sandbox.Docker.Build)
	assert.Equal(t, ".", cfg.Sandbox.Docker.Build.Context)
	assert.Equal(t, ".sbxgo/Dockerfile", cfg.Sandbox.Docker.Build.Dockerfile)
}

func TestParse_DockerBuildExplicit(t *testing.T) {
	t.Parallel()

	data := []byte(`
[sandbox]
agent = "claude"

[sandbox.docker.build]
context    = "./build"
dockerfile = "build/Dockerfile.dev"
`)

	cfg, err := config.Parse(data, "in-memory")
	require.NoError(t, err)
	require.NotNil(t, cfg.Sandbox.Docker.Build)
	assert.Equal(t, "./build", cfg.Sandbox.Docker.Build.Context)
	assert.Equal(t, "build/Dockerfile.dev", cfg.Sandbox.Docker.Build.Dockerfile)
}

func TestParse_DockerImageAndBuildIsError(t *testing.T) {
	t.Parallel()

	data := []byte(`
[sandbox]
agent = "claude"

[sandbox.docker]
image = "alpine:3"

[sandbox.docker.build]
dockerfile = "Dockerfile"
`)

	_, err := config.Parse(data, "in-memory")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
}

func TestParse_DockerEmptySectionIsError(t *testing.T) {
	t.Parallel()

	data := []byte(`
[sandbox]
agent = "claude"

[sandbox.docker]
`)

	_, err := config.Parse(data, "in-memory")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
}
