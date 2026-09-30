//go:build linux

package agentservice

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureChildLifecycle(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM, Setpgid: true}
	command.Cancel = func() error { return signalChildGroup(command, os.Kill) }
}

func signalChildGroup(command *exec.Cmd, signal os.Signal) error {
	err := syscall.Kill(-command.Process.Pid, signal.(syscall.Signal))
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
