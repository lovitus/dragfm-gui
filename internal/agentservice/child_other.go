//go:build !linux

package agentservice

import "os/exec"

func configureChildLifecycle(*exec.Cmd) {}
