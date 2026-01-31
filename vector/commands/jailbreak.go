package commands

import (
	"flag"
	"fmt"
	"os"
)

// JailbreakCommand is a command for permanently converting the system to mutable Gentoo
type JailbreakCommand struct {
	fs *flag.FlagSet
}

// NewJailbreakCommand creates a new JailbreakCommand
func NewJailbreakCommand() *JailbreakCommand {
	return &JailbreakCommand{
		fs: flag.NewFlagSet("jailbreak", flag.ExitOnError),
	}
}

// Name returns the name of the command
func (c *JailbreakCommand) Name() string {
	return c.fs.Name()
}

// Init initializes the command
func (c *JailbreakCommand) Init(args []string) error {
	c.fs.Usage = func() {
		fmt.Printf("Usage: vector %s\n", c.Name())
		c.fs.PrintDefaults()
	}
	return c.fs.Parse(args)
}

// Run runs the command
func (c *JailbreakCommand) Run() error {
	// Check if we are running as root. If running as user, exit with error.
	if getEuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}

	devDir := os.Getenv("MATRIXOS_DEV_DIR")
	if devDir == "" {
		devDir = "/matrixos"
	}

	cmd := execCommand(fmt.Sprintf("%s/install/jailbreak", devDir))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
