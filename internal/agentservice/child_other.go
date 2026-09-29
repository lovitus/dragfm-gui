//go:build !linux

package agentservice

import (
	"os"
	"os/exec"
)

func configureChildLifecycle(*exec.Cmd) {}

func signalChildGroup(command *exec.Cmd, signal os.Signal) error {
	return command.Process.Signal(signal)
}
