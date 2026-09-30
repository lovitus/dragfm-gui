package webgui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/jobs"
)

func TestQueuedJobSecretsStayRedactedAfterConfigDeletion(t *testing.T) {
	for _, outcome := range []string{"output", "error", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			app := unlockedTestApp(t)
			const secret = "queued-credential-fixture"
			if _, err := app.SaveConfigTexts("#主机\n##fixture\nu:" + secret + "@192.0.2.1:22\n#私钥\n#socks池\n"); err != nil {
				t.Fatal(err)
			}
			gate, started := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(gate) }) }
			defer release()
			if _, err := app.queue.Submit(jobs.Job{ID: "barrier", Run: func(ctx context.Context, _ func(jobs.Update)) error {
				close(started)
				select {
				case <-gate:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("queue did not start fixture barrier")
			}
			done := make(chan JobUpdateModel, 1)
			app.mu.Lock()
			app.eventSink = func(event string, value any) {
				if event != "job:update" {
					return
				}
				update := value.(JobUpdateModel)
				if update.ID == "secret-job" && (update.State == "succeeded" || update.State == "failed") {
					select {
					case done <- update:
					default:
					}
				}
			}
			generation := app.generation
			app.mu.Unlock()
			if _, err := app.submitFor(generation, jobs.Job{ID: "secret-job", Description: "fixture " + secret, Run: func(_ context.Context, emit func(jobs.Update)) error {
				switch outcome {
				case "error":
					return errors.New("fixture failure: " + secret)
				case "panic":
					panic("fixture panic: " + secret)
				default:
					emit(jobs.Update{Message: "fixture output: " + secret, Method: secret})
					return nil
				}
			}}); err != nil {
				t.Fatal(err)
			}
			if _, err := app.SaveConfigTexts("#主机\n#私钥\n#socks池\n"); err != nil {
				t.Fatal(err)
			}
			release()
			select {
			case update := <-done:
				if strings.Contains(update.Description+update.Message+update.Method, secret) {
					t.Fatal("deleted credential leaked from a queued job")
				}
				if !strings.Contains(update.Description, "***") {
					t.Fatal("queued credential was not redacted")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("queued job did not finish")
			}
			for _, update := range app.queue.Snapshot() {
				if update.ID == "secret-job" && strings.Contains(update.Description+update.Message+update.Error+update.Method, secret) {
					t.Fatal("unredacted credential remained in authoritative queue state")
				}
			}
		})
	}
}
