//go:build integration && !windows

package webgui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/configtext"
	"github.com/lovitus/dragfm-gui/internal/jobs"
)

func TestHostedQueuedTransferDoesNotFollowEditedHostName(t *testing.T) {
	source, target, document := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
	defer source.Remove(context.Background(), sourceRoot, true)
	defer target.Remove(context.Background(), targetRoot, true)
	if _, err := source.Stat(ctx, targetRoot); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("fixture shadow path must be absent before setup")
	}
	if err := source.MkdirAll(ctx, targetRoot, 0700); err != nil {
		t.Fatal(err)
	}
	defer source.Remove(context.Background(), targetRoot, true)
	for index := range document.Hosts {
		host := &document.Hosts[index]
		if len(host.KeyIDs) == 0 {
			t.Fatal("fixture requires an explicitly configured key")
		}
		host.RouteSpec = fmt.Sprintf(`%s@%s:%d --keys "%s"`, host.User, host.Address, host.Port, host.KeyIDs[0])
		host.HopFingerprints = []string{host.HostFingerprint}
	}
	app := unlockedTestApp(t)
	app.mu.Lock()
	app.document = document.Clone()
	app.mu.Unlock()
	if _, err := app.SaveConfigTexts(configtext.Markdown(document)); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ChangeEndpoint(LeftPane, source.Name(), target.Name()); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ChangeEndpoint(RightPane, target.Name(), source.Name()); err != nil {
		t.Fatal(err)
	}
	from, to := source.Join(sourceRoot, "queued.txt"), target.Join(targetRoot, "queued.txt")
	const contents = "this file must reach the endpoint selected before editing"
	writeRemoteFile(t, ctx, source, from, contents)
	gate, started := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	defer release()
	if _, err := app.queue.Submit(jobs.Job{ID: "binding-barrier", Run: func(ctx context.Context, _ func(jobs.Update)) error {
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
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	done := make(chan JobUpdateModel, 1)
	app.mu.Lock()
	app.eventSink = func(event string, value any) {
		if event == "challenge" {
			challenge := value.(ChallengeModel)
			accepted := challenge.Kind == "confirm-host-key" && strings.Contains(challenge.Message, document.Hosts[0].HostFingerprint)
			app.ResolveChallenge(challenge.ID, accepted, "", false)
			return
		}
		if event != "job:update" {
			return
		}
		update := value.(JobUpdateModel)
		if update.ID != "binding-barrier" && (update.State == "succeeded" || update.State == "failed" || update.State == "cancelled") {
			select {
			case done <- update:
			default:
			}
		}
	}
	app.mu.Unlock()
	preview, err := app.PrepareDrop(LeftPane, from, RightPane, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	jobID, err := app.QueueTransfer(TransferRequest{DropPreview: preview})
	if err != nil {
		t.Fatal(err)
	}
	// Keep the display name, replace its route with a different real host.
	// Old helpers resolve this name at execution time and write the shadow
	// path on source; a bound job must continue to use the original target.
	app.mu.RLock()
	edited := app.document.Clone()
	app.mu.RUnlock()
	edited.Hosts[1].RouteSpec = edited.Hosts[0].RouteSpec
	if _, err := app.SaveConfigTexts(configtext.Markdown(edited)); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ChangeEndpoint(RightPane, target.Name(), source.Name()); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case update := <-done:
		if update.ID != jobID || update.State != "succeeded" {
			t.Fatalf("original queued transfer did not succeed: state=%s message=%s", update.State, update.Message)
		}
	case <-ctx.Done():
		t.Fatal("queued transfer did not finish within fixture deadline")
	}
	assertRemoteFile(t, ctx, target, to, contents)
	assertRemoteFile(t, ctx, source, from, contents)
	if _, err := source.Stat(ctx, to); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("queued transfer wrote to the replacement host")
	}
}
