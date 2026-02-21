package commands

import (
	"flag"
	"fmt"
)

// BranchCommand is a command for managing branches
type BranchCommand struct {
	BaseCommand
	fs   *flag.FlagSet
	sub  string
	args []string
}

// NewBranchCommand creates a new BranchCommand
func NewBranchCommand() ICommand {
	return &BranchCommand{}
}

// Name returns the name of the command
func (c *BranchCommand) Name() string {
	return "branch"
}

// Init initializes the command
func (c *BranchCommand) Init(args []string) error {
	if err := c.initClientConfig(); err != nil {
		return err
	}

	if err := c.initOstree(); err != nil {
		return err
	}

	return c.parseArgs(args)
}

// parseArgs parses the command-line arguments without initializing config or ostree.
func (c *BranchCommand) parseArgs(args []string) error {
	c.fs = flag.NewFlagSet("branch", flag.ContinueOnError)
	c.fs.Usage = func() {
		fmt.Printf("Usage: vector %s <subcommand>\n", c.Name())
		fmt.Println("Subcommands: show, list, switch")
	}
	err := c.fs.Parse(args)
	if err != nil {
		return err
	}
	if c.fs.NArg() < 1 {
		c.fs.Usage()
		return fmt.Errorf("no subcommand provided")
	}
	c.sub = c.fs.Arg(0)
	c.args = c.fs.Args()[1:]
	return nil
}

// Run runs the command
func (c *BranchCommand) Run() error {
	switch c.sub {
	case "show":
		deployments, err := c.ot.ListDeployments(false)
		if err != nil {
			return fmt.Errorf("failed to list deployments: %w", err)
		}

		for _, dep := range deployments {
			if dep.Booted {
				fmt.Println("Current branch:")
				fmt.Printf("  Name: %s\n", dep.Stateroot)
				fmt.Printf("  Branch/Ref: %s\n", dep.Refspec)
				fmt.Printf("  Checksum: %s\n", dep.Checksum)
				fmt.Printf("  Index: %d\n", dep.Index)
				fmt.Printf("  Serial: %d\n", dep.Serial)
				return nil
			}
		}

		return fmt.Errorf("could not find booted deployment")

	case "list":
		refs, err := c.ot.RemoteRefs(false)
		if err != nil {
			return fmt.Errorf("failed to list remote refs: %w", err)
		}
		for _, ref := range refs {
			fmt.Println(ref)
		}
		return nil

	case "switch":
		if len(c.args) < 1 {
			return fmt.Errorf("switch command requires a branch/ref name")
		}
		ref := c.args[0]
		return c.ot.Switch(ref, true)

	default:
		return fmt.Errorf("unknown subcommand: %s", c.sub)
	}
}
