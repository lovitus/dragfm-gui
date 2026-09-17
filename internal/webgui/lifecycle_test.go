package webgui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/vault"
)

func unlockedTestApp(t *testing.T) *App {
	t.Helper()
	app := New(filepath.Join(t.TempDir(), vault.FileName))
	if _, err := app.CreateVault("test hint", "test master password", "test master password"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Lock() })
	return app
}

func TestEveryFrontendRPCExistsOnNativeApp(t *testing.T) {
	source, err := os.ReadFile("../../frontend/src/api.ts")
	if err != nil {
		t.Fatal(err)
	}
	calls := regexp.MustCompile(`invoke\('([^']+)'`).FindAllStringSubmatch(string(source), -1)
	if len(calls) < 20 {
		t.Fatal("RPC contract parser found too few methods")
	}
	appType := reflect.TypeOf((*App)(nil))
	for _, call := range calls {
		if _, ok := appType.MethodByName(call[1]); !ok {
			t.Errorf("frontend calls missing native RPC %s", call[1])
		}
	}
}

func TestLockDrainsCancelledHistoryAndUnlockUsesFreshQueue(t *testing.T) {
	app := unlockedTestApp(t)
	app.mu.Lock()
	app.document.Hosts = []config.Host{{Password: "bare-fixture-secret"}}
	generation := app.generation
	app.mu.Unlock()
	started := make(chan struct{})
	if _, err := app.submitFor(generation, jobs.Job{ID: "running", Description: "bare-fixture-secret", Run: func(ctx context.Context, emit func(jobs.Update)) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := app.submitFor(generation, jobs.Job{ID: "pending", Run: func(context.Context, func(jobs.Update)) error { t.Error("pending job ran"); return nil }}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := app.JobSnapshot()
	if err != nil || len(snapshot) != 2 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if snapshot[0].Description == "bare-fixture-secret" {
		t.Fatal("snapshot leaked a known secret")
	}
	if err := app.Lock(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.QueueCommand("控制机", "echo must-not-run"); err == nil {
		t.Fatal("locked application accepted a command")
	}
	if _, err := app.JobSnapshot(); err == nil {
		t.Fatal("locked application exposed job state")
	}
	_, persisted, err := vault.Open(app.vaultPath, []byte("test master password"))
	if err != nil || len(persisted.History) != 2 {
		t.Fatalf("history=%+v err=%v", persisted.History, err)
	}
	for _, item := range persisted.History {
		if item.Success || strings.Contains(item.Operation, "bare-fixture-secret") {
			t.Fatalf("unsafe history: %+v", item)
		}
	}
	oldQueue := app.queue
	if _, err := app.Unlock("test master password"); err != nil {
		t.Fatal(err)
	}
	if app.queue == oldQueue {
		t.Fatal("unlock reused the cancelled queue")
	}
	if state, err := app.JobSnapshot(); err != nil || len(state) != 0 {
		t.Fatalf("pending jobs restored: %+v %v", state, err)
	}
	if _, err := app.submitFor(generation, jobs.Job{Run: func(context.Context, func(jobs.Update)) error { return nil }}); err == nil {
		t.Fatal("old session admitted work after unlock")
	}
}

func TestCommandOutputIsConcurrentAndBounded(t *testing.T) {
	var output commandOutput
	var writers sync.WaitGroup
	for i := 0; i < 16; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for j := 0; j < 32; j++ {
				_, _ = io.WriteString(&output, strings.Repeat("x", 1024))
			}
		}()
	}
	writers.Wait()
	result := output.String()
	if len(result) > maxCommandOutput+128 || !strings.Contains(result, "64 KiB") {
		t.Fatalf("output length=%d", len(result))
	}
}

func TestTerminalWaitsForReadyBeforeEmittingInitialPrompt(t *testing.T) {
	app := unlockedTestApp(t)
	events := make(chan terminalDataModel, 64)
	app.eventSink = func(name string, value any) {
		if name == "terminal:data" {
			select {
			case events <- value.(terminalDataModel):
			default:
			}
		}
	}
	id, err := app.StartTerminal(LeftPane, t.TempDir(), 24, 80)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
		t.Fatal("prompt emitted before TerminalReady")
	case <-time.After(100 * time.Millisecond):
	}
	if err := app.TerminalReady(id); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Session != id {
			t.Fatal("wrong terminal session")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("initial PTY output never arrived")
	}
	app.CloseTerminal(id)
}
