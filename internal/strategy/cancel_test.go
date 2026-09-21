package strategy

import (
	"context"
	"errors"
	"testing"
)

func TestCancellationNeverFallsThroughToAnotherTransport(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded} {
		called := false
		err := Execute(context.Background(), []Attempt{{Run: func(context.Context) error { return failure }}, {Run: func(context.Context) error { called = true; return nil }}}, nil, nil)
		if !errors.Is(err, failure) || called {
			t.Fatalf("cancelled attempt retried: %v %t", err, called)
		}
	}
}
