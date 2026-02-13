package commands

import (
	"flag"
	"fmt"
	"matrixos/vector/commands/cleaners"
	"matrixos/vector/lib/config"
	"os"
)

// JanitorCommand is a command for cleaning up development toolkit artifacts
type JanitorCommand struct {
	fs           *flag.FlagSet
	confFilePath string
}

// NewJanitorCommand creates a new JanitorCommand
func NewJanitorCommand() ICommand {
	c := &JanitorCommand{
		fs: flag.NewFlagSet("janitor", flag.ExitOnError),
	}
	c.fs.StringVar(&c.confFilePath, "conf", "", "Path to a custom config file")
	return c
}

// Name returns the name of the command
func (c *JanitorCommand) Name() string {
	return c.fs.Name()
}

// Init initializes the command
func (c *JanitorCommand) Init(args []string) error {
	c.fs.Usage = func() {
		fmt.Printf("Usage: vector %s\n", c.Name())
		c.fs.PrintDefaults()
	}
	return c.fs.Parse(args)
}

// Run runs the command
func (c *JanitorCommand) Run() error {
	// Check if we are running as root. If running as user, exit with error.
	if getEuid() != 0 {
		return fmt.Errorf("this command must be run as root")
	}

	// Load the matrixOS config.
	var cfg config.IConfig
	var err error
	if c.confFilePath != "" {
		cfg, err = config.NewIniConfigFromFile(c.confFilePath, ".")
	} else {
		cfg, err = config.NewIniConfig()
	}

	if err != nil {
		return fmt.Errorf("error reading config: %w", err)
	}
	if err := cfg.Load(); err != nil {
		return fmt.Errorf("error reading config: %w", err)
	}

	fmt.Println("Initializing images cleaner ...")
	icln := &cleaners.ImagesCleaner{}
	if err := icln.Init(cfg); err != nil {
		return fmt.Errorf("error initializing images cleaner: %w", err)
	}

	fmt.Println("Initializing downloads cleaner ...")
	dcln := &cleaners.DownloadsCleaner{}
	if err := dcln.Init(cfg); err != nil {
		return fmt.Errorf("error initializing downloads cleaner: %w", err)
	}

	fmt.Println("Initializing logs cleaner ...")
	lcln := &cleaners.LogsCleaner{}
	if err := lcln.Init(cfg); err != nil {
		return fmt.Errorf("error initializing logs cleaner: %w", err)
	}

	fmt.Println("Initializing all cleaners ...")
	clnrs := []cleaners.ICleaner{
		icln,
		dcln,
		lcln,
	}

	var errors []error
	for _, cln := range clnrs {
		fmt.Printf("Starting cleaner: %s\n", cln.Name())
		if err := cln.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing cleaner %s: %v\n", cln.Name(), err)
			errors = append(errors, err)
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during janitor execution", len(errors))
	}
	return nil
}
