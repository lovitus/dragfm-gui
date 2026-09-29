//go:build !linux

package rsyncbridge

import "os/exec"

func configureChildLifecycle(*exec.Cmd) {}

func killChild(command *exec.Cmd) error { return command.Process.Kill() }
