package sbx_test

import (
	"context"
	"testing"

	"github.com/HenrikPoulsen/sbxgo/internal/runner"
	"github.com/HenrikPoulsen/sbxgo/internal/sbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	lsFlagJSON  = "--json"
	policyGroup = "policy"
)

const sampleListJSON = `{
  "sandboxes": [
    {
      "name": "claude-SymbolServer",
      "agent": "claude",
      "status": "stopped",
      "workspaces": ["D:\\DEVELOP\\SymbolServer"]
    },
    {
      "name": "claude-MyProject",
      "agent": "claude",
      "status": "running",
      "workspaces": ["D:\\DEVELOP\\MyProject", "D:\\DEVELOP\\Shared"]
    }
  ]
}`

func TestParseList(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{"ls", lsFlagJSON}, []byte(sampleListJSON))

	client := sbx.NewClient(fake)
	sandboxes, err := client.List(context.Background())

	require.NoError(t, err)
	require.Len(t, sandboxes, 2)

	assert.Equal(t, "claude-SymbolServer", sandboxes[0].Name)
	assert.Equal(t, "claude", sandboxes[0].Agent)
	assert.Equal(t, "stopped", sandboxes[0].Status)
	assert.Equal(t, []string{`D:\DEVELOP\SymbolServer`}, sandboxes[0].Workspaces)

	assert.Equal(t, "claude-MyProject", sandboxes[1].Name)
	assert.Equal(t, "running", sandboxes[1].Status)
	assert.Len(t, sandboxes[1].Workspaces, 2)
}

func TestParseList_Empty(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{"ls", lsFlagJSON}, []byte(`{"sandboxes": []}`))

	client := sbx.NewClient(fake)
	sandboxes, err := client.List(context.Background())

	require.NoError(t, err)
	assert.Empty(t, sandboxes)
}

func TestExists_Found(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{"ls", lsFlagJSON}, []byte(sampleListJSON))

	client := sbx.NewClient(fake)
	exists, err := client.Exists(context.Background(), "claude-SymbolServer")

	require.NoError(t, err)
	assert.True(t, exists)
}

func TestExists_NotFound(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{"ls", lsFlagJSON}, []byte(sampleListJSON))

	client := sbx.NewClient(fake)
	exists, err := client.Exists(context.Background(), "claude-NonExistent")

	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCurrentPolicy_Balanced(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{policyGroup, "ls", "--type", "network"},
		[]byte("Current default policy: balanced\n"))

	client := sbx.NewClient(fake)
	policy, err := client.CurrentPolicy(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "balanced", policy)
}

// TestCurrentPolicy_Unknown covers the issue-#126 case: `policy ls` may not
// surface the default-* row at all. parsePolicy returns "" so callers stay
// silent rather than nag with a useless "unknown" warning.
func TestCurrentPolicy_Unknown(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{policyGroup, "ls", "--type", "network"},
		[]byte("local:abc  network  local  allow  active  example.com\n"))

	client := sbx.NewClient(fake)
	policy, err := client.CurrentPolicy(context.Background())

	require.NoError(t, err)
	assert.Empty(t, policy)
}

// TestCurrentPolicy_SubstringFoolerIgnored verifies the parser does not pick up
// a policy keyword that only appears as part of a longer rule name. A user rule
// called "default-balanced-corp" must NOT register as `balanced`.
func TestCurrentPolicy_SubstringFoolerIgnored(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{policyGroup, "ls", "--type", "network"},
		[]byte("default-balanced-corp  network  local  allow  active  api.example.com\n"))

	client := sbx.NewClient(fake)
	policy, err := client.CurrentPolicy(context.Background())

	require.NoError(t, err)
	assert.Empty(t, policy, "tokenized parse should ignore substring-only matches")
}

func TestListSecrets(t *testing.T) {
	t.Parallel()

	tabular := "SCOPE      SERVICE   SECRET\n" +
		"(global)   github    test-v****\n" +
		"my-sbx     openai    sk-12****\n"

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(tabular))

	client := sbx.NewClient(fake)
	secrets, err := client.ListSecrets(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"github", "openai"}, secrets)
}

func TestListSecrets_Empty(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(""))

	client := sbx.NewClient(fake)
	secrets, err := client.ListSecrets(context.Background())

	require.NoError(t, err)
	assert.Empty(t, secrets)
}

// TestListSecrets_NoneFound covers the human-readable "No secrets found."
// message sbx prints when nothing is stored — there is no header row to parse.
func TestListSecrets_NoneFound(t *testing.T) {
	t.Parallel()

	msg := "No secrets found. Run 'sbx secret set --help' to see available services.\n"

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{"secret", "ls"}, []byte(msg))

	client := sbx.NewClient(fake)
	secrets, err := client.ListSecrets(context.Background())

	require.NoError(t, err)
	assert.Empty(t, secrets)
}

// TestListSandboxRules_ParsesAllowAndDenyAndContinuations covers the three
// row shapes parseSandboxRules has to handle: a single-resource allow row,
// a deny row, and a multi-resource rule whose extra resources appear on
// continuation lines (indented, no NAME/TYPE/ORIGIN columns).
func TestListSandboxRules_ParsesAllowAndDenyAndContinuations(t *testing.T) {
	t.Parallel()

	output := `NAME                                         TYPE      ORIGIN                 DECISION   STATUS   RESOURCES
local:abc                                    network   local                  allow      active   github.com
local:def                                    network   local                  deny       active   ads.example.com
kit:claude-sbxgo                             network   sandbox:claude-sbxgo   allow      active   api.github.com
                                                                                                  proxy.golang.org
`

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{policyGroup, "ls", "claude-sbxgo"}, []byte(output))

	client := sbx.NewClient(fake)
	rules, err := client.ListSandboxRules(context.Background(), "claude-sbxgo")

	require.NoError(t, err)
	assert.Equal(t, []sbx.PolicyRule{
		{Decision: "allow", Resource: "github.com"},
		{Decision: "deny", Resource: "ads.example.com"},
		{Decision: "allow", Resource: "api.github.com"},
		{Decision: "allow", Resource: "proxy.golang.org"},
	}, rules)
}

// TestListSandboxRules_EmptyOutput defends against the "no rules apply"
// case: empty stdout (or just a header row) must yield a nil/empty slice.
func TestListSandboxRules_EmptyOutput(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{policyGroup, "ls", "claude-sbxgo"}, []byte(""))

	client := sbx.NewClient(fake)
	rules, err := client.ListSandboxRules(context.Background(), "claude-sbxgo")

	require.NoError(t, err)
	assert.Empty(t, rules)
}

func TestAllowNetwork_SingleDomain(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := sbx.NewClient(fake)

	err := client.AllowNetwork(context.Background(), "claude-myproject", "github.com")

	require.NoError(t, err)
	require.Len(t, fake.RunCalls, 1)
	assert.Equal(t,
		[]string{policyGroup, "allow", "network", "claude-myproject", "github.com"},
		fake.RunCalls[0].Args)
}

func TestAllowNetwork_BatchesIntoOneCall(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := sbx.NewClient(fake)

	err := client.AllowNetwork(context.Background(), "claude-myproject", "github.com", "proxy.golang.org")

	require.NoError(t, err)
	require.Len(t, fake.RunCalls, 1)
	assert.Equal(t,
		[]string{policyGroup, "allow", "network", "claude-myproject", "github.com,proxy.golang.org"},
		fake.RunCalls[0].Args)
}

func TestAllowNetwork_EmptyIsNoOp(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := sbx.NewClient(fake)

	require.NoError(t, client.AllowNetwork(context.Background(), "claude-myproject"))
	assert.Empty(t, fake.RunCalls, "empty domains must not invoke sbx")
}

func TestDenyNetwork_BatchesIntoOneCall(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := sbx.NewClient(fake)

	err := client.DenyNetwork(context.Background(), "claude-myproject", "evil.com", "ads.example.com")

	require.NoError(t, err)
	require.Len(t, fake.RunCalls, 1)
	assert.Equal(t,
		[]string{policyGroup, "deny", "network", "claude-myproject", "evil.com,ads.example.com"},
		fake.RunCalls[0].Args)
}

func TestDenyNetwork_EmptyIsNoOp(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := sbx.NewClient(fake)

	require.NoError(t, client.DenyNetwork(context.Background(), "claude-myproject"))
	assert.Empty(t, fake.RunCalls)
}

func TestRemove_SendsCorrectCommand(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := sbx.NewClient(fake)

	err := client.Remove(context.Background(), "claude-myproject")

	require.NoError(t, err)
	require.Len(t, fake.RunCalls, 1)
	assert.Equal(t, []string{"rm", "--force", "claude-myproject"}, fake.RunCalls[0].Args)
}

func TestCreate_SendsCorrectCommand(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := sbx.NewClient(fake)

	err := client.Create(context.Background(), []string{"--kit", "go", "claude", "."})

	require.NoError(t, err)
	require.Len(t, fake.RunCalls, 1)
	assert.Equal(t, []string{"create", "--kit", "go", "claude", "."}, fake.RunCalls[0].Args)
}

func TestSetDebug_PrependsDebugFlag(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	fake.SetOutputResponse("sbx", []string{"--debug", "ls", lsFlagJSON}, []byte(`{"sandboxes":[]}`))

	client := sbx.NewClient(fake).SetDebug(true)

	_, err := client.List(context.Background())
	require.NoError(t, err)

	err = client.Remove(context.Background(), "claude-myproject")
	require.NoError(t, err)

	err = client.Create(context.Background(), []string{"--kit", "go", "claude", "."})
	require.NoError(t, err)

	require.Len(t, fake.OutputCalls, 1)
	assert.Equal(t, []string{"--debug", "ls", lsFlagJSON}, fake.OutputCalls[0].Args)

	require.Len(t, fake.RunCalls, 2)
	assert.Equal(t, []string{"--debug", "rm", "--force", "claude-myproject"}, fake.RunCalls[0].Args)
	assert.Equal(t, []string{"--debug", "create", "--kit", "go", "claude", "."}, fake.RunCalls[1].Args)
}

func TestDebugDisabled_DoesNotPrependFlag(t *testing.T) {
	t.Parallel()

	fake := runner.NewFakeRunner()
	client := sbx.NewClient(fake).SetDebug(false)

	err := client.Remove(context.Background(), "claude-myproject")
	require.NoError(t, err)

	require.Len(t, fake.RunCalls, 1)
	assert.Equal(t, []string{"rm", "--force", "claude-myproject"}, fake.RunCalls[0].Args)
}
