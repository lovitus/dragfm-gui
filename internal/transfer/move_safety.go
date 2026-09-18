package transfer

import (
	"context"
	"errors"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

// preserveSourceError prevents a fallback from taking a new baseline after a
// transfer has committed but failed safety verification. Retrying such a move
// could otherwise delete a source that changed after the user's initial request.
type preserveSourceError struct{ error }

func (e preserveSourceError) Unwrap() error   { return e.error }
func (e preserveSourceError) Retryable() bool { return false }

func PreserveSource(err error) error {
	if err == nil {
		return nil
	}
	return preserveSourceError{err}
}

// VerifySourceUnchanged is deliberately called after destination verification,
// immediately before removal. Destination hashing can itself take a long time.
func VerifySourceUnchanged(ctx context.Context, source endpoint.Endpoint, root string, before Manifest) error {
	if err := ctx.Err(); err != nil {
		return PreserveSource(err)
	}
	after, err := Snapshot(ctx, source, root, true)
	if err != nil {
		return PreserveSource(err)
	}
	if err := CompareManifests(before, after, false); err != nil {
		return errors.Join(ErrSourceChanged, err)
	}
	return nil
}

// Retryable reports whether a failed attempt may safely fall through to a new route.
func Retryable(err error) bool {
	var marker interface{ Retryable() bool }
	return !errors.As(err, &marker) || marker.Retryable()
}
