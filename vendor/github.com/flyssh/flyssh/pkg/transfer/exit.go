package transfer

import "fmt"

// ExitUnconfirmedError preserves the original transport/protocol failure when
// no remote exit status was received. A closed SSH connection is not proof
// that the remote process stopped writing. Consumers must not erase staging
// paths or start a replacement writer on this evidence.
type ExitUnconfirmedError struct{ Cause error }

func (e *ExitUnconfirmedError) Error() string {
	return fmt.Sprintf("remote process exit unconfirmed: %v", e.Cause)
}
func (e *ExitUnconfirmedError) Unwrap() error   { return e.Cause }
func (e *ExitUnconfirmedError) Retryable() bool { return false }
