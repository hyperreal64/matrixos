package commands

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// mockExecCommand is a helper function to mock exec.Command for testing.
func mockExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{" -test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

// TestHelperProcess isn't a real test. It's used as a helper for mockExecCommand.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No command to mock\n")
		os.Exit(1)
	}

	cmd, args := args[0], args[1:]
	if cmd == "ostree" && args[0] == "admin" && args[1] == "status" {
		fmt.Fprint(os.Stdout, `{
			"deployments": [
				{
					"booted": true,
					"checksum": "f2a3c7f8...",
					"origin": "remote:branch",
					"refspec": ["remote:branch/1/2"]
				}
			]
		}`)
	} else if cmd == "ostree" && args[0] == "remote" && args[1] == "refs" {
		fmt.Fprint(os.Stdout, "origin:branch1\norigin:branch2")
	} else if cmd == "ostree" && args[0] == "admin" && args[1] == "switch" {
		// Do nothing
	} else {
		fmt.Fprintf(os.Stderr, "Unknown command: %s %v\n", cmd, args)
		os.Exit(1)
	}
}

func TestBranchShow(t *testing.T) {
	execCommand = mockExecCommand
	defer func() { execCommand = exec.Command }()

	cmd := NewBranchCommand()
	err := cmd.Init([]string{"show"})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Redirect stdout to capture the output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = cmd.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	expected := "Current branch:\n  RefSpec: [remote:branch/1/2]\n  Checksum: f2a3c7f8...\n  Origin: remote:branch\n"
	if output != expected {
		t.Errorf("Expected output %q, but got %q", expected, output)
	}
}

func TestBranchList(t *testing.T) {
	execCommand = mockExecCommand
	defer func() { execCommand = exec.Command }()

	cmd := NewBranchCommand()
	err := cmd.Init([]string{"list"})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = cmd.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	expected := "origin:branch1\norigin:branch2"
	if output != expected {
		t.Errorf("Expected output %q, but got %q", expected, output)
	}
}

func TestBranchSwitch(t *testing.T) {
	execCommand = mockExecCommand
	defer func() { execCommand = exec.Command }()

	cmd := NewBranchCommand()
	err := cmd.Init([]string{"switch", "new/branch"})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Redirect stdout to check the "Switching to..." message
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = cmd.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	expectedMsg := "Switching to origin:new/branch...\n"
	if !strings.Contains(output, expectedMsg) {
		t.Errorf("Expected output to contain %q, but got %q", expectedMsg, output)
	}
}
