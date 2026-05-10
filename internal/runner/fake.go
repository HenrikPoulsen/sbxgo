package runner

import (
	"context"
	"strings"

	"github.com/rotisserie/eris"
)

// Call records a single invocation of a CommandRunner method.
type Call struct {
	Name string
	Args []string
}

// FakeRunner is a test fake for CommandRunner that records calls.
type FakeRunner struct {
	// RunCalls records all calls to Run.
	RunCalls []Call
	// OutputCalls records all calls to Output.
	OutputCalls []Call
	// RunError is returned by Run (if non-nil).
	RunError error
	// OutputResponses maps "name arg0 arg1..." to the bytes to return.
	// If a key is not found, empty bytes are returned.
	OutputResponses map[string][]byte
	// OutputError is returned by Output (if non-nil).
	OutputError error
}

// NewFakeRunner creates a new FakeRunner with an empty response map.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{
		OutputResponses: make(map[string][]byte),
	}
}

// SetOutputResponse configures the response for a given command+args combination.
func (f *FakeRunner) SetOutputResponse(name string, args []string, data []byte) {
	key := commandKey(name, args)
	f.OutputResponses[key] = data
}

func commandKey(name string, args []string) string {
	var b strings.Builder
	b.WriteString(name)

	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(a)
	}

	return b.String()
}

// Run records the call and returns RunError.
func (f *FakeRunner) Run(_ context.Context, name string, args ...string) error {
	f.RunCalls = append(f.RunCalls, Call{Name: name, Args: args})
	return f.RunError
}

// Output records the call and returns configured output or OutputError.
func (f *FakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.OutputCalls = append(f.OutputCalls, Call{Name: name, Args: args})
	if f.OutputError != nil {
		return nil, f.OutputError
	}

	key := commandKey(name, args)
	if data, ok := f.OutputResponses[key]; ok {
		return data, nil
	}

	return nil, eris.Errorf("fake: no response configured for %q", key)
}
