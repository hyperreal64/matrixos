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

type commandHandler func(args []string) bool

var mockCommandHandlers = []commandHandler{
	handleOstreeStatus,
	handleOstreeRevParse,
	handleOstreeUpgradePull,
	handleOstreeUpgradeDeploy,
	handleOstreeListPackages,
	handleReboot,
	handleSbverify,
}

func mockExecUpgradeCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestUpgradeHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)

	env := []string{"GO_WANT_UPGRADE_HELPER_PROCESS=1"}
	vars := []string{
		"TEST_UPGRADE_CURRENT_SHA",
		"TEST_UPGRADE_NEW_SHA",
		"TEST_UPGRADE_SHOW_NEW_DEPLOYMENT",
		"DEBUG_TEST",
	}
	for _, v := range vars {
		if val := os.Getenv(v); val != "" {
			env = append(env, v+"="+val)
		}
	}
	cmd.Env = env
	return cmd
}

func TestUpgradeHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_UPGRADE_HELPER_PROCESS") != "1" {
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

	for _, handler := range mockCommandHandlers {
		if handler(args) {
			return
		}
	}

	fmt.Fprintf(os.Stderr, "Unknown command in test: %v\n", args)
	os.Exit(1)
}

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

func handleOstreeStatus(args []string) bool {
	p := parseArgs(args)
	if p.cmd != "ostree" || !p.hasSubCmd("admin") || !p.hasSubCmd("status") {
		return false
	}

	currentSHA := mockCurrentSHA
	if val := os.Getenv("TEST_UPGRADE_CURRENT_SHA"); val != "" {
		currentSHA = val
	}

	newSHA := mockNewSHA
	if val := os.Getenv("TEST_UPGRADE_NEW_SHA"); val != "" {
		newSHA = val
	}

	deployments := fmt.Sprintf(`{
			"booted": true,
			"checksum": "%s",
			"stateroot": "%s",
			"refspec": "%s"
		}`, currentSHA, stateroot, mockRefSpec)

	if os.Getenv("TEST_UPGRADE_SHOW_NEW_DEPLOYMENT") == "1" {
		deployments += fmt.Sprintf(`,{
			"booted": false,
			"checksum": "%s",
			"stateroot": "%s",
			"refspec": "%s"
		}`, newSHA, stateroot, mockRefSpec)
	}

	fmt.Printf(`{ "deployments": [ %s ] }`, deployments)

	if os.Getenv("DEBUG_TEST") == "1" {
		fmt.Fprintf(os.Stderr, "DEBUG: deployments json: { \"deployments\": [ %s ] }\n", deployments)
	}
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
	if p.cmd != "ostree" || !p.hasSubCmd("admin") || !p.hasSubCmd("upgrade") {
		return false
	}
	return p.hasFlag("pull-only")
}

func handleOstreeUpgradeDeploy(args []string) bool {
	p := parseArgs(args)
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

	for _, arg := range p.raw {
		if strings.Contains(arg, "/usr/var-db-pkg") {
			os.Exit(1)
			return true
		}
	}

	var commit string
	for _, arg := range p.raw {
		if arg == mockCurrentSHA || arg == mockNewSHA {
			commit = arg
			break
		}
	}

	if commit == "" {
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

func handleSbverify(args []string) bool {
	if len(args) > 0 && args[0] == "sbverify" {
		return true
	}
	return false
}

type testEnv struct {
	tmpDir     string
	originFile string
	cleanup    func()
}

func setupUpgradeTest(t *testing.T, currentSHA string) *testEnv {
	origExec := execCommand
	execCommand = mockExecUpgradeCommand

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

	origEuid := getEuid
	getEuid = func() int { return 0 }

	tmpDir, err := os.MkdirTemp("", "upgrade-test")
	if err != nil {
		t.Fatal(err)
	}

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

	if err := os.WriteFile(filepath.Join(tmpDir, ".matrixos"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)

	originDir := filepath.Join(tmpDir, "ostree/deploy", stateroot, "deploy")
	if err := os.MkdirAll(originDir, 0755); err != nil {
		t.Fatal(err)
	}

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

func createMockDeployment(t *testing.T, env *testEnv, commit string) string {
	depDir := filepath.Join(env.tmpDir, "ostree/deploy", stateroot, "deploy", commit+".0")
	if err := os.MkdirAll(depDir, 0755); err != nil {
		t.Fatal(err)
	}
	return depDir
}

func TestUpgradeRun(t *testing.T) {
	env := setupUpgradeTest(t, mockCurrentSHA)
	defer env.cleanup()

	output, err := runCaptureStdout(func() error {
		cmd := NewUpgradeCommand()
		if err := cmd.Init([]string{"-y"}); err != nil {
			return fmt.Errorf("Init failed: %v", err)
		}
		return cmd.Run()
	})

	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	expected := []string{
		"Checking for updates on branch: " + mockRefSpec,
		"Current version: " + mockCurrentSHA,
		"Fetching updates...",
		"Update Available: " + mockNewSHA,
		"Analyzing package changes...",
		"app-misc/foo-1.0 -> app-misc/foo-1.1",
		"Deploying update...",
		"Upgrade successful!",
		"Please reboot at your earliest convenience.",
	}

	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("Missing expected output: %q", s)
		}
	}
}

func TestUpgradeNoUpdate(t *testing.T) {
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

	msg := "System is already up to date"
	if !strings.Contains(output, msg) {
		t.Errorf("Expected %q, got output:\n%s", msg, output)
	}
}

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

func TestUpgradePretend(t *testing.T) {
	env := setupUpgradeTest(t, mockCurrentSHA)
	defer env.cleanup()

	output, err := runCaptureStdout(func() error {
		cmd := NewUpgradeCommand()
		if err := cmd.Init([]string{"--pretend"}); err != nil {
			return fmt.Errorf("Init failed: %v", err)
		}
		return cmd.Run()
	})

	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	expected := []string{
		"Fetching updates...",
		"Analyzing package changes...",
		"Running in pretend mode. Exiting.",
	}

	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("Missing expected output: %q\nGot:\n%s", s, output)
		}
	}

	forbidden := "Deploying update..."
	if strings.Contains(output, forbidden) {
		t.Errorf("Unexpected output: %q (should handle pretend mode)", forbidden)
	}
}

func TestUpgradeForce(t *testing.T) {
	os.Setenv("TEST_UPGRADE_CURRENT_SHA", mockNewSHA)
	defer os.Unsetenv("TEST_UPGRADE_CURRENT_SHA")

	env := setupUpgradeTest(t, mockNewSHA)
	defer env.cleanup()

	output, err := runCaptureStdout(func() error {
		cmd := NewUpgradeCommand()
		if err := cmd.Init([]string{"--force", "-y"}); err != nil {
			return fmt.Errorf("Init failed: %v", err)
		}
		return cmd.Run()
	})

	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	expected := []string{
		"System is already up to date.",
		"Forcing update despite no changes...",
		"Deploying update...",
		"Upgrade successful!",
	}

	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("Missing expected output: %q\nGot:\n%s", s, output)
		}
	}
}

func TestUpgradeAbort(t *testing.T) {
	env := setupUpgradeTest(t, mockCurrentSHA)
	defer env.cleanup()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdin := os.Stdin
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		r.Close()
	}()

	go func() {
		w.Write([]byte("n\n"))
		w.Close()
	}()

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

	expected := "Aborted."
	if !strings.Contains(output, expected) {
		t.Errorf("Expected output to contain %q, got:\n%s", expected, output)
	}

	forbidden := "Deploying update..."
	if strings.Contains(output, forbidden) {
		t.Errorf("Unexpected output: %q (should have aborted)", forbidden)
	}
}

func TestUpgradeBootloaderSuccess(t *testing.T) {
	env := setupUpgradeTest(t, mockCurrentSHA)
	defer env.cleanup()

	newRoot := createMockDeployment(t, env, mockNewSHA)

	grubSrc := filepath.Join(newRoot, "usr/lib/grub/grub-x86_64.efi.signed")
	if err := os.MkdirAll(filepath.Dir(grubSrc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grubSrc, []byte("new grub"), 0644); err != nil {
		t.Fatal(err)
	}

	shimDir := filepath.Join(newRoot, "usr/share/shim")
	if err := os.MkdirAll(shimDir, 0755); err != nil {
		t.Fatal(err)
	}
	shimSrc := filepath.Join(shimDir, "shimx64.efi")
	if err := os.WriteFile(shimSrc, []byte("new shim"), 0644); err != nil {
		t.Fatal(err)
	}

	efiRoot := filepath.Join(env.tmpDir, "efi")
	grubDir := filepath.Join(efiRoot, "EFI/BOOT")
	if err := os.MkdirAll(grubDir, 0755); err != nil {
		t.Fatal(err)
	}

	existingGrub := filepath.Join(grubDir, "GRUBX64.EFI")
	if err := os.WriteFile(existingGrub, []byte("old grub"), 0644); err != nil {
		t.Fatal(err)
	}

	certFile := filepath.Join(efiRoot, "secureboot.crt")
	if err := os.WriteFile(certFile, []byte("dummy cert"), 0644); err != nil {
		t.Fatal(err)
	}

	configContent := fmt.Sprintf(`
[matrixOS]
Root=%s

[Ostree]
Root=%s

[Imager]
EfiRoot=%s
EfiCertificateFileName=secureboot.crt
`, env.tmpDir, env.tmpDir, efiRoot)

	confPath := filepath.Join(env.tmpDir, "conf/matrixos.conf")
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("TEST_UPGRADE_SHOW_NEW_DEPLOYMENT", "1")
	defer os.Unsetenv("TEST_UPGRADE_SHOW_NEW_DEPLOYMENT")
	os.Setenv("DEBUG_TEST", "1")
	defer os.Unsetenv("DEBUG_TEST")

	output, err := runCaptureStdout(func() error {
		cmd := NewUpgradeCommand()
		if err := cmd.Init([]string{"-y", "--update-bootloader"}); err != nil {
			return fmt.Errorf("Init failed: %v", err)
		}
		return cmd.Run()
	})

	if err != nil {
		t.Fatalf("Command failed: %v", err)
	}

	expected := []string{
		"Updating bootloader binaries...",
		"Found EFI file: " + existingGrub,
		"Verified EFI file: " + existingGrub,
		"Updating GRUB/Shim in " + grubDir,
		"Copying grub-x86_64.efi.signed to " + existingGrub,
		"Copying shimx64.efi to " + filepath.Join(grubDir, "shimx64.efi"),
		"Upgrade successful!",
	}

	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("Missing expected output: %q\nGot:\n%s", s, output)
		}
	}

	content, err := os.ReadFile(existingGrub)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new grub" {
		t.Errorf("Expected 'new grub', got %q", content)
	}
}

func TestUpgradeBootloaderMissingConfig(t *testing.T) {
	env := setupUpgradeTest(t, mockCurrentSHA)
	defer env.cleanup()

	output, err := runCaptureStdout(func() error {
		cmd := NewUpgradeCommand()
		if err := cmd.Init([]string{"-y", "--update-bootloader"}); err != nil {
			return fmt.Errorf("Init failed: %v", err)
		}
		return cmd.Run()
	})

	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("Unexpected error message: %v", err)
	}

	_ = output
}

func TestUpgradeBootloaderMissingCert(t *testing.T) {
	env := setupUpgradeTest(t, mockCurrentSHA)
	defer env.cleanup()

	efiRoot := filepath.Join(env.tmpDir, "efi")
	if err := os.MkdirAll(efiRoot, 0755); err != nil {
		t.Fatal(err)
	}

	configContent := fmt.Sprintf(`
[matrixOS]
Root=%s

[Ostree]
Root=%s

[Imager]
EfiRoot=%s
`, env.tmpDir, env.tmpDir, efiRoot)
	confPath := filepath.Join(env.tmpDir, "conf/matrixos.conf")
	if err := os.WriteFile(confPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := runCaptureStdout(func() error {
		cmd := NewUpgradeCommand()
		if err := cmd.Init([]string{"-y", "--update-bootloader"}); err != nil {
			return fmt.Errorf("Init failed: %v", err)
		}
		return cmd.Run()
	})

	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Errorf("Unexpected error message: %v", err)
	}
}
