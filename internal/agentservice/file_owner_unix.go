//go:build !windows

package agentservice

import (
	"os"
	"syscall"
)

func sameOwner(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return false
	}
	first, ok := a.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	second, ok := b.Sys().(*syscall.Stat_t)
	return ok && first.Uid == second.Uid
}
