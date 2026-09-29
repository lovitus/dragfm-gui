//go:build integration && !windows

package webgui

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/remoteagent"
	"github.com/lovitus/dragfm-gui/internal/vault"
)

// Actual queue -> SSH installation -> encrypted vault -> close/Lock. The
// baseline API and JSON projection keep old-red behavioral: no durable record,
// or a helper created despite an unwritable vault, not an undefined symbol.
func TestHostedHelperInstallationJournalLifecycle(t *testing.T) {
	for _, candidate := range []string{"ordinary", "sudo", "lock", "write-failure", "partial-crash"} {
		t.Run(candidate, func(t *testing.T) {
			source, _, document := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			var architecture bytes.Buffer
			if err := source.Exec(ctx, "uname -m", endpoint.ExecOptions{Stdout: &architecture}); err != nil {
				t.Fatal(err)
			}
			before, err := source.List(ctx, "/tmp")
			if err != nil {
				t.Fatal(err)
			}
			installations := func(entries []endpoint.Entry) map[string]bool {
				names := make(map[string]bool)
				for _, item := range entries {
					if strings.HasPrefix(item.Name, ".dragfm-") {
						names[item.Name] = true
					}
				}
				return names
			}
			app := unlockedTestApp(t)
			app.mu.Lock()
			app.document = document.Clone()
			generation := app.generation
			app.mu.Unlock()
			if err := app.save(); err != nil {
				t.Fatal(err)
			}
			if candidate == "write-failure" {
				parent := filepath.Join(t.TempDir(), "not-a-directory")
				if err := os.WriteFile(parent, []byte("filesystem failure fixture"), 0600); err != nil {
					t.Fatal(err)
				}
				app.saveMu.Lock()
				app.mu.Lock()
				app.store.Path = filepath.Join(parent, vault.FileName)
				app.mu.Unlock()
				app.saveMu.Unlock()
				defer func() {
					app.saveMu.Lock()
					defer app.saveMu.Unlock()
					app.mu.Lock()
					defer app.mu.Unlock()
					if app.store != nil {
						app.store.Path = app.vaultPath
					}
				}()
			}
			type started struct {
				helper *remoteagent.Session
				err    error
			}
			ready, release := make(chan started, 1), make(chan struct{}, 1)
			defer close(release)
			finished := make(chan JobUpdateModel, 1)
			app.mu.Lock()
			app.eventSink = func(name string, value any) {
				if name == "job:update" {
					update := value.(JobUpdateModel)
					if update.ID == "helper-journal" && (update.State == "failed" || update.State == "succeeded" || update.State == "cancelled") {
						finished <- update
					}
				}
			}
			app.mu.Unlock()
			if _, err := app.submitFor(generation, jobs.Job{ID: "helper-journal", Description: "hosted helper recovery lifecycle", Run: func(jobCtx context.Context, _ func(jobs.Update)) error {
				call, stop := context.WithTimeout(jobCtx, 40*time.Second)
				defer stop()
				helper, cleanup, err := app.startTransferAgent(call, source, strings.TrimSpace(architecture.String()), candidate == "sudo", "")
				ready <- started{helper: helper, err: err}
				if err != nil {
					return err
				}
				defer cleanup() // Both the baseline func() and current func() error compile.
				select {
				case <-release:
					return helper.Close()
				case <-call.Done():
					return errors.Join(call.Err(), helper.Close())
				}
			}}); err != nil {
				t.Fatal(err)
			}
			var active started
			select {
			case active = <-ready:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if candidate == "write-failure" {
				if active.err == nil {
					t.Fatal("unwritable vault still allowed an unjournaled helper to start")
				}
				var pathErr *fs.PathError
				if !errors.As(active.err, &pathErr) {
					t.Fatalf("failure was not the injected vault filesystem failure: %v", active.err)
				}
				after, err := source.List(ctx, "/tmp")
				if err != nil || !reflect.DeepEqual(installations(before), installations(after)) {
					t.Fatalf("failed write-ahead save changed remote installation namespace: %v", err)
				}
				return
			}
			if active.err != nil {
				t.Fatal(active.err)
			}
			directory := active.helper.Directory
			records := readHelperJournal(t, app.vaultPath)
			if len(records) != 1 {
				t.Fatalf("live helper has %d durable installation records, want 1", len(records))
			}
			record := records[0]
			identity, err := source.Identity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if record.Path != directory || record.HostID != document.Hosts[0].ID || record.Fingerprint != identity.Fingerprint || record.MachineID != identity.MachineID || record.Elevated != (candidate == "sudo") || record.DirectoryID == "" || record.ParentID == "" {
				t.Fatal("durable record is not bound to the admitted host, actual identity, inode and privilege")
			}
			var marker bytes.Buffer
			if err := source.Exec(ctx, "cat -- "+shellQuote(directory+"/.dragfm-owner-v1"), endpoint.ExecOptions{Stdout: &marker}); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(marker.Bytes())
			if record.MarkerSHA256 != hex.EncodeToString(digest[:]) {
				t.Fatal("journal does not match actual installation ownership marker")
			}
			if ciphertext, err := os.ReadFile(app.vaultPath); err != nil || bytes.Contains(ciphertext, []byte(directory)) {
				t.Fatalf("installation metadata exposed outside encrypted payload: %v", err)
			}
			if _, err := active.helper.Call("probe", nil, nil); err != nil {
				t.Fatal("journaled helper cannot serve a real protocol request", err)
			}
			if candidate == "partial-crash" {
				exerciseHelperPartialRecovery(t, ctx, app, source, active.helper, release, finished)
				return
			}
			if candidate == "lock" {
				if err := app.Lock(); err != nil {
					t.Fatal(err)
				}
				if after := readHelperJournal(t, app.vaultPath); len(after) != 1 || !reflect.DeepEqual(records, after) {
					t.Fatal("old task erased or rewrote its recovery record across vault Lock")
				}
				if _, err := app.Unlock("test master password"); err != nil {
					t.Fatal(err)
				}
				if snapshot, err := app.JobSnapshot(); err != nil || len(snapshot) != 0 {
					t.Fatal("unlock resumed old Pending/helper work", err)
				}
			} else {
				release <- struct{}{}
				select {
				case final := <-finished:
					if final.State != "succeeded" {
						t.Fatalf("helper cleanup: %s: %s", final.State, final.Message)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if _, err := source.Stat(ctx, directory); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("acknowledged helper cleanup left installation: %v", err)
				}
				if records := readHelperJournal(t, app.vaultPath); len(records) != 0 {
					t.Fatal("acknowledged cleanup did not retire its durable record")
				}
			}
			if _, err := source.List(ctx, "/tmp"); err != nil {
				t.Fatal("helper journal lifecycle disrupted browsing", err)
			}
		})
	}
}

type helperJournalWire struct {
	HostID       string `json:"host_id"`
	Fingerprint  string `json:"fingerprint"`
	MachineID    string `json:"machine_id"`
	Path         string `json:"path"`
	ParentID     string `json:"parent_id"`
	DirectoryID  string `json:"directory_id"`
	MarkerSHA256 string `json:"marker_sha256"`
	Elevated     bool   `json:"elevated"`
	Partials     []struct {
		Path     string `json:"path"`
		ParentID string `json:"parent_id"`
		FileID   string `json:"file_id"`
	} `json:"partials"`
}

// Real SFTP creation pins the inode BEFORE writing data. Kill the control
// process while an independent SFTP process is still stopped with its lease.
// Reopen the actual vault, reconnect: first preserve the live writer; only
// after pidfd-observed exit may the next connection reclaim the partial.
func exerciseHelperPartialRecovery(t *testing.T, ctx context.Context, app *App, source *endpoint.Remote, helper *remoteagent.Session, release chan<- struct{}, finished <-chan JobUpdateModel) {
	t.Helper()
	// The baseline has already failed the absent-journal assertion above.
	// This interface keeps the old test overlay compilable while exercising
	// the real file-view implementation on the candidate, not a mock endpoint.
	opener, ok := any(helper).(interface {
		OpenFiles(context.Context) (*endpoint.Remote, error)
	})
	if !ok {
		t.Fatal("journaled helper does not expose its independent file channel")
	}
	files, err := opener.OpenFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parent := remoteTempDir(t, ctx, source)
	t.Cleanup(func() { _ = source.Remove(context.Background(), parent, true) })
	writer, err := files.CreateAtomic(ctx, source.Join(parent, "unpublished.txt"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	records := readHelperJournal(t, app.vaultPath)
	if len(records) != 1 || len(records[0].Partials) != 1 || records[0].Partials[0].FileID == "" || records[0].Partials[0].ParentID == "" {
		t.Fatal("SFTP returned a writable file before its real inode was durable")
	}
	partial := records[0].Partials[0].Path
	if _, err := io.WriteString(writer, "partial from an actual interrupted SFTP writer"); err != nil {
		t.Fatal(err)
	}
	input, feed := io.Pipe()
	output, stream := io.Pipe()
	defer input.Close()
	defer feed.Close()
	defer output.Close()
	defer stream.Close()
	observerDone := make(chan error, 1)
	go func() {
		command := "sudo -n python3 -u -c " + shellQuote(helperExitObserver) + " " + shellQuote(helper.Directory) + " crash sftp preserve"
		err := source.Exec(ctx, command, endpoint.ExecOptions{Stdin: input, Stdout: stream})
		_ = stream.CloseWithError(err)
		observerDone <- err
	}()
	stop := context.AfterFunc(ctx, func() { _ = output.CloseWithError(ctx.Err()); _ = feed.CloseWithError(ctx.Err()) })
	defer stop()
	reader := bufio.NewReader(output)
	if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("real SFTP crash fixture not ready: %q %v", line, err)
	}
	if err := helper.Close(); err == nil {
		t.Fatal("control loss with a stopped SFTP writer claimed cleanup success")
	}
	release <- struct{}{}
	select {
	case final := <-finished:
		if final.State != "failed" {
			t.Fatalf("interrupted helper job ended %s", final.State)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	ageJournaledWorkspace(t, ctx, app, source, helper.Directory)
	recovered := make(chan JobUpdateModel, 2)
	app.mu.Lock()
	app.eventSink = func(name string, value any) {
		if name == "job:update" {
			update := value.(JobUpdateModel)
			if strings.HasPrefix(update.Description, "清理过期临时资源 · ") && (update.State == "succeeded" || update.State == "failed" || update.State == "cancelled") {
				recovered <- update
			}
		}
	}
	app.mu.Unlock()
	reconnect := func() {
		if err := app.Lock(); err != nil {
			t.Fatal(err)
		}
		if _, err := app.Unlock("test master password"); err != nil {
			t.Fatal(err)
		}
		if _, err := app.ChangeEndpoint(LeftPane, "ci-source", ""); err != nil {
			t.Fatal(err)
		}
		select {
		case final := <-recovered:
			if final.State != "succeeded" {
				t.Fatalf("recovery: %s: %s", final.State, final.Message)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	reconnect()
	assertRemoteFile(t, ctx, source, partial, "partial from an actual interrupted SFTP writer")
	if records := readHelperJournal(t, app.vaultPath); len(records) != 1 {
		t.Fatal("live SFTP writer lost its durable recovery record")
	}
	if _, err := io.WriteString(feed, "finish\n"); err != nil {
		t.Fatal(err)
	}
	_ = feed.Close()
	if line, err := reader.ReadString('\n'); err != nil || line != "exited\n" {
		t.Fatalf("writer exit not observed: %q %v", line, err)
	}
	select {
	case err := <-observerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	reconnect()
	for _, path := range []string{partial, helper.Directory, source.Join(parent, "unpublished.txt")} {
		if _, err := source.Stat(ctx, path); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("unpublished/owned path unexpectedly exists: %v", err)
		}
	}
	if records := readHelperJournal(t, app.vaultPath); len(records) != 0 {
		t.Fatal("recovered partial/install still journaled")
	}
	if _, err := app.List(LeftPane, "ci-source", parent); err != nil {
		t.Fatal("recovery disrupted browsing", err)
	}
}

// Age only the disposable marker and its existing encrypted record. No clock,
// expiry policy, file endpoint, or production cleanup method is replaced.
func ageJournaledWorkspace(t *testing.T, ctx context.Context, app *App, source *endpoint.Remote, directory string) {
	t.Helper()
	var marker bytes.Buffer
	if err := source.Exec(ctx, "cat -- "+shellQuote(directory+"/.dragfm-owner-v1"), endpoint.ExecOptions{Stdout: &marker}); err != nil {
		t.Fatal(err)
	}
	var owner map[string]any
	if err := json.Unmarshal(marker.Bytes(), &owner); err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-48 * time.Hour)
	owner["created"] = created
	data, _ := json.Marshal(owner)
	writeRemoteFile(t, ctx, source, directory+"/.dragfm-owner-v1", string(data))
	if err := source.Chmod(ctx, directory+"/.dragfm-owner-v1", 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	app.mu.Lock()
	encoded, _ := json.Marshal(app.document)
	var document map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &document)
	var saved []map[string]any
	_ = json.Unmarshal(document["workspaces"], &saved)
	if len(saved) != 1 || saved[0]["path"] != directory {
		app.mu.Unlock()
		t.Fatal("crash lost the actual workspace recovery record")
	}
	saved[0]["created_at"], saved[0]["marker_sha256"] = created, hex.EncodeToString(digest[:])
	document["workspaces"], _ = json.Marshal(saved)
	encoded, _ = json.Marshal(document)
	err := json.Unmarshal(encoded, &app.document)
	app.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := app.save(); err != nil {
		t.Fatal(err)
	}
}

func readHelperJournal(t *testing.T, file string) []helperJournalWire {
	t.Helper()
	_, document, err := vault.Open(file, []byte("test master password"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Workspaces []helperJournalWire `json:"workspaces"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(fmt.Errorf("decode durable journal: %w", err))
	}
	return wire.Workspaces
}
