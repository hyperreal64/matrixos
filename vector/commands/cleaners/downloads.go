package cleaners

import (
	"matrixos/vector/lib/config"
	"time"
)

const (
	downloadsCutoffAge = 30 * 24 * time.Hour
)

type DownloadsCleaner struct {
	cfg config.IConfig
}

func (c *DownloadsCleaner) Name() string {
	return "downloads"
}

func (c *DownloadsCleaner) Init(cfg config.IConfig) error {
	c.cfg = cfg
	return nil
}

func (c *DownloadsCleaner) isDryRun() (bool, error) {
	val, err := c.cfg.GetItem("DownloadsCleaner.DryRun")
	if err != nil {
		return false, err
	}
	return val == "true", nil
}

func (c *DownloadsCleaner) getDownloadsDir() (string, error) {
	val, err := c.cfg.GetItem("Seeder.DownloadsDir")
	if err != nil {
		return "", err
	}
	return val, nil
}

func (c *DownloadsCleaner) Run() error {
	downloadsDir, err := c.getDownloadsDir()
	if err != nil {
		return err
	}
	dryRun, err := c.isDryRun()
	if err != nil {
		return err
	}

	return cleanDirectoryBasedOnMtime(downloadsDir, downloadsCutoffAge, dryRun)
}
