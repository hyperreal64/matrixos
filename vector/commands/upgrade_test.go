package commands

import (
	"bytes"
	"fmt"
	"io"
	"matrixos/vector/lib/cds"
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
			"stateroot": "%s",
			"refspec": "%s"
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

// Mock package data
var mockPackages = map[string][]string{
	mockCurrentSHA: {
		"d00755 0 0 0 /var/db/pkg/app-misc/foo-1.0/",
		"-00644 0 0 0 /var/db/pkg/app-misc/foo-1.0/CONTENTS",
	},
	mockNewSHA: {
		"d00755 0 0 0 /var/db/pkg/app-misc/foo-1.1/",
		"-00644 0 0 0 /var/db/pkg/app-misc/foo-1.1/CONTENTS",
	},
}

// Helper to parse arguments into a more queryable structure
// This allows tests to be resilient to flag reordering
type parsedArgs struct {
	cmd     string
	subCmds []string
	flags   map[string]string
	posArgs []string
	raw     []string
}

func parseArgs(args []string) *parsedArgs {
	p := &parsedArgs{
		flags: make(map[string]string),
		raw:   args,
	}

	if len(args) > 0 {
		p.cmd = args[0]
	}

	// Simple parser: strings starting with - are flags
	// everything else (until --) is a subcommand or positional arg.
	// After --, everything is a positional arg.
	inDashDash := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			inDashDash = true
			continue
		}

		if inDashDash {
			p.posArgs = append(p.posArgs, arg)
			continue
		}

		if strings.HasPrefix(arg, "-") {
			// Handle both --long and -s flags
			trimmed := strings.TrimLeft(arg, "-")
			parts := strings.SplitN(trimmed, "=", 2)
			key := parts[0]
			val := "true"
			if len(parts) > 1 {
				val = parts[1]
			}
			p.flags[key] = val
		} else {
			p.subCmds = append(p.subCmds, arg)
		}
	}
	return p
}

func (p *parsedArgs) hasSubCmd(name string) bool {
	for _, s := range p.subCmds {
		if s == name {
			return true
		}
	}
	return false
}

func (p *parsedArgs) hasFlag(name string) bool {
	_, ok := p.flags[name]
	return ok
}

// --- Command Handlers ---

func handleOstreeStatus(args []string) bool {
	p := parseArgs(args)
	if p.cmd != "ostree" || !p.hasSubCmd("admin") || !p.hasSubCmd("status") {
		return false
	}

	sha := mockCurrentSHA
	if val := os.Getenv("TEST_UPGRADE_CURRENT_SHA"); val != "" {
		sha = val
	}
	// We always return the mockRefSpec as the origin refspec
	fmt.Printf(ostreeStatusTmpl, sha, stateroot, mockRefSpec)
	return true
}

func handleOstreeRevParse(args []string) bool {
	p := parseArgs(args)
	if p.cmd != "ostree" || !p.hasSubCmd("rev-parse") {
		return false
	}
	fmt.Print(mockNewSHA)
	return true
}

func handleOstreeUpgradePull(args []string) bool {
	p := parseArgs(args)
	// match: ostree admin upgrade ... --pull-only
	if p.cmd != "ostree" || !p.hasSubCmd("admin") || !p.hasSubCmd("upgrade") {
		return false
	}
	return p.hasFlag("pull-only")
}

func handleOstreeUpgradeDeploy(args []string) bool {
	p := parseArgs(args)
	// match: ostree admin upgrade ... --deploy-only
	if p.cmd != "ostree" || !p.hasSubCmd("admin") || !p.hasSubCmd("upgrade") {
		return false
	}
	return p.hasFlag("deploy-only")
}

func handleOstreeListPackages(args []string) bool {
	p := parseArgs(args)
	if p.cmd != "ostree" || !p.hasSubCmd("ls") || !p.hasFlag("R") {
		return false
	}

	// Simulate failure for /usr/var-db-pkg to force fallback
	for _, arg := range p.raw {
		if strings.Contains(arg, "/usr/var-db-pkg") {
			os.Exit(1)
			return true
		}
	}

	// Commit finding logic:
	var commit string
	for _, arg := range p.raw {
		if arg == mockCurrentSHA || arg == mockNewSHA {
			commit = arg
			break
		}
	}

	if commit == "" {
		// fallback to looking relative to known flags
		return false
	}

	switch commit {
	case mockCurrentSHA:
		for _, line := range mockPackages[mockCurrentSHA] {
			fmt.Println(line)
		}
	case mockNewSHA:
		for _, line := range mockPackages[mockNewSHA] {
			fmt.Println(line)
		}
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

	// Mock cds.RunWithStdoutCapture
	origRunWithCapture := cds.RunWithStdoutCapture
	cds.RunWithStdoutCapture = func(verbose bool, args ...string) (io.Reader, error) {
		cmd := mockExecUpgradeCommand("ostree", args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("mock run failed: %v, stderr: %s",
				err, stderr.String())
		}
		return &stdout, nil
	}

	// Mock cds.Run
	origRun := cds.Run
	cds.Run = func(verbose bool, args ...string) error {
		cmd := mockExecUpgradeCommand("ostree", args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("mock run failed: %v, stderr: %s",
				err, stderr.String())
		}
		return nil
	}

	// Mock root user
	origEuid := getEuid
	getEuid = func() int { return 0 }

	// Create temp fs
	tmpDir, err := os.MkdirTemp("", "upgrade-test")
	if err != nil {
		t.Fatal(err)
	}

	// Setup config in temp dir
	confDir := filepath.Join(tmpDir, "conf")
	if err := os.Mkdir(confDir, 0755); err != nil {
		t.Fatal(err)
	}

	configContent := fmt.Sprintf(`
[matrixOS]
Root=%s

[Ostree]
Root=%s
`, tmpDir, tmpDir)

	if err := os.WriteFile(filepath.Join(confDir, "matrixos.conf"), []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	// MatrixOS marker
	if err := os.WriteFile(filepath.Join(tmpDir, ".matrixos"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)

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
			os.Chdir(origWd)
			os.RemoveAll(tmpDir)
			execCommand = origExec
			getEuid = origEuid
			cds.RunWithStdoutCapture = origRunWithCapture
			cds.Run = origRun
		},
	}
}

// --- Tests ---

func TestUpgradeRun(t *testing.T) {
	env := setupUpgradeTest(t, mockCurrentSHA)
	defer env.cleanup()

	output, err := runCaptureStdout(func() error {
		cmd := NewUpgradeCommand()
		if err := cmd.Init([]string{"-y", "--reboot"}); err != nil {
			return fmt.Errorf("Init failed: %v", err)
		}
		return cmd.Run()
	})

	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

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
	os.Setenv("TEST_UPGRADE_CURRENT_SHA", mockNewSHA)
	defer os.Unsetenv("TEST_UPGRADE_CURRENT_SHA")

	env := setupUpgradeTest(t, mockNewSHA)
	defer env.cleanup()

	output, err := runCaptureStdout(func() error {
		cmd := NewUpgradeCommand()
		if err := cmd.Init([]string{}); err != nil {
			return fmt.Errorf("Init failed: %v", err)
		}
		return cmd.Run()
	})

	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	// Validate "no update" message
	msg := "System is already up to date"
	if !strings.Contains(output, msg) {
		t.Errorf("Expected %q, got output:\n%s", msg, output)
	}
}

// runCaptureStdout runs the given function and captures its stdout output.
func runCaptureStdout(f func() error) (string, error) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := f()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return stripAnsi(buf.String()), err
}

func stripAnsi(str string) string {
	const ansi = "[\u001B\u009B][[\\]()#;?]*(?:(?:(?:[a-zA-Z\\d]*(?:;[a-zA-Z\\d]*)*)?\u0007)|(?:(?:\\d{1,4}(?:;\\d{0,4})*)?[\\dA-PRZcf-ntqry=><~]))"
	var re = regexp.MustCompile(ansi)
	return re.ReplaceAllString(str, "")
}
