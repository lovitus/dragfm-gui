//go:build !windows

package agentservice

import (
	"os"
	"syscall"
)

func filesystemVersion(info os.FileInfo) (uint64, uint64) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0
	}
	return uint64(stat.Dev), uint64(stat.Ino)
}
