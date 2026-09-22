//go:build linux

package agentservice

import (
	"os/exec"
	"syscall"
)

func configureChildLifecycle(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
