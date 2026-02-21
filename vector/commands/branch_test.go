package commands

import (
	"bytes"
	"matrixos/vector/lib/cds"
	"os"
	"testing"
)

// newTestBranchCommand creates a BranchCommand with injected mock dependencies,
// bypassing initConfig/initOstree which require real config files.
func newTestBranchCommand(ot cds.IOstree) *BranchCommand {
	cmd := &BranchCommand{}
	cmd.ot = ot
	return cmd
}

// captureStdout runs fn while capturing os.Stdout and returns the output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestBranchShow(t *testing.T) {
	mock := &mockOstree{
		deployments: []cds.Deployment{
			{
				Booted:    true,
				Checksum:  "abc123",
				Stateroot: "matrixos",
				Refspec:   "origin:matrixos/amd64/gnome",
				Index:     0,
				Serial:    1,
			},
		},
	}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"show"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	output := captureStdout(t, func() {
		if err := cmd.Run(); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	})

	expected := "Current branch:\n" +
		"  Name: matrixos\n" +
		"  Branch/Ref: origin:matrixos/amd64/gnome\n" +
		"  Checksum: abc123\n" +
		"  Index: 0\n" +
		"  Serial: 1\n"
	if output != expected {
		t.Errorf("output mismatch\nwant: %q\n got: %q", expected, output)
	}
}

func TestBranchShowNoBooted(t *testing.T) {
	mock := &mockOstree{
		deployments: []cds.Deployment{
			{Booted: false, Stateroot: "matrixos"},
		},
	}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"show"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error for no booted deployment, got nil")
	}
	if err.Error() != "could not find booted deployment" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBranchList(t *testing.T) {
	mock := &mockOstree{
		refs: []string{"origin:branch1", "origin:branch2"},
	}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"list"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	output := captureStdout(t, func() {
		if err := cmd.Run(); err != nil {
			t.Fatalf("Run failed: %v", err)
		}
	})

	expected := "origin:branch1\norigin:branch2\n"
	if output != expected {
		t.Errorf("output mismatch\nwant: %q\n got: %q", expected, output)
	}
}

func TestBranchSwitch(t *testing.T) {
	mock := &mockOstree{}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"switch", "new/branch"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if mock.switchRef != "new/branch" {
		t.Errorf("expected switch ref %q, got %q", "new/branch", mock.switchRef)
	}
}

func TestBranchSwitchMissingArg(t *testing.T) {
	mock := &mockOstree{}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"switch"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error for missing switch arg, got nil")
	}
}

func TestBranchUnknownSubcommand(t *testing.T) {
	mock := &mockOstree{}
	cmd := newTestBranchCommand(mock)
	if err := cmd.parseArgs([]string{"foo"}); err != nil {
		t.Fatalf("parseArgs failed: %v", err)
	}

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error for unknown subcommand, got nil")
	}
}

func TestBranchNoSubcommand(t *testing.T) {
	mock := &mockOstree{}
	cmd := newTestBranchCommand(mock)
	err := cmd.parseArgs([]string{})
	if err == nil {
		t.Fatal("expected error for missing subcommand, got nil")
	}
}
