package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func awaitDone(t *testing.T, q *Queue) {
	t.Helper()
	select {
	case <-q.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}

func TestCloseWithoutEventConsumerRetainsFinalSnapshot(t *testing.T) {
	q := New(1)
	started := make(chan struct{})
	_, err := q.Submit(Job{ID: "running", Run: func(ctx context.Context, emit func(Update)) error {
		close(started)
		for i := 0; i < eventLimit*2; i++ {
			emit(Update{Message: fmt.Sprint(i)})
		}
		<-ctx.Done()
		return ctx.Err()
	}})
	if err != nil { t.Fatal(err) }
	<-started
	if _, err := q.Submit(Job{ID: "pending", Run: func(context.Context, func(Update)) error {
		t.Error("pending job ran during shutdown")
		return nil
	}}); err != nil { t.Fatal(err) }
	q.Close()
	awaitDone(t, q)
	snapshot := q.Snapshot()
	if len(snapshot) != 2 { t.Fatalf("snapshot=%+v", snapshot) }
	for _, job := range snapshot {
		if job.State != Cancelled || job.FinishedAt.IsZero() {
			t.Fatalf("missing cancellation state: %+v", job)
		}
	}
	if _, err := q.Submit(Job{Run: func(context.Context, func(Update)) error { return nil }}); err == nil {
		t.Fatal("closed queue accepted work")
	}
}

func TestConcurrentSubmitCancelClose(t *testing.T) {
	for iteration := 0; iteration < 50; iteration++ {
		q := New(8)
		var callers sync.WaitGroup
		for index := 0; index < 16; index++ {
			callers.Add(1)
			go func(index int) {
				defer callers.Done()
				id := fmt.Sprint(index)
				_, err := q.Submit(Job{ID: id, Run: func(ctx context.Context, emit func(Update)) error {
					emit(Update{ProgressKnown: true, Progress: 0.5})
					<-ctx.Done()
					return ctx.Err()
				}})
				if err == nil { q.Cancel(id) }
			}(index)
		}
		q.Close()
		callers.Wait()
		awaitDone(t, q)
		for range q.Updates() {}
	}
}

func TestPanicIsFailedAndNextJobRuns(t *testing.T) {
	q := New(4)
	defer q.Close()
	if _, err := q.Submit(Job{ID: "panic", Run: func(context.Context, func(Update)) error { panic("runner failure") }}); err != nil { t.Fatal(err) }
	if _, err := q.Submit(Job{ID: "next", Run: func(context.Context, func(Update)) error { return nil }}); err != nil { t.Fatal(err) }
	deadline := time.After(3 * time.Second)
	var failed, succeeded bool
	for !failed || !succeeded {
		select {
		case update := <-q.Updates():
			failed = failed || (update.ID == "panic" && update.State == Failed)
			succeeded = succeeded || (update.ID == "next" && update.State == Succeeded)
		case <-deadline:
			t.Fatal("panic stopped the worker")
		}
	}
}

func TestConcurrentProgressAndDuplicateID(t *testing.T) {
	q := New(2)
	defer q.Close()
	started, release := make(chan struct{}), make(chan struct{})
	_, err := q.Submit(Job{ID: "copy", Run: func(context.Context, func(Update)) error {
		close(started)
		<-release
		return errors.New("expected failure")
	}})
	if err != nil { t.Fatal(err) }
	<-started
	if _, err := q.Submit(Job{ID: "copy", Run: func(context.Context, func(Update)) error { return nil }}); err == nil {
		t.Fatal("duplicate active ID accepted")
	}
	close(release)
	_, err = q.Submit(Job{ID: "concurrent", Run: func(_ context.Context, emit func(Update)) error {
		var writers sync.WaitGroup
		for i := 0; i < 10; i++ {
			writers.Add(1)
			go func() {
				defer writers.Done()
				emit(Update{Progress: .5, ProgressKnown: true, BytesDone: 50, BytesTotal: 100, Stage: "copy", Method: "stream"})
			}()
		}
		writers.Wait()
		emit(Update{Message: "verified"})
		return nil
	}})
	if err != nil { t.Fatal(err) }
	deadline := time.After(3 * time.Second)
	var revision uint64
	for {
		select {
		case update := <-q.Updates():
			if update.Revision <= revision { t.Fatalf("non-monotonic events: %+v", update) }
			revision = update.Revision
			if update.ID == "concurrent" && update.State == Succeeded {
				if update.BytesDone != 50 || update.BytesTotal != 100 || update.Stage != "copy" || update.Method != "stream" {
					t.Fatalf("final update lost counters: %+v", update)
				}
				return
			}
		case <-deadline:
			t.Fatal("concurrent progress did not finish")
		}
	}
}
