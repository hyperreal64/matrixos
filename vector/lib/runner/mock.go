package runner

import "io"

// MockRunnerCall records a single command invocation.
type MockRunnerCall struct {
	Name string
	Args []string
}

// MockRunner records calls and returns configurable errors.
// Use NewMockRunner for a runner that always succeeds, or
// NewMockRunnerFailOnCall to fail on a specific invocation index.
type MockRunner struct {
	Calls  []MockRunnerCall
	Err    error
	FailOn int // Fail on this call index (0-based), -1 means always fail if Err != nil

	// OutputData maps a call index (0-based) to the byte slice returned by
	// Output or CombinedOutput for that invocation. If no entry exists for
	// the current call index, an empty slice is returned.
	OutputData map[int][]byte
}

// Run implements the Func signature.
func (mr *MockRunner) Run(stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	mr.Calls = append(mr.Calls, MockRunnerCall{Name: name, Args: args})
	if mr.FailOn >= 0 && len(mr.Calls)-1 == mr.FailOn {
		return mr.Err
	}
	if mr.FailOn < 0 && mr.Err != nil {
		return mr.Err
	}
	return nil
}

// errForCall returns the error for the current call index, if any.
func (mr *MockRunner) errForCall() error {
	idx := len(mr.Calls) - 1
	if mr.FailOn >= 0 && idx == mr.FailOn {
		return mr.Err
	}
	if mr.FailOn < 0 && mr.Err != nil {
		return mr.Err
	}
	return nil
}

// outputForCall returns the output data configured for the current call index.
func (mr *MockRunner) outputForCall() []byte {
	if mr.OutputData == nil {
		return nil
	}
	return mr.OutputData[len(mr.Calls)-1]
}

// Output implements the OutputFunc signature.
func (mr *MockRunner) Output(name string, args ...string) ([]byte, error) {
	mr.Calls = append(mr.Calls, MockRunnerCall{Name: name, Args: args})
	return mr.outputForCall(), mr.errForCall()
}

// CombinedOutput implements the CombinedOutputFunc signature.
func (mr *MockRunner) CombinedOutput(name string, args ...string) ([]byte, error) {
	mr.Calls = append(mr.Calls, MockRunnerCall{Name: name, Args: args})
	return mr.outputForCall(), mr.errForCall()
}

// ChrootRun implements the ChrootRunFunc signature.
func (mr *MockRunner) ChrootRun(stdin io.Reader, stdout, stderr io.Writer, chrootDir, chrootExec string, args ...string) error {
	mr.Calls = append(mr.Calls, MockRunnerCall{Name: "chroot:" + chrootExec, Args: args})
	if mr.FailOn >= 0 && len(mr.Calls)-1 == mr.FailOn {
		return mr.Err
	}
	if mr.FailOn < 0 && mr.Err != nil {
		return mr.Err
	}
	return nil
}

// ChrootOutput implements the ChrootOutputFunc signature.
func (mr *MockRunner) ChrootOutput(chrootDir, chrootExec string, args ...string) ([]byte, error) {
	mr.Calls = append(mr.Calls, MockRunnerCall{Name: "chroot:" + chrootExec, Args: args})
	return mr.outputForCall(), mr.errForCall()
}

// NewMockRunner creates a MockRunner that always succeeds.
func NewMockRunner() *MockRunner {
	return &MockRunner{FailOn: -1}
}

// NewMockRunnerFailOnCall creates a MockRunner that returns err on the n-th
// call (0-based) and succeeds on all others.
func NewMockRunnerFailOnCall(n int, err error) *MockRunner {
	return &MockRunner{FailOn: n, Err: err}
}

// NewMockRunnerWithOutput creates a MockRunner that always succeeds and
// returns the given output data for each call index.
func NewMockRunnerWithOutput(data map[int][]byte) *MockRunner {
	return &MockRunner{FailOn: -1, OutputData: data}
}
