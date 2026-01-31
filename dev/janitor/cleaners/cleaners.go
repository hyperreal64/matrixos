package cleaners

import (
	"fmt"
	"matrixos/dev/janitor/config"
	"os"
)

// ICleaner defines the interface for a janitor cleaner
type ICleaner interface {
	Name() string
	Init(cfg config.IConfig) error
	Run() error
}

func deletePaths(paths []string) error {
	for _, path := range paths {
		fmt.Printf("Deleting: %s\n", path)
		err := os.Remove(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to delete %s: %v.\n", path, err)
			return err
		}
	}
	return nil
}
