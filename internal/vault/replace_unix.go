//go:build !windows

package vault

import (
	"errors"
	"os"
	"path/filepath"
)

func restrictVaultFile(path string) error { return os.Chmod(path, 0600) }

func replaceVaultFile(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
