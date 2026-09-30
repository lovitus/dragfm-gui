//go:build !windows

package agentlease

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func lock(file *os.File, exclusive bool) error {
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}
	if err := unix.Flock(int(file.Fd()), mode|unix.LOCK_NB); err != nil {
		return fmt.Errorf("helper installation lease (child may still be running): %w", err)
	}
	return nil
}
