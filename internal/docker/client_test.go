package docker_test

import (
	"context"
	"testing"

	"github.com/HenrikPoulsen/sbxgo/internal/docker"
	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuild_SendsCorrectCommand(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := docker.NewClient(fake)

	err := client.Build(context.Background(), "/tmp/iid", "claude-myproj", ".sbxgo/Dockerfile", "./build")

	require.NoError(t, err)
	require.Len(t, fake.RunCalls, 1)
	assert.Equal(t, "docker", fake.RunCalls[0].Name)
	assert.Equal(t,
		[]string{"build", "--iidfile", "/tmp/iid", "-t", "claude-myproj", "-f", ".sbxgo/Dockerfile", "./build"},
		fake.RunCalls[0].Args)
}

// TestBuild_DefaultsContextToCwd verifies that an empty buildContext falls back to ".".
func TestBuild_DefaultsContextToCwd(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := docker.NewClient(fake)

	err := client.Build(context.Background(), "/tmp/iid", "tag", ".sbxgo/Dockerfile", "")

	require.NoError(t, err)
	require.Len(t, fake.RunCalls, 1)
	// Last positional arg is the context; expect "." rather than the empty string we passed.
	args := fake.RunCalls[0].Args
	assert.Equal(t, ".", args[len(args)-1])
}

func TestPull_SendsCorrectCommand(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := docker.NewClient(fake)

	err := client.Pull(context.Background(), "ghcr.io/acme/dev:1.4.0")

	require.NoError(t, err)
	require.Len(t, fake.RunCalls, 1)
	assert.Equal(t, "docker", fake.RunCalls[0].Name)
	assert.Equal(t, []string{"pull", "ghcr.io/acme/dev:1.4.0"}, fake.RunCalls[0].Args)
}

func TestTag_SendsCorrectCommand(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := docker.NewClient(fake)

	err := client.Tag(context.Background(), "ghcr.io/acme/dev:1.4.0", "claude-myproj")

	require.NoError(t, err)
	require.Len(t, fake.RunCalls, 1)
	assert.Equal(t, []string{"tag", "ghcr.io/acme/dev:1.4.0", "claude-myproj"}, fake.RunCalls[0].Args)
}

func TestSave_SendsCorrectCommand(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := docker.NewClient(fake)

	err := client.Save(context.Background(), "claude-myproj", "/tmp/sbx-template.tar")

	require.NoError(t, err)
	require.Len(t, fake.RunCalls, 1)
	assert.Equal(t,
		[]string{"image", "save", "-o", "/tmp/sbx-template.tar", "claude-myproj"},
		fake.RunCalls[0].Args)
}

func TestInspectID_SendsCorrectCommandAndTrimsOutput(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("docker",
		[]string{"image", "inspect", "--format", "{{.Id}}", "claude-myproj"},
		[]byte("sha256:abc123\n"))

	client := docker.NewClient(fake)
	id, err := client.InspectID(context.Background(), "claude-myproj")

	require.NoError(t, err)
	assert.Equal(t, "sha256:abc123", id, "expected trailing newline to be trimmed")
	require.Len(t, fake.OutputCalls, 1)
	assert.Equal(t,
		[]string{"image", "inspect", "--format", "{{.Id}}", "claude-myproj"},
		fake.OutputCalls[0].Args)
}
