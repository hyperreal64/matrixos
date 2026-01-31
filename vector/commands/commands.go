package commands

import (
	"fmt"
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
