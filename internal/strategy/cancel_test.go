package strategy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCancellationNeverFallsThroughToAnotherTransport(t *testing.T) {
	t.Run("explicit cancellation", func(t *testing.T) {
		called := false
		err := Execute(context.Background(), []Attempt{
			{Run: func(context.Context) error { return context.Canceled }},
			{Run: func(context.Context) error { called = true; return nil }},
		}, nil, nil)
		if !errors.Is(err, context.Canceled) || called {
			t.Fatalf("cancelled attempt retried: %v %t", err, called)
		}
	})
	t.Run("owning deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		called := false
		err := Execute(ctx, []Attempt{
			{Run: func(context.Context) error { <-ctx.Done(); return ctx.Err() }},
			{Run: func(context.Context) error { called = true; return nil }},
		}, nil, nil)
		if !errors.Is(err, context.DeadlineExceeded) || called {
			t.Fatalf("expired owning job retried: %v %t", err, called)
		}
	})
	t.Run("cancelled parent with transport error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		called := false
		err := Execute(ctx, []Attempt{
			{Run: func(context.Context) error { cancel(); return errors.New("socket closed") }},
			{Run: func(context.Context) error { called = true; return nil }},
		}, nil, nil)
		if !errors.Is(err, context.Canceled) || called {
			t.Fatalf("cancelled parent retried: %v %t", err, called)
		}
	})
	// Route-local deadlines with a still-live parent are intentionally tested
	// separately by TestRouteDeadlineFallsBackWithoutCancellingLiveJob. Merely
	// returning DeadlineExceeded from a child does not cancel the owning job.
}
