//go:build windows

package endpoint

import (
 "context"
 "os/exec"
 "strconv"
 "syscall"
 "time"
)

func configureCommandCancellation(cmd *exec.Cmd) {
 cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
 cmd.WaitDelay = 2 * time.Second
 cmd.Cancel = func() error {
  // taskkill receives only the PID of this owned command, never its command
  // text or credentials. A bounded fallback still kills the parent process.
  ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
  defer cancel()
  kill := exec.CommandContext(ctx, "taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
  kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
  if err := kill.Run(); err != nil { return cmd.Process.Kill() }
  return nil
 }
}
