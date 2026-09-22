//go:build !windows

package endpoint

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Each queued command owns a process group. Cancelling only its shell leaves
// children running and can leave stdout/stderr open until those children exit.
func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
