//go:build linux

package filecommit

import "golang.org/x/sys/unix"

// NoReplace performs the existence check and rename in one kernel operation.
// Unsupported filesystems fail closed, never silently replacing a new target.
func NoReplace(source, target string) error {
	return unix.Renameat2(unix.AT_FDCWD, source, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
}
