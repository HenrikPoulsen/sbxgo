package sandbox_test

import (
	"testing"

	"github.com/HenrikPoulsen/sbxgo/internal/config"
	"github.com/HenrikPoulsen/sbxgo/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	agentClaude  = "claude"
	agentCopilot = "copilot"
	kitGo        = ".sbxgo/kits/go"
	kitNode      = ".sbxgo/kits/node"
	flagKit      = "--kit"
	flagClone    = "--clone"
	flagTemplate = "--template"
	templateName = "claude-myproject"
)

func TestSandboxName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		agent    string
		workdir  string
		expected string
	}{
		{agentClaude, "/some/path/SymbolServer", "claude-SymbolServer"},
		{agentClaude, "/home/user/myproject", templateName},
		{agentCopilot, "/repos/MyRepo", "copilot-MyRepo"},
		{agentClaude, "/path/proj.v1+rc", "claude-proj.v1+rc"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()

			got, err := sandbox.Name(tt.agent, tt.workdir)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestSandboxName_RejectsInvalidChars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		agent   string
		workdir string
	}{
		{"space in basename", agentClaude, "/path/My Project"},
		{"underscore in basename", agentClaude, "/path/my_project"},
		{"slash-only basename empty", agentClaude, "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := sandbox.Name(tt.agent, tt.workdir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid characters")
		})
	}
}

func TestBuildRunArgs_NoTemplate(t *testing.T) {
	t.Parallel()

	cfg := &config.SandboxConfig{Agent: agentClaude}
	args := sandbox.BuildRunArgs(cfg, false, "")

	assert.Equal(t, []string{agentClaude, "."}, args)
}

func TestBuildRunArgs_WithTemplate(t *testing.T) {
	t.Parallel()

	cfg := &config.SandboxConfig{Agent: agentClaude}
	args := sandbox.BuildRunArgs(cfg, true, templateName)

	assert.Equal(t, []string{flagTemplate, templateName, agentClaude, "."}, args)
}

func TestBuildRunArgs_WithKits(t *testing.T) {
	t.Parallel()

	cfg := &config.SandboxConfig{
		Agent: agentClaude,
		Kits:  []string{kitGo, kitNode},
	}
	args := sandbox.BuildRunArgs(cfg, false, "")

	assert.Equal(t, []string{flagKit, kitGo, flagKit, kitNode, agentClaude, "."}, args)
}

func TestBuildRunArgs_WithClone(t *testing.T) {
	t.Parallel()

	cfg := &config.SandboxConfig{Agent: agentClaude, Clone: true}
	args := sandbox.BuildRunArgs(cfg, false, "")

	assert.Equal(t, []string{flagClone, agentClaude, "."}, args)
}

func TestBuildRunArgs_WithExtraWorkspaces(t *testing.T) {
	t.Parallel()

	cfg := &config.SandboxConfig{
		Agent:           agentClaude,
		ExtraWorkspaces: []string{"/shared", "/tools"},
	}
	args := sandbox.BuildRunArgs(cfg, false, "")

	assert.Equal(t, []string{agentClaude, ".", "/shared", "/tools"}, args)
}

func TestBuildRunArgs_FullOptions(t *testing.T) {
	t.Parallel()

	cfg := &config.SandboxConfig{
		Agent:           agentClaude,
		Kits:            []string{kitGo},
		Clone:           true,
		ExtraWorkspaces: []string{"/extra"},
	}
	args := sandbox.BuildRunArgs(cfg, true, templateName)

	assert.Equal(t, []string{
		flagTemplate, templateName,
		flagKit, kitGo,
		flagClone,
		agentClaude, ".",
		"/extra",
	}, args)
}
