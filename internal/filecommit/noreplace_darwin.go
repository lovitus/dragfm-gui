//go:build darwin

package filecommit

import "golang.org/x/sys/unix"

func NoReplace(source, target string) error {
	return unix.RenamexNp(source, target, unix.RENAME_EXCL)
}
