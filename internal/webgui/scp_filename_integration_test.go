//go:build integration && !windows

package webgui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

// Run the public queue against actual OpenSSH and the controller filesystem.
// Disable only the fixture's rsync executable so success must name SCP (or,
// for protocol-inexpressible LF/CR names, the lossless stream fallback).
func TestHostedQueuedSCPPreservesDistinctWhitespaceNames(t *testing.T) {
	source, _, document := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	info, err := source.Stat(ctx, "/usr/bin/rsync")
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Exec(ctx, "sudo -n chmod 0644 /usr/bin/rsync", endpoint.ExecOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		restore, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := source.Exec(restore, fmt.Sprintf("sudo -n chmod %04o /usr/bin/rsync", info.Mode.Perm()), endpoint.ExecOptions{}); err != nil {
			t.Error(err)
		}
	})
	for _, newline := range []bool{false, true} {
		t.Run(fmt.Sprintf("newline=%t", newline), func(t *testing.T) {
			root := remoteTempDir(t, ctx, source)
			defer source.Remove(context.Background(), root, true)
			from := source.Join(root, "source ")
			if err := source.MkdirAll(ctx, from, 0700); err != nil {
				t.Fatal(err)
			}
			names := []string{"report", "report ", "report  ", "\treport\t"}
			if newline {
				names = append(names, "report\nnext", "report\rnext")
			}
			for i, name := range names {
				writeRemoteFile(t, ctx, source, source.Join(from, name), fmt.Sprintf("distinct contents %d", i))
			}
			app := unlockedTestApp(t)
			app.mu.Lock()
			app.document = document.Clone()
			app.mu.Unlock()
			if _, err := app.ChangeEndpoint(LeftPane, source.Name(), "本机"); err != nil {
				t.Fatal(err)
			}
			done := make(chan JobUpdateModel, 1)
			app.mu.Lock()
			app.eventSink = func(name string, value any) {
				if name == "challenge" {
					challenge := value.(ChallengeModel)
					app.ResolveChallenge(challenge.ID, false, "", false)
				}
				if name == "job:update" {
					job := value.(JobUpdateModel)
					if job.State == "succeeded" || job.State == "failed" || job.State == "cancelled" {
						select {
						case done <- job:
						default:
						}
					}
				}
			}
			app.mu.Unlock()
			targetRoot := t.TempDir()
			preview, err := app.PrepareDrop(LeftPane, from, RightPane, targetRoot)
			if err != nil {
				t.Fatal(err)
			}
			id, err := app.QueueTransfer(TransferRequest{DropPreview: preview})
			if err != nil {
				t.Fatal(err)
			}
			var result JobUpdateModel
			select {
			case result = <-done:
			case <-ctx.Done():
				t.Fatal("queued filename transfer did not finish")
			}
			if result.ID != id || result.State != "succeeded" {
				t.Fatalf("queued filename transfer failed: %+v", result)
			}
			if !newline && result.Method != "direct · target-pull · scp" {
				t.Fatalf("fixture did not exercise actual SCP: %s", result.Method)
			}
			if newline && !strings.Contains(result.Method, "memory-stream") {
				t.Fatalf("newline names did not use lossless stream fallback: %s", result.Method)
			}
			target := filepath.Join(targetRoot, "source ")
			entries, err := os.ReadDir(target)
			if err != nil || len(entries) != len(names) {
				t.Fatalf("transfer collapsed distinct filename bytes: entries=%d want=%d err=%v", len(entries), len(names), err)
			}
			for i, name := range names {
				contents, err := os.ReadFile(filepath.Join(target, name))
				if err != nil || string(contents) != fmt.Sprintf("distinct contents %d", i) {
					t.Fatalf("transfer changed filename/content for %q: %v", name, err)
				}
			}
		})
	}
}
