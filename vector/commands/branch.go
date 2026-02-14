package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

const defaultMatrixOSOstreeRemote = "origin"

// BranchCommand is a command for managing branches
type BranchCommand struct {
	fs   *flag.FlagSet
	sub  string
	args []string
}

// NewBranchCommand creates a new BranchCommand
func NewBranchCommand() ICommand {
	return &BranchCommand{
		fs: flag.NewFlagSet("branch", flag.ExitOnError),
	}
}

// Name returns the name of the command
func (c *BranchCommand) Name() string {
	return c.fs.Name()
}

// Init initializes the command
func (c *BranchCommand) Init(args []string) error {
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
		cmd := execCommand("ostree", "admin", "status", "--json")
		out, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to execute ostree command: %w", err)
		}

		type Deployment struct {
			Booted   bool     `json:"booted"`
			Checksum string   `json:"checksum"`
			Origin   string   `json:"origin"`
			RefSpec  []string `json:"refspec"`
		}
		var status struct {
			Deployments []Deployment `json:"deployments"`
		}

		if err := json.Unmarshal(out, &status); err != nil {
			return fmt.Errorf("failed to parse ostree status %s: %w", out, err)
		}

		for _, dep := range status.Deployments {
			if dep.Booted {
				fmt.Println("Current branch:")
				fmt.Printf("  RefSpec: %s\n", dep.RefSpec)
				fmt.Printf("  Checksum: %s\n", dep.Checksum)
				fmt.Printf("  Origin: %s\n", dep.Origin)
				return nil
			}
		}

		return fmt.Errorf("could not find booted deployment")

	case "list":
		cmd := execCommand("ostree", "remote", "refs", defaultMatrixOSOstreeRemote)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()

	case "switch":
		if len(c.args) < 1 {
			return fmt.Errorf("switch command requires a branch name")
		}
		branchName := c.args[0]
		refspec := defaultMatrixOSOstreeRemote + ":" + branchName
		fmt.Printf("Switching to %s...\n", refspec)
		cmd := execCommand("ostree", "admin", "switch", refspec)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()

	default:
		return fmt.Errorf("unknown subcommand: %s", c.sub)
	}
}
