//go:build !windows

package endpoint

import (
	"os"
	"syscall"
)

func localFileVersion(target string) (uint64, uint64, error) {
	info, err := os.Lstat(target)
	if err != nil {
		return 0, 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, syscall.EINVAL
	}
	return uint64(stat.Dev), uint64(stat.Ino), nil
}
