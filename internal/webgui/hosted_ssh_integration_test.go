//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"github.com/lovitus/dragfm-gui/internal/vault"
)

func fixtureEndpoints(t *testing.T) (*endpoint.Remote, *endpoint.Remote, config.Document) {
	t.Helper()
	sourceName, targetName := os.Getenv("DRAGFM_E2E_SOURCE_SSH"), os.Getenv("DRAGFM_E2E_TARGET_SSH")
	if sourceName == "" || targetName == "" {
		t.Skip("disposable SSH fixtures are not configured")
	}
	sourceRoute, sourceKeys := openSSHRoute(t, sourceName)
	targetRoute, targetKeys := openSSHRoute(t, targetName)
	source, err := endpoint.DialSSH(context.Background(), "ci-source", sourceRoute.Hops[0].HostKey.PinnedSHA256, sourceRoute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	target, err := endpoint.DialSSH(context.Background(), "ci-target", targetRoute.Hops[0].HostKey.PinnedSHA256, targetRoute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	document := config.NewDocument()
	document.Keys = append(sourceKeys, targetKeys...)
	document.Hosts = []config.Host{hostFromRoute("source", "ci-source", sourceRoute, sourceKeys), hostFromRoute("target", "ci-target", targetRoute, targetKeys)}
	return source, target, document
}

func waitForJob(t *testing.T, app *App, id string, expected string) JobUpdateModel {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		snapshot, err := app.JobSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		for _, job := range snapshot {
			if job.ID != id {
				continue
			}
			if job.State == expected {
				return job
			}
			if job.State == "succeeded" || job.State == "failed" || job.State == "cancelled" {
				t.Fatalf("job %s finished as %s, expected %s: %s", id, job.State, expected, job.Message)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s", id, expected)
	return JobUpdateModel{}
}

func TestHostedSSHQueueAndHistory(t *testing.T) {
	source, target, document := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
	t.Cleanup(func() {
		_ = source.Remove(context.Background(), sourceRoot, true)
		_ = target.Remove(context.Background(), targetRoot, true)
	})
	if err := source.Exec(ctx, "head -c 67108864 /dev/zero > "+shellQuote(source.Join(sourceRoot, "first.bin")), endpoint.ExecOptions{}); err != nil {
		t.Fatal(err)
	}
	writeRemoteFile(t, ctx, source, source.Join(sourceRoot, "second.txt"), "real OpenSSH queued transfer\n")
	app := New(filepath.Join(t.TempDir(), vault.FileName))
	t.Cleanup(func() { _ = app.Lock() })
	if _, err := app.CreateVault("hosted fixture only", "ci-master-not-a-user-secret", "ci-master-not-a-user-secret"); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	app.document.Keys, app.document.Hosts = document.Keys, document.Hosts
	app.mu.Unlock()
	var eventMu sync.Mutex
	states := make(map[string]string)
	sawRunningAndPending := false
	invalidConcurrency := false
	app.mu.Lock()
	app.eventSink = func(name string, value any) {
		if name != "job:update" {
			return
		}
		event := value.(JobUpdateModel)
		eventMu.Lock()
		defer eventMu.Unlock()
		states[event.ID] = event.State
		running, pending := 0, 0
		for _, state := range states {
			if state == "running" {
				running++
			}
			if state == "pending" {
				pending++
			}
		}
		if running > 1 {
			invalidConcurrency = true
		}
		if running == 1 && pending > 0 {
			sawRunningAndPending = true
		}
	}
	app.mu.Unlock()
	if _, err := app.ChangeEndpoint(LeftPane, "ci-source", "ci-target"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ChangeEndpoint(RightPane, "ci-target", "ci-source"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.List(LeftPane, "ci-source", sourceRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := app.List(RightPane, "ci-target", targetRoot); err != nil {
		t.Fatal(err)
	}
	submit := func(name string) string {
		preview, err := app.PrepareDrop(LeftPane, source.Join(sourceRoot, name), RightPane, targetRoot)
		if err != nil {
			t.Fatal(err)
		}
		if preview.Conflict {
			t.Fatal("unexpected fixture conflict")
		}
		id, err := app.QueueTransfer(TransferRequest{DropPreview: preview})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	first, second := submit("first.bin"), submit("second.txt")
	one, two := waitForJob(t, app, first, "succeeded"), waitForJob(t, app, second, "succeeded")
	if one.BytesDone != 67108864 || two.BytesDone == 0 {
		t.Fatalf("lost final byte counts: %d, %d", one.BytesDone, two.BytesDone)
	}
	eventMu.Lock()
	pair, badConcurrency := sawRunningAndPending, invalidConcurrency
	eventMu.Unlock()
	if !pair || badConcurrency {
		t.Fatalf("running/pending pair=%v invalidConcurrency=%v", pair, badConcurrency)
	}
	sourceManifest, err := transfer.Snapshot(ctx, source, source.Join(sourceRoot, "first.bin"), true)
	if err != nil {
		t.Fatal(err)
	}
	targetManifest, err := transfer.Snapshot(ctx, target, target.Join(targetRoot, "first.bin"), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := transfer.CompareManifests(sourceManifest, targetManifest, true); err != nil {
		t.Fatal(err)
	}
	assertRemoteFile(t, ctx, target, target.Join(targetRoot, "second.txt"), "real OpenSSH queued transfer\n")
	failed, err := app.QueueCommand("左栏", "exit 19")
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, app, failed, "failed")
	// A real SSH command keeps the worker occupied while a real transfer is
	// cancelled in Pending. Cancellation must not perform the file operation.
	running, err := app.QueueCommand("左栏", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, app, running, "running")
	pendingPreview, err := app.PrepareDrop(LeftPane, source.Join(sourceRoot, "second.txt"), RightPane, targetRoot)
	if err != nil {
		t.Fatal(err)
	}
	pendingPreview.TargetPath = target.Join(targetRoot, "cancelled.txt")
	pending, err := app.QueueTransfer(TransferRequest{DropPreview: pendingPreview})
	if err != nil {
		t.Fatal(err)
	}
	app.CancelJob(pending)
	waitForJob(t, app, pending, "cancelled")
	app.CancelJob(running)
	waitForJob(t, app, running, "cancelled")
	if _, err := target.Stat(ctx, pendingPreview.TargetPath); !os.IsNotExist(err) {
		t.Fatalf("cancelled pending transfer wrote a target: %v", err)
	}
	if err := app.Lock(); err != nil {
		t.Fatal(err)
	}
	reopened := New(app.vaultPath)
	t.Cleanup(func() { _ = reopened.Lock() })
	boot, err := reopened.Unlock("ci-master-not-a-user-secret")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{first: false, second: false, failed: false, running: false, pending: false}
	for _, item := range boot.History {
		if _, expected := ids[item.ID]; expected {
			ids[item.ID] = true
		}
		if strings.Contains(item.Message, "PRIVATE KEY") || strings.Contains(item.Message, "ci-master-not-a-user-secret") {
			t.Fatal("history contains credentials")
		}
	}
	for id, found := range ids {
		if !found {
			t.Errorf("history did not survive restart for %s", id)
		}
	}
	snapshot, err := reopened.JobSnapshot()
	if err != nil || len(snapshot) != 0 {
		t.Fatalf("pending jobs resumed after restart: %+v, %v", snapshot, err)
	}
	t.Log("real SSH: two transfers, Running+Pending, complete hashes, failure, cancellation, five history entries restored; no pending work restored")
}

func TestHostedNonRootSudoTransfers(t *testing.T) {
	if os.Getenv("DRAGFM_E2E_SUDO") == "" {
		t.Skip("non-root sudo fixture not enabled")
	}
	source, target, document := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
	t.Cleanup(func() {
		_ = source.Exec(context.Background(), "sudo -n rm -rf -- "+shellQuote(sourceRoot), endpoint.ExecOptions{})
		_ = target.Exec(context.Background(), "sudo -n rm -rf -- "+shellQuote(targetRoot), endpoint.ExecOptions{})
	})
	app := New(filepath.Join(t.TempDir(), "unused.vault"))
	defer app.Lock()
	app.document = document
	for _, remote := range []*endpoint.Remote{source, target} {
		var uid bytes.Buffer
		if err := remote.Exec(ctx, "id -u; sudo -n id -u", endpoint.ExecOptions{Stdout: &uid}); err != nil {
			t.Fatal(err)
		}
		values := strings.Fields(uid.String())
		if len(values) != 2 || values[0] == "0" || values[1] != "0" {
			t.Fatalf("not a genuine non-root -> sudo fixture: %q", uid.String())
		}
	}
	for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
		t.Run(string(direction), func(t *testing.T) {
			name := string(direction) + ".txt"
			sourcePath, targetPath := source.Join(sourceRoot, name), target.Join(targetRoot, name)
			contents := "genuine scoped sudo " + string(direction)
			writeRemoteFile(t, ctx, source, sourcePath, contents)
			if direction == strategy.SourcePush {
				if err := source.Exec(ctx, "sudo -n chown root:root -- "+shellQuote(sourcePath)+" && sudo -n chmod 0600 -- "+shellQuote(sourcePath), endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
			} else {
				protected := target.Join(targetRoot, "protected")
				if err := target.Exec(ctx, "sudo -n mkdir -m 0700 -- "+shellQuote(protected), endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
				targetPath = target.Join(protected, name)
			}
			operation := transfer.Operation{Source: source, Destination: target, SourcePath: sourcePath, TargetPath: targetPath}
			report := mustPreflight(t, ctx, operation)
			if err := app.runAgentStream(ctx, operation, report, direction, false, "", nil, nil, ""); err == nil {
				t.Fatal("ordinary user unexpectedly accessed a protected fixture")
			}
			if err := app.runAgentStream(ctx, operation, report, direction, true, "", nil, nil, ""); err != nil {
				t.Fatalf("scoped sudo transfer: %v", err)
			}
			var output bytes.Buffer
			if err := target.Exec(ctx, "sudo -n cat -- "+shellQuote(targetPath), endpoint.ExecOptions{Stdout: &output}); err != nil {
				t.Fatal(err)
			}
			if output.String() != contents {
				t.Fatalf("sudo transfer corrupted data: %q", output.String())
			}
			if entry, err := source.Stat(ctx, sourcePath); err != nil || entry.Mode&fs.ModeType != 0 {
				t.Fatal("copy removed or replaced source")
			}
			t.Log(fmt.Sprintf("ordinary %s blocked; scoped sudo %s transferred verified contents", direction, direction))
		})
	}
}
