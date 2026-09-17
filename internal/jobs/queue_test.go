package jobs

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueRunsOneJobAtATime(t *testing.T) {
	t.Parallel()
	queue := New(4)
	defer queue.Close()
	var active, maximum atomic.Int32
	runner := func(context.Context, func(Update)) error {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return nil
	}
	_, _ = queue.Submit(Job{Run: runner})
	_, _ = queue.Submit(Job{Run: runner})
	completed := 0
	deadline := time.After(time.Second)
	for completed < 2 {
		select {
		case update := <-queue.Updates():
			if update.State == Succeeded {
				completed++
			}
		case <-deadline:
			t.Fatal("timeout")
		}
	}
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrency=%d", maximum.Load())
	}
}

func TestCancelPendingJob(t *testing.T) {
	queue := New(4)
	defer queue.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	_, err := queue.Submit(Job{ID: "blocker", Description: "blocker", Run: func(context.Context, func(Update)) error {
		close(started)
		<-release
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	run := false
	_, err = queue.Submit(Job{ID: "pending", Description: "pending", Run: func(context.Context, func(Update)) error {
		run = true
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !queue.Cancel("pending") {
		t.Fatal("pending job was not cancelled")
	}
	close(release)
	deadline := time.After(time.Second)
	for {
		select {
		case update := <-queue.Updates():
			if update.ID == "pending" && update.State == Cancelled {
				if run {
					t.Fatal("cancelled pending job ran")
				}
				return
			}
		case <-deadline:
			t.Fatal("missing cancelled update")
		}
	}
}

func TestProgressSurvivesMessageOnlyUpdatesAndSuccessCompletes(t *testing.T) {
	queue := New(4)
	defer queue.Close()
	_, err := queue.Submit(Job{ID: "progress", Description: "copy", Run: func(_ context.Context, emit func(Update)) error {
		emit(Update{Progress: 0.4, ProgressKnown: true, BytesDone: 40, BytesTotal: 100, Message: "40 bytes"})
		emit(Update{Message: "strategy still running"})
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	var messageOnly, completed *Update
	deadline := time.After(time.Second)
	for completed == nil {
		select {
		case update := <-queue.Updates():
			copy := update
			if update.State == Running && update.Message == "strategy still running" {
				messageOnly = &copy
			}
			if update.State == Succeeded {
				completed = &copy
			}
		case <-deadline:
			t.Fatal("timeout")
		}
	}
	if messageOnly == nil || !messageOnly.ProgressKnown || messageOnly.Progress != 0.4 {
		t.Fatalf("message-only update lost progress: %#v", messageOnly)
	}
	if !completed.ProgressKnown || completed.Progress != 1 {
		t.Fatalf("successful final update=%#v", completed)
	}
}
