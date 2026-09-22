package endpoint

import (
 "context"
 "time"

 "golang.org/x/crypto/ssh"
)

func (r *Remote) cancelCommand(ctx context.Context, session *ssh.Session, done <-chan error) error {
 // RFC 4254 signal requests tell OpenSSH to terminate the owned process group.
 // Channel Close alone is not a process kill and can leave Wait blocked on the
 // server's exit status until a long-running command finishes naturally.
 signalled := make(chan struct{})
 go func() { _ = session.Signal(ssh.SIGKILL); close(signalled) }()
 select { case <-signalled: case <-time.After(150*time.Millisecond): }
 go func() { _ = session.Close() }()
 select {
 case <-done:
  return ctx.Err()
 case <-time.After(750*time.Millisecond):
  // A non-cooperative peer may block channel writes too. Close the owning
  // transport as a last resort; endpoint reconnection handles subsequent use.
  _ = r.Close()
 }
 select { case <-done: case <-time.After(750*time.Millisecond): }
 return ctx.Err()
}
