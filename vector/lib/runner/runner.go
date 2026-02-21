// Package runner provides a shared command execution abstraction for running
// external programs, plus test helpers (MockRunner) for unit testing.
package runner

import (
	"fmt"
	"io"
	"os/exec"
)

// Func is the canonical function type for executing an external command.
// Consumers store a value of this type and call it to run shell commands;
// tests replace it with MockRunner.Run (or a custom closure) to avoid
// real process execution.
type Func func(stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error

// OutputFunc is a function type that executes an external command and
// returns its standard output. It mirrors the (*exec.Cmd).Output() pattern.
// Tests can replace the default with a mock to avoid real process execution.
type OutputFunc func(name string, args ...string) ([]byte, error)

// CombinedOutputFunc is a function type that executes an external command
// and returns its combined standard output and standard error. It mirrors
// the (*exec.Cmd).CombinedOutput() pattern.
// Tests can replace the default with a mock to avoid real process execution.
type CombinedOutputFunc func(name string, args ...string) ([]byte, error)

// Run is the default Func implementation. It executes the named program
// with the given arguments, wiring stdin/stdout/stderr to the supplied
// writers.
var Run Func = func(stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// Output is the default OutputFunc implementation. It executes the named
// program and returns its standard output, mirroring (*exec.Cmd).Output().
var Output OutputFunc = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// CombinedOutput is the default CombinedOutputFunc implementation. It
// executes the named program and returns its combined stdout and stderr,
// mirroring (*exec.Cmd).CombinedOutput().
var CombinedOutput CombinedOutputFunc = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// chrootArgs builds the unshare argument list for running a command inside
// a chroot. It preserves the exact invocation pattern:
//
//	unshare --pid --fork --kill-child --mount --uts --ipc \
//	    --mount-proc=<chrootDir>/proc chroot <chrootDir> <chrootExec> [args...]
func chrootArgs(chrootDir, chrootExec string, args ...string) ([]string, error) {
	if chrootDir == "" {
		return nil, fmt.Errorf("missing chrootDir parameter")
	}
	if chrootExec == "" {
		return nil, fmt.Errorf("missing chrootExec parameter")
	}

	cmdArgs := []string{
		"--pid",
		"--fork",
		"--kill-child",
		"--mount",
		"--uts",
		"--ipc",
		fmt.Sprintf("--mount-proc=%s/proc", chrootDir),
		"chroot",
		chrootDir,
		chrootExec,
	}
	cmdArgs = append(cmdArgs, args...)
	return cmdArgs, nil
}

// ChrootRunFunc is a function type that executes a command inside a chroot
// via unshare, wiring stdin/stdout/stderr to the supplied writers.
type ChrootRunFunc func(stdin io.Reader, stdout, stderr io.Writer, chrootDir, chrootExec string, args ...string) error

// ChrootOutputFunc is a function type that executes a command inside a chroot
// via unshare and returns its standard output.
type ChrootOutputFunc func(chrootDir, chrootExec string, args ...string) ([]byte, error)

// ChrootRun is the default ChrootRunFunc implementation.
var ChrootRun ChrootRunFunc = func(stdin io.Reader, stdout, stderr io.Writer, chrootDir, chrootExec string, args ...string) error {
	uArgs, err := chrootArgs(chrootDir, chrootExec, args...)
	if err != nil {
		return err
	}
	return Run(stdin, stdout, stderr, "unshare", uArgs...)
}

// ChrootOutput is the default ChrootOutputFunc implementation.
var ChrootOutput ChrootOutputFunc = func(chrootDir, chrootExec string, args ...string) ([]byte, error) {
	uArgs, err := chrootArgs(chrootDir, chrootExec, args...)
	if err != nil {
		return nil, err
	}
	return Output("unshare", uArgs...)
}
