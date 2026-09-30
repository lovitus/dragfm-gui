package filecommit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SyncPaths confirms final file data/metadata and directory entries after all
// metadata changes and publication. Callers include each changed directory and
// the publication parent, not symlink referents. This is not a power-loss test.
func SyncPaths(ctx context.Context, paths []string) error {
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !filepath.IsAbs(path) {
			return fmt.Errorf("sync requires an absolute path: %q", path)
		}
		before, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !before.Mode().IsRegular() && !before.IsDir() {
			return fmt.Errorf("refusing to synchronize a symlink or special file: %q", path)
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		opened, statErr := file.Stat()
		if statErr != nil || !os.SameFile(before, opened) {
			return errors.Join(fmt.Errorf("sync path identity changed: %q", path), statErr, file.Close())
		}
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(syncErr, closeErr); err != nil {
			return fmt.Errorf("sync %q: %w", path, err)
		}
	}
	return nil
}
