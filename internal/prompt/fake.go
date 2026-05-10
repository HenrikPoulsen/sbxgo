package prompt

// FakePrompter is a test fake for Prompter.
type FakePrompter struct {
	// Response is the value returned by Confirm.
	Response bool
	// Err is the error returned by Confirm (if non-nil).
	Err error
	// Calls records all questions passed to Confirm.
	Calls []string
	// Defaults records the defaultYes flag passed for each call, indexed alongside Calls.
	Defaults []bool
}

// NewFakePrompter creates a FakePrompter with a default Response of false.
func NewFakePrompter(response bool) *FakePrompter {
	return &FakePrompter{Response: response}
}

// Confirm records the question and returns the configured response.
func (f *FakePrompter) Confirm(question string, defaultYes bool) (bool, error) {
	f.Calls = append(f.Calls, question)
	f.Defaults = append(f.Defaults, defaultYes)

	return f.Response, f.Err
}
