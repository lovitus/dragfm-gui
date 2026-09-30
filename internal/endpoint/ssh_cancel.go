package endpoint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

// Channel closure is not proof of remote process exit. Callers that own
// temporary files must not remove them while an unconfirmed writer may live.
var ErrCommandExitUnconfirmed = commandExitUnconfirmed{}

type commandExitUnconfirmed struct{}

func (commandExitUnconfirmed) Error() string   { return "SSH command exit could not be confirmed" }
func (commandExitUnconfirmed) Retryable() bool { return false }

func (r *Remote) cancelCommand(ctx context.Context, session *ssh.Session, done <-chan error) error {
	// OpenSSH signals with the login user's UID. sudo can forward TERM to its
	// privileged child but cannot forward KILL. Give its monitor time to relay
	// and reap before escalation; do not close the channel before Wait confirms
	// a real exit status/signal and drains inherited stdout/stderr pipes.
	for _, signal := range []ssh.Signal{ssh.SIGTERM, ssh.SIGKILL} {
		signalled := make(chan struct{})
		go func() { _ = session.Signal(signal); close(signalled) }()
		select {
		case <-signalled:
		case <-time.After(150 * time.Millisecond):
		}
		select {
		case err := <-done:
			var exited *ssh.ExitError
			if err == nil || errors.As(err, &exited) {
				// A wrapper can report an unknown writer exit through its own
				// reserved status. Preserve the structured result for that
				// caller to classify; cancellation must not erase it. Ordinary
				// Exec does not assign protocol meaning to any numeric status.
				return errors.Join(ctx.Err(), err)
			}
			return errors.Join(ctx.Err(), fmt.Errorf("%w: %w", ErrCommandExitUnconfirmed, err))
		case <-time.After(750 * time.Millisecond):
		}
	}
	// No exit evidence: close the session, then its owned transport if needed,
	// to release local goroutines. This does NOT turn uncertainty into success.
	go func() { _ = session.Close() }()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		go func() { _ = r.Close() }()
		select {
		case <-done:
		case <-time.After(300 * time.Millisecond):
		}
	}
	return errors.Join(ctx.Err(), ErrCommandExitUnconfirmed)
}
