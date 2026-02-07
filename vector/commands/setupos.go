package commands

import (
	"flag"
	"fmt"
	"os"
)

// SetupOSCommand is a command for running the OS setup script
type SetupOSCommand struct {
	fs *flag.FlagSet
}

// NewSetupOSCommand creates a new SetupOSCommand
func NewSetupOSCommand() ICommand {
	return &SetupOSCommand{
		fs: flag.NewFlagSet("setupOS", flag.ExitOnError),
	}
}

// Name returns the name of the command
func (c *SetupOSCommand) Name() string {
	return c.fs.Name()
}

// Init initializes the command
func (c *SetupOSCommand) Init(args []string) error {
	c.fs.Usage = func() {
		fmt.Printf("Usage: vector %s\n", c.Name())
		c.fs.PrintDefaults()
	}
	return c.fs.Parse(args)
}

// Run runs the command
func (c *SetupOSCommand) Run() error {
	// Check if we are running as root. If running as user, exit with error.
	if getEuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}

	devDir := os.Getenv("MATRIXOS_DEV_DIR")
	if devDir == "" {
		devDir = "/matrixos"
	}

	cmd := execCommand(fmt.Sprintf("%s/install/setupOS", devDir))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
