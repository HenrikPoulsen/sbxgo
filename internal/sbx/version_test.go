package sbx_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
)

func TestVersion_ParsesClientVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "release",
			out:  "Client Version:  v0.29.0 7055fecde6b84aeb963d1680879e5620af15c119\nServer Version: Unavailable\n",
			want: "0.29.0",
		},
		{
			name: "prerelease stripped",
			out:  "Client Version:  v0.29.0-rc1 abc\n",
			want: "0.29.0",
		},
		{
			name: "no v prefix",
			out:  "Client Version: 1.2.3 deadbeef\n",
			want: "1.2.3",
		},
		{
			name: "windows newlines",
			out:  "Client Version:  v0.30.1 sha\r\nServer Version: Unavailable\r\n",
			want: "0.30.1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := runner.NewFakeRunner()
			fake.SetOutputResponse("sbx", []string{"version"}, []byte(tc.out))

			client := sbx.NewClient(fake)
			got, err := client.Version(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestVersion_ErrorsOnUnparseableOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		out  string
	}{
		{name: "empty", out: ""},
		{name: "no client line", out: "Server Version: v0.29.0\n"},
		{name: "missing version token", out: "Client Version:\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := runner.NewFakeRunner()
			fake.SetOutputResponse("sbx", []string{"version"}, []byte(tc.out))

			client := sbx.NewClient(fake)
			_, err := client.Version(context.Background())
			assert.Error(t, err)
		})
	}
}

func TestCheckMinVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "equal", version: "v" + sbx.MinVersion, wantErr: false},
		{name: "newer patch", version: "v0.29.5", wantErr: false},
		{name: "newer minor", version: "v0.30.0", wantErr: false},
		{name: "newer major", version: "v1.0.0", wantErr: false},
		{name: "older patch", version: "v0.28.3", wantErr: true},
		{name: "older minor", version: "v0.0.0", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := runner.NewFakeRunner()
			fake.SetOutputResponse("sbx", []string{"version"},
				[]byte("Client Version:  "+tc.version+" sha\n"))

			client := sbx.NewClient(fake)
			err := client.CheckMinVersion(context.Background())

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), sbx.MinVersion)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckMinVersion_PropagatesRunnerError(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	// No response configured → Output returns an error, simulating sbx not on PATH.

	client := sbx.NewClient(fake)
	err := client.CheckMinVersion(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sbx version")
}
