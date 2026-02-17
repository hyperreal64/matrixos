package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// ICommand defines the interface for a vector command
type ICommand interface {
	Name() string
	Init(args []string) error
	Run() error
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
