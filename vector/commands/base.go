package commands

import (
	"fmt"
	"matrixos/vector/lib/cds"
	"matrixos/vector/lib/config"
)

type BaseCommand struct {
	cfg config.IConfig
	ot  cds.IOstree
}

// initBaseConfig initializes the base configuration for the command.
func (c *BaseCommand) initBaseConfig() error {
	cfg, err := config.NewBaseConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := cfg.Load(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	c.cfg = cfg
	return nil
}

// initClientConfig initializes the client configuration for the command.
func (c *BaseCommand) initClientConfig() error {
	cfg, err := config.NewClientConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := cfg.Load(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	c.cfg = cfg
	return nil
}

// initOstree initializes the ostree client for the command.
func (c *BaseCommand) initOstree() error {
	if c.cfg == nil {
		return fmt.Errorf("config not initialized")
	}
	ot, err := cds.NewOstree(c.cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize ostree: %w", err)
	}
	c.ot = ot
	return nil
}
