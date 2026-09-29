package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCancelledJobRetainsCleanupFailure(t *testing.T) {
	for _, cleanupFailed := range []bool{false, true} {
		name := "ordinary-cancel"
		if cleanupFailed {
			name = "cleanup-incomplete"
		}
		t.Run(name, func(t *testing.T) {
			queue := New(4)
			defer queue.Close()
			started := make(chan struct{})
			const detail = "owned temporary writer exit unconfirmed"
			id, err := queue.Submit(Job{Run: func(ctx context.Context, _ func(Update)) error {
				close(started)
				<-ctx.Done()
				if cleanupFailed {
					return errors.Join(ctx.Err(), errors.New(detail))
				}
				return ctx.Err()
			}})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			select {
			case <-started:
			case <-deadline.C:
				t.Fatal("job did not start")
			}
			if !queue.Cancel(id) {
				t.Fatal("running job was not cancelled")
			}
			for {
				select {
				case update := <-queue.Updates():
					if update.ID != id || update.State != Cancelled {
						continue
					}
					if cleanupFailed && (!strings.Contains(update.Message, detail) || !strings.Contains(update.Error, detail)) {
						t.Fatal("cancelled status hid the unconfirmed resource cleanup")
					}
					if !cleanupFailed && update.Message != "已取消" {
						t.Fatal("ordinary cancellation invented a cleanup warning")
					}
					for _, stored := range queue.Snapshot() {
						if stored.ID == id && stored.State == Cancelled && stored.Message == update.Message {
							return
						}
					}
					t.Fatal("snapshot lost the final cancellation detail")
				case <-deadline.C:
					t.Fatal("job cancellation did not finish")
				}
			}
		})
	}
}
