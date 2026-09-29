//go:build !windows

package agentservice

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

var errDirectoryLeased = errors.New("temporary directory is still leased")

func effectiveOwner(info os.FileInfo) bool {
	owner, ok := info.Sys().(*syscall.Stat_t)
	return ok && owner.Uid == uint32(os.Geteuid())
}

func lockStaleDirectory(root *os.Root) (*os.File, error) {
	file, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errDirectoryLeased
		}
		return nil, err
	}
	return file, nil
}
