//go:build linux

package rsyncbridge

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureChildLifecycle(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM, Setpgid: true}
	command.Cancel = func() error { return killChild(command) }
}

func killChild(command *exec.Cmd) error {
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
