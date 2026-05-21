package conformance

import (
	"testing"
	"time"
)

// RunnerTarget identifies which binary a command case is being run against.
type RunnerTarget string

const (
	// RunnerRef selects the reference aria2c binary.
	RunnerRef RunnerTarget = "ref"
	// RunnerImpl selects the aria2go implementation binary.
	RunnerImpl RunnerTarget = "impl"
)

// CommandMatrixCase describes one data-driven ref-vs-impl command probe.
type CommandMatrixCase struct {
	Name       string
	Args       []string
	ArgsFor    func(RunnerTarget) []string
	Stdin      string
	Env        []string
	Dir        string
	DirFor     func(RunnerTarget) string
	Timeout    time.Duration
	Assert     func(*testing.T, CommandPairResult)
	SkipReason string
}

// CommandPairResult holds the paired outputs for one conformance command case.
type CommandPairResult struct {
	Case CommandMatrixCase
	Ref  RunResult
	Impl RunResult
}

// RunCommandMatrix runs each case as a subtest against aria2c and aria2go.
func RunCommandMatrix(t *testing.T, cases []CommandMatrixCase) {
	t.Helper()
	SkipIfNoRef(t)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.testName(), func(t *testing.T) {
			if tc.SkipReason != "" {
				t.Skip(tc.SkipReason)
			}
			result := RunCommandPair(t, tc)
			if tc.Assert != nil {
				tc.Assert(t, result)
			}
		})
	}
}

// RunCommandPair runs one command case against both binaries.
func RunCommandPair(t *testing.T, tc CommandMatrixCase) CommandPairResult {
	t.Helper()

	ref, err := RunCommandTarget(t, RunnerRef, tc)
	if err != nil {
		t.Fatalf("RunRef %s: %v", tc.testName(), err)
	}
	impl, err := RunCommandTarget(t, RunnerImpl, tc)
	if err != nil {
		t.Fatalf("RunImpl %s: %v", tc.testName(), err)
	}
	return CommandPairResult{Case: tc, Ref: ref, Impl: impl}
}

// RunCommandTarget runs one command case against the selected binary.
func RunCommandTarget(t *testing.T, target RunnerTarget, tc CommandMatrixCase) (RunResult, error) {
	t.Helper()

	args := tc.argsFor(target)
	opts := RunOptions{
		Env:     append([]string(nil), tc.Env...),
		Dir:     tc.dirFor(target),
		Timeout: tc.Timeout,
	}
	switch target {
	case RunnerRef:
		return RunRefWithOptions(t, args, tc.Stdin, opts)
	case RunnerImpl:
		return RunImplWithOptions(t, args, tc.Stdin, opts)
	default:
		t.Fatalf("unknown runner target %q", target)
		return RunResult{}, nil
	}
}

func (tc CommandMatrixCase) testName() string {
	if tc.Name != "" {
		return tc.Name
	}
	return "unnamed"
}

func (tc CommandMatrixCase) argsFor(target RunnerTarget) []string {
	if tc.ArgsFor != nil {
		return append([]string(nil), tc.ArgsFor(target)...)
	}
	return append([]string(nil), tc.Args...)
}

func (tc CommandMatrixCase) dirFor(target RunnerTarget) string {
	if tc.DirFor != nil {
		return tc.DirFor(target)
	}
	return tc.Dir
}
