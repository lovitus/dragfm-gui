package strategy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRouteDeadlineFallsBackWithoutCancellingLiveJob(t *testing.T) {
	var events []Event
	recovered := false
	err := Execute(context.Background(), []Attempt{
		{Tier: Direct, Method: SCP, Run: func(ctx context.Context) error {
			probe, cancel := context.WithTimeout(ctx, time.Millisecond)
			defer cancel()
			<-probe.Done()
			return fmt.Errorf("route handshake: %w", probe.Err())
		}},
		{Tier: SOCKSPool, Method: SCP, Run: func(context.Context) error { recovered = true; return nil }},
	}, nil, func(event Event) { events = append(events, event) })
	if err != nil || !recovered || len(events) != 4 || events[1].Stage != "failed" || events[3].Stage != "succeeded" {
		t.Fatalf("route-local deadline aborted fallback: recovered=%t events=%+v error=%v", recovered, events, err)
	}
}

func TestOwningJobDeadlineStopsFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := Execute(ctx, []Attempt{
		{Run: func(context.Context) error { <-ctx.Done(); return ctx.Err() }},
		{Run: func(context.Context) error { t.Fatal("cancelled job tried another route"); return nil }},
	}, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) { t.Fatalf("owning deadline: %v", err) }
}

func TestNoEligibleAttemptDoesNotSucceed(t *testing.T) {
	if err := Execute(context.Background(), []Attempt{{}}, nil, nil); err == nil {
		t.Fatal("an empty strategy plan succeeded")
	}
}
