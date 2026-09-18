package endpoint

import (
	"fmt"
	"github.com/pkg/sftp"
)

// The standard SFTP rename refuses an existing target. Never implement atomic
// replacement by deleting that target first, including after extension errors.
func renameSFTP(client *sftp.Client, source, target string, overwrite bool) error {
	if overwrite {
		if _, supported := client.HasExtension("posix-rename@openssh.com"); supported {
			return client.PosixRename(source, target)
		}
	}
	if err := client.Rename(source, target); err != nil {
		return fmt.Errorf("safe SFTP rename failed (old destination retained): %w", err)
	}
	return nil
}
