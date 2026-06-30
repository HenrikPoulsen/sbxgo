package sbx_test

import (
	"context"
	"testing"

	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleVersionOutput = `Client Version:  v0.31.1 7055fecde6b84aeb963d1680879e5620af15c119
Server Version:  Unavailable (daemon not running — use 'sbx daemon start')
`

func TestVersion_ParsesClientLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "multi-line (pre-0.32.0 and -D)",
			out:  sampleVersionOutput,
			want: "0.31.1",
		},
		{
			name: "single-line (sbx 0.32.0+ default)",
			out:  "sbx version: v0.34.0 2eae0c4fc3894475da3318615f69783b0e7be747\n",
			want: "0.34.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := runner.NewFakeRunner()
			fake.SetOutputResponse("sbx", []string{"version"}, []byte(tt.out))

			client := sbx.NewClient(fake)
			v, err := client.Version(context.Background())

			require.NoError(t, err)
			assert.Equal(t, tt.want, v)
		})
	}
}

func TestVersion_Unparseable(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{"version"}, []byte("garbage output\n"))

	client := sbx.NewClient(fake)
	_, err := client.Version(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not parse sbx version")
}

func TestCheckMinVersion_Accepts(t *testing.T) {
	t.Parallel()

	tests := []string{
		"sbx version: v0.34.0 abc\n",
		"Client Version:  v0.34.0 abc\n",
		"Client Version:  v0.34.1 abc\n",
		"Client Version:  v0.40.0 abc\n",
		"Client Version:  v1.0.0 abc\n",
	}

	for _, out := range tests {
		t.Run(out, func(t *testing.T) {
			t.Parallel()

			fake := runner.NewFakeRunner()
			fake.SetOutputResponse("sbx", []string{"version"}, []byte(out))

			client := sbx.NewClient(fake)
			require.NoError(t, client.CheckMinVersion(context.Background()))
		})
	}
}

func TestCheckMinVersion_Rejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
	}{
		{"older minor", "Client Version:  v0.29.0 abc\n"},
		{"older patch", "Client Version:  v0.33.0 abc\n"},
		{"pre-release of min", "Client Version:  v0.34.0-rc1 abc\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := runner.NewFakeRunner()
			fake.SetOutputResponse("sbx", []string{"version"}, []byte(tt.out))

			client := sbx.NewClient(fake)
			err := client.CheckMinVersion(context.Background())

			require.Error(t, err)
			assert.Contains(t, err.Error(), "older than the minimum required")
			assert.Contains(t, err.Error(), sbx.MinVersion)
		})
	}
}
