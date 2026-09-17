//go:build linux

package rsyncbridge

import (
	"os/exec"
	"syscall"
)

func configureChildLifecycle(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
