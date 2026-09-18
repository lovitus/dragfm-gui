package agentservice

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestCancelledListenerReadyResultDoesNotBecomeTransportFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for index := 0; index < 100; index++ {
		ctx, cancel := context.WithCancel(context.Background())
		service := NewWithContext(ctx)
		job := &listenerJob{listener: listener, done: make(chan error, 1)}
		service.jobs["cancelled"] = job
		job.done <- net.ErrClosed
		cancel()
		// Both select arms are already ready; every scheduling choice must
		// preserve cancellation rather than falling through to another route.
		if err := service.wait(ctx, "cancelled"); !errors.Is(err, context.Canceled) {
			t.Fatalf("iteration %d: got %v", index, err)
		}
	}
}
