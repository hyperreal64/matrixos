package commands

import (
	"flag"
	"fmt"
	"os"
)

// ReadWriteCommand is a command for unlocking the system
type ReadWriteCommand struct {
	fs *flag.FlagSet
}

// NewReadWriteCommand creates a new ReadWriteCommand
func NewReadWriteCommand() ICommand {
	return &ReadWriteCommand{
		fs: flag.NewFlagSet("readwrite", flag.ExitOnError),
	}
}

// Name returns the name of the command
func (c *ReadWriteCommand) Name() string {
	return c.fs.Name()
}

// Init initializes the command
func (c *ReadWriteCommand) Init(args []string) error {
	c.fs.Usage = func() {
		fmt.Printf("Usage: vector %s\n", c.Name())
		c.fs.PrintDefaults()
	}
	return c.fs.Parse(args)
}

// Run runs the command
func (c *ReadWriteCommand) Run() error {
	sysroot := os.Getenv("ROOT")
	if sysroot == "" {
		sysroot = "/"
	}

	// Check if we are running as root. If running as user, exit with error.
	if getEuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}

	cmd := execCommand("ostree", getSysrootFlag(sysroot), "admin", "unlock", "--hotfix")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
