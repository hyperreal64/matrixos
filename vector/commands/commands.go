package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

// ICommand defines the interface for a vector command
type ICommand interface {
	Name() string
	Init(args []string) error
	Run() error
}

// UI provides common UI styles and icons for commands
type UI struct {
	// UI Styles
	cReset, cRed, cGreen, cYellow, cBlue string
	cMagenta, cCyan, cWhite, cBold       string

	// UI Icons
	iconSearch, iconDownload, iconCheck         string
	iconUpdate, iconPackage                     string
	iconQuestion, iconRocket, iconGear, iconDoc string
	iconNew, iconError, iconWarn                string
	separator                                   string
}

// StartUI initializes the UI component with environment detection
func (ui *UI) StartUI() {
	useColor := false
	useEmoji := false

	// Check if stdout is a terminal
	_, err := unix.IoctlGetTermios(int(os.Stdout.Fd()), unix.TCGETS)
	isTerm := err == nil

	if isTerm {
		termEnv := os.Getenv("TERM")
		if termEnv != "dumb" {
			useColor = true
		}
		// Linux console has limited font support
		if termEnv != "linux" {
			useEmoji = true
		}
	}

	if useColor {
		ui.cReset = "\033[0m"
		ui.cRed = "\033[31m"
		ui.cGreen = "\033[32m"
		ui.cYellow = "\033[33m"
		ui.cBlue = "\033[34m"
		ui.cMagenta = "\033[35m"
		ui.cCyan = "\033[36m"
		ui.cWhite = "\033[37m"
		ui.cBold = "\033[1m"
	}

	if useEmoji {
		ui.iconSearch = "○ "
		ui.iconDownload = "⇩ "
		ui.iconCheck = "✔ "
		ui.iconUpdate = "↻ "
		ui.iconPackage = "▤ "
		ui.iconQuestion = "? "
		ui.iconRocket = "➤ "
		ui.iconGear = "⚙ "
		ui.iconDoc = "≡ "
		ui.iconNew = "★ "
		ui.iconError = "✖ "
		ui.iconWarn = "⚠ "
		ui.separator = "   ───────────────────────────────────────────────────"
	} else {
		ui.iconSearch = "[?] "
		ui.iconDownload = "[v] "
		ui.iconCheck = "[OK] "
		ui.iconUpdate = "[~] "
		ui.iconPackage = "[#] "
		ui.iconQuestion = "[?] "
		ui.iconRocket = "[>] "
		ui.iconGear = "[*] "
		ui.iconDoc = "[f] "
		ui.iconNew = "[+] "
		ui.iconError = "[X] "
		ui.iconWarn = "[!] "
		ui.separator = "   ---------------------------------------------------"
	}
}

var execCommand = exec.Command
var getEuid = os.Geteuid

func getSysrootFlag(sysroot string) string {
	return fmt.Sprintf("--sysroot=%s", sysroot)
}

func getRepoFlag(sysroot string) string {
	return fmt.Sprintf("--repo=%s/ostree/repo", sysroot)
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst + ".tmp")
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	err = destFile.Sync()
	if err != nil {
		return err
	}
	sourceFile.Close()
	destFile.Close()

	return os.Rename(dst+".tmp", dst)
}
