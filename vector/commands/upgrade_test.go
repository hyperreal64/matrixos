package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Constants for mock data
const (
	mockCurrentSHA = "old-sha"
	mockNewSHA     = "new-sha"
	mockRefSpec    = "remote:branch"
	stateroot      = "matrixos"
)

const ostreeStatusTmpl = `{
	"deployments": [
		{
			"booted": true,
			"checksum": "%s",
			"stateroot": "%s"
		}
	]
}`

// Helper type for command matching logic
type commandHandler func(args []string) bool

// Registry of mocked commands
var mockCommandHandlers = []commandHandler{
	handleOstreeStatus,
	handleOstreeRevParse,
	handleOstreeUpgradePull,
	handleOstreeUpgradeDeploy,
	handleOstreeListPackages,
	handleReboot,
}

// mockExecUpgradeCommand intercepts exec.Command calls
func mockExecUpgradeCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestUpgradeHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)

	// Pass through special environment variables for test configuration
	env := []string{"GO_WANT_UPGRADE_HELPER_PROCESS=1"}
	if val := os.Getenv("TEST_UPGRADE_CURRENT_SHA"); val != "" {
		env = append(env, "TEST_UPGRADE_CURRENT_SHA="+val)
	}
	cmd.Env = env
	return cmd
}

// TestUpgradeHelperProcess is the entry point for mocked subprocesses
func TestUpgradeHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_UPGRADE_HELPER_PROCESS") != "1" {
		return
	}
	// Helper process must exit
	defer os.Exit(0)

	// Strip test runner flags to get the actual command
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

	// Try to handle the command with registered handlers
	for _, handler := range mockCommandHandlers {
		if handler(args) {
			return
		}
	}

	fmt.Fprintf(os.Stderr, "Unknown command in test: %v\n", args)
	os.Exit(1)
}

// --- Command Handlers ---

func handleOstreeStatus(args []string) bool {
	// match: ostree --sysroot=... admin status --json
	if len(args) < 4 || args[0] != "ostree" || args[2] != "admin" || args[3] != "status" {
		return false
	}

	sha := mockCurrentSHA
	if val := os.Getenv("TEST_UPGRADE_CURRENT_SHA"); val != "" {
		sha = val
	}
	fmt.Printf(ostreeStatusTmpl, sha, stateroot)
	return true
}

func handleOstreeRevParse(args []string) bool {
	// match: ostree --repo=... rev-parse <ref>
	if len(args) < 3 || args[0] != "ostree" || args[2] != "rev-parse" {
		return false
	}
	// The mocked command returns the NEW available SHA
	fmt.Print(mockNewSHA)
	return true
}

func handleOstreeUpgradePull(args []string) bool {
	// match: ostree --sysroot=... admin upgrade --pull-only
	if len(args) < 5 || args[0] != "ostree" || args[2] != "admin" || args[3] != "upgrade" {
		return false
	}
	return args[4] == "--pull-only"
}

func handleOstreeUpgradeDeploy(args []string) bool {
	// match: ostree --sysroot=... admin upgrade --deploy-only
	// or:    ostree admin upgrade --deploy-only (no sysroot arg)

	if args[0] != "ostree" {
		return false
	}

	// Helper to find index of a string in slice
	idx := func(s string) int {
		for i, a := range args {
			if a == s {
				return i
			}
		}
		return -1
	}

	adminIdx := idx("admin")
	if adminIdx == -1 {
		return false
	}

	// Ensure we have enough args after "admin"
	if len(args) <= adminIdx+2 {
		return false
	}

	return args[adminIdx+1] == "upgrade" && args[adminIdx+2] == "--deploy-only"
}

func handleOstreeListPackages(args []string) bool {
	// match: ostree --repo=... ls -R <commit> -- <path>
	if len(args) < 6 || args[0] != "ostree" || args[2] != "ls" || args[3] != "-R" {
		return false
	}

	path := args[6]
	// Simulate failure for /usr/var-db-pkg to force fallback to /var/db/pkg
	if strings.Contains(path, "/usr/var-db-pkg") {
		os.Exit(1)
		return true
	}

	commit := args[4]
	switch commit {
	case mockCurrentSHA:
		fmt.Println("d00755 0 0 0 /var/db/pkg/app-misc/foo-1.0/")
		fmt.Println("-00644 0 0 0 /var/db/pkg/app-misc/foo-1.0/CONTENTS")
	case mockNewSHA:
		fmt.Println("d00755 0 0 0 /var/db/pkg/app-misc/foo-1.1/")
		fmt.Println("-00644 0 0 0 /var/db/pkg/app-misc/foo-1.1/CONTENTS")
	}
	return true
}

func handleReboot(args []string) bool {
	if args[0] == "reboot" {
		fmt.Println("Rebooting system...")
		return true
	}
	return false
}

// --- Test Setup Helper ---

type testEnv struct {
	tmpDir     string
	originFile string
	cleanup    func()
}

func setupUpgradeTest(t *testing.T, currentSHA string) *testEnv {
	// Redirect internal execCommand
	origExec := execCommand
	execCommand = mockExecUpgradeCommand

	// Mock root user
	origEuid := getEuid
	getEuid = func() int { return 0 }

	// Create temp fs
	tmpDir, err := os.MkdirTemp("", "upgrade-test")
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("ROOT", tmpDir)

	// Create origin file structure
	originDir := filepath.Join(tmpDir, "ostree/deploy", stateroot, "deploy")
	if err := os.MkdirAll(originDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create the .origin file
	originPath := filepath.Join(originDir, currentSHA+".0.origin")
	content := fmt.Sprintf("[origin]\nrefspec=%s\n", mockRefSpec)
	if err := os.WriteFile(originPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	return &testEnv{
		tmpDir:     tmpDir,
		originFile: originPath,
		cleanup: func() {
			os.RemoveAll(tmpDir)
			os.Unsetenv("ROOT")
			execCommand = origExec
			getEuid = origEuid
		},
	}
}

// --- Tests ---

func TestUpgradeRun(t *testing.T) {
	env := setupUpgradeTest(t, mockCurrentSHA)
	defer env.cleanup()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := NewUpgradeCommand()
	if err := cmd.Init([]string{"-y", "--reboot"}); err != nil {
		w.Close()
		os.Stdout = oldStdout
		t.Fatalf("Init failed: %v", err)
	}

	if err := cmd.Run(); err != nil {
		w.Close()
		os.Stdout = oldStdout
		t.Fatalf("Run failed: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := stripAnsi(buf.String())

	// Validate output content
	expected := []string{
		"Creating diff for ref: " + mockRefSpec,
		"Current Booted SHA:  " + mockCurrentSHA,
		"Fetching updates...",
		"Available Update SHA: " + mockNewSHA,
		"Analyzing package changes...",
		"app-misc/foo-1.0 -> app-misc/foo-1.1",
		"Deploying update...",
		"Upgrade successful.",
		"Rebooting...",
	}

	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("Missing expected output: %q", s)
		}
	}
}

func TestUpgradeNoUpdate(t *testing.T) {
	// Configure mock to return "new-sha" as current system state
	// This simulates that we are ALREADY on the latest commit
	// returned by rev-parse (mockNewSHA)
	os.Setenv("TEST_UPGRADE_CURRENT_SHA", mockNewSHA)
	defer os.Unsetenv("TEST_UPGRADE_CURRENT_SHA")

	env := setupUpgradeTest(t, mockNewSHA)
	defer env.cleanup()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := NewUpgradeCommand()
	if err := cmd.Init([]string{}); err != nil {
		w.Close()
		os.Stdout = oldStdout
		t.Fatalf("Init failed: %v", err)
	}

	if err := cmd.Run(); err != nil {
		w.Close()
		os.Stdout = oldStdout
		t.Fatalf("Run failed: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := stripAnsi(buf.String())

	// Validate "no update" message
	msg := "System is already up to date"
	if !strings.Contains(output, msg) {
		t.Errorf("Expected %q, got output:\n%s", msg, output)
	}
}

func stripAnsi(str string) string {
	const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))"
	var re = regexp.MustCompile(ansi)
	return re.ReplaceAllString(str, "")
}
