package cleaners

import (
	"errors"
	"fmt"
	"matrixos/dev/janitor/config"
	"os"
	"path/filepath"
	"time"
)

const (
	cutoffAge = 30 * 24 * time.Hour
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
	return val.Item == "true", nil
}

func (c *DownloadsCleaner) getDownloadsDir() (string, error) {
	val, err := c.cfg.GetItem("Seeder.DownloadsDir")
	if err != nil {
		return "", err
	}
	return val.Item, nil
}

func (c *DownloadsCleaner) Run() error {
	downloadsDir, err := c.getDownloadsDir()
	if err != nil {
		return err
	}

	fmt.Printf("Cleaning old downloads from %s ...\n", downloadsDir)

	// Here we are ok following symlinks, because the user could have just swapped
	// out a normal dir for a dir symlink.
	stat, err := os.Stat(downloadsDir)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "Downloads directory %s does not exist. Nothing to do.\n", downloadsDir)
		return nil
	}
	if !stat.IsDir() {
		fmt.Fprintf(os.Stderr, "Downloads directory %s is not a directory.\n", downloadsDir)
		return os.ErrNotExist
	}

	entries, err := os.ReadDir(downloadsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read downloads directory %s: %v\n", downloadsDir, err)
		return err
	}

	var candidates []string
	for _, entry := range entries {
		path := filepath.Join(downloadsDir, entry.Name())
		lstat, err := os.Lstat(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to stat download %s: %v\n", path, err)
			continue
		}

		mode := lstat.Mode()
		isFile := mode.IsRegular()
		if !isFile {
			fmt.Fprintf(os.Stderr, "Path %s is not a regular file. Ignoring this file.\n", path)
			continue
		}

		mtime := lstat.ModTime()
		if time.Since(mtime) < cutoffAge {
			fmt.Fprintf(
				os.Stdout,
				"%s is newer than %v days. Skipping.\n",
				path,
				cutoffAge.Hours()/24,
			)
			continue
		}

		fmt.Fprintf(os.Stdout, "Found candidate download file: %s\n", path)
		candidates = append(candidates, path)
	}

	if len(candidates) == 0 {
		fmt.Println("No downloads to remove.")
		return nil
	}

	for _, path := range candidates {
		fmt.Printf("Selected: %s\n", path)
	}

	dryRun, err := c.isDryRun()
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Println("Dry run mode enabled. Not cleaning downloads.")
		return nil
	}

	return deletePaths(candidates)
}
