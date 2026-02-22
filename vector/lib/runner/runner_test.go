package runner

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Run / Output / CombinedOutput – real execution with a trivial command
// ---------------------------------------------------------------------------

func TestRun_Echo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run(nil, &stdout, &stderr, "echo", "hello")
	if err != nil {
		t.Fatalf("Run(echo hello): unexpected error: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "hello" {
		t.Errorf("stdout = %q, want %q", got, "hello")
	}
}

func TestRun_Failure(t *testing.T) {
	err := Run(nil, io.Discard, io.Discard, "false")
	if err == nil {
		t.Fatal("Run(false): expected error, got nil")
	}
}

func TestOutput_Echo(t *testing.T) {
	out, err := Output("echo", "world")
	if err != nil {
		t.Fatalf("Output(echo world): unexpected error: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "world" {
		t.Errorf("output = %q, want %q", got, "world")
	}
}

func TestCombinedOutput_Echo(t *testing.T) {
	out, err := CombinedOutput("echo", "combined")
	if err != nil {
		t.Fatalf("CombinedOutput(echo combined): unexpected error: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "combined" {
		t.Errorf("output = %q, want %q", got, "combined")
	}
}

// ---------------------------------------------------------------------------
// chrootArgs
// ---------------------------------------------------------------------------

func TestChrootArgs_Valid(t *testing.T) {
	args, err := chrootArgs("/mnt/root", "/bin/bash", "-c", "ls")
	if err != nil {
		t.Fatalf("chrootArgs: unexpected error: %v", err)
	}

	expected := []string{
		"--pid", "--fork", "--kill-child",
		"--mount", "--uts", "--ipc",
		"--mount-proc=/mnt/root/proc",
		"chroot", "/mnt/root", "/bin/bash",
		"-c", "ls",
	}
	if len(args) != len(expected) {
		t.Fatalf("len(args) = %d, want %d", len(args), len(expected))
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestChrootArgs_NoExtraArgs(t *testing.T) {
	args, err := chrootArgs("/chroot", "/bin/sh")
	if err != nil {
		t.Fatalf("chrootArgs: unexpected error: %v", err)
	}
	if args[len(args)-1] != "/bin/sh" {
		t.Errorf("last arg = %q, want %q", args[len(args)-1], "/bin/sh")
	}
}

func TestChrootArgs_EmptyDir(t *testing.T) {
	_, err := chrootArgs("", "/bin/sh")
	if err == nil {
		t.Fatal("expected error for empty chrootDir")
	}
}

func TestChrootArgs_EmptyExec(t *testing.T) {
	_, err := chrootArgs("/mnt", "")
	if err == nil {
		t.Fatal("expected error for empty chrootExec")
	}
}

// ---------------------------------------------------------------------------
// ChrootRun / ChrootOutput – with mocked Run/Output
// ---------------------------------------------------------------------------

func TestChrootRun_DelegatesToRun(t *testing.T) {
	origRun := Run
	defer func() { Run = origRun }()

	var captured struct {
		name string
		args []string
	}
	Run = func(_ io.Reader, _, _ io.Writer, name string, args ...string) error {
		captured.name = name
		captured.args = args
		return nil
	}

	err := ChrootRun(nil, io.Discard, io.Discard, "/mnt", "/bin/bash", "-c", "ls")
	if err != nil {
		t.Fatalf("ChrootRun: unexpected error: %v", err)
	}
	if captured.name != "unshare" {
		t.Errorf("command = %q, want %q", captured.name, "unshare")
	}
	if captured.args[len(captured.args)-1] != "ls" {
		t.Errorf("last arg = %q, want %q", captured.args[len(captured.args)-1], "ls")
	}
}

func TestChrootRun_ErrorOnEmptyDir(t *testing.T) {
	err := ChrootRun(nil, io.Discard, io.Discard, "", "/bin/bash")
	if err == nil {
		t.Fatal("expected error for empty chrootDir")
	}
}

func TestChrootOutput_DelegatesToOutput(t *testing.T) {
	origOutput := Output
	defer func() { Output = origOutput }()

	Output = func(name string, args ...string) ([]byte, error) {
		return []byte("mocked"), nil
	}

	out, err := ChrootOutput("/mnt", "/bin/echo")
	if err != nil {
		t.Fatalf("ChrootOutput: unexpected error: %v", err)
	}
	if string(out) != "mocked" {
		t.Errorf("output = %q, want %q", string(out), "mocked")
	}
}

func TestChrootOutput_ErrorOnEmptyExec(t *testing.T) {
	_, err := ChrootOutput("/mnt", "")
	if err == nil {
		t.Fatal("expected error for empty chrootExec")
	}
}

// ---------------------------------------------------------------------------
// MockRunner basics
// ---------------------------------------------------------------------------

func TestMockRunner_Success(t *testing.T) {
	mr := NewMockRunner()
	err := mr.Run(nil, io.Discard, io.Discard, "cmd", "a", "b")
	if err != nil {
		t.Fatalf("MockRunner.Run: unexpected error: %v", err)
	}
	if len(mr.Calls) != 1 {
		t.Fatalf("len(Calls) = %d, want 1", len(mr.Calls))
	}
	if mr.Calls[0].Name != "cmd" {
		t.Errorf("Calls[0].Name = %q, want %q", mr.Calls[0].Name, "cmd")
	}
}

func TestMockRunner_FailOnCall(t *testing.T) {
	testErr := errors.New("boom")
	mr := NewMockRunnerFailOnCall(1, testErr)

	// Call 0 succeeds
	if err := mr.Run(nil, io.Discard, io.Discard, "a"); err != nil {
		t.Fatalf("call 0: unexpected error: %v", err)
	}
	// Call 1 fails
	if err := mr.Run(nil, io.Discard, io.Discard, "b"); !errors.Is(err, testErr) {
		t.Fatalf("call 1: got %v, want %v", err, testErr)
	}
	// Call 2 succeeds
	if err := mr.Run(nil, io.Discard, io.Discard, "c"); err != nil {
		t.Fatalf("call 2: unexpected error: %v", err)
	}
}

func TestMockRunner_OutputWithData(t *testing.T) {
	mr := NewMockRunnerWithOutput(map[int][]byte{
		0: []byte("first"),
		1: []byte("second"),
	})

	out0, err := mr.Output("cmd0")
	if err != nil {
		t.Fatalf("Output call 0: %v", err)
	}
	out1, err := mr.Output("cmd1")
	if err != nil {
		t.Fatalf("Output call 1: %v", err)
	}
	if string(out0) != "first" {
		t.Errorf("out0 = %q, want %q", string(out0), "first")
	}
	if string(out1) != "second" {
		t.Errorf("out1 = %q, want %q", string(out1), "second")
	}
}
