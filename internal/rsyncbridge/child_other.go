//go:build !linux

package rsyncbridge

import "os/exec"

func configureChildLifecycle(*exec.Cmd) {}
