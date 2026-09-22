package webgui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/vault"
)

func TestFilterCWDMarkersAcrossReads(t *testing.T) {
	t.Parallel()
	reader := io.MultiReader(strings.NewReader("before\x1b]777;drag"), strings.NewReader("fm-cwd=/tmp/work\x07after"))
	var directory string
	output, err := io.ReadAll(filterCWDMarkers(reader, func(value string) { directory = value }))
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "beforeafter" || directory != "/tmp/work" {
		t.Fatalf("output=%q directory=%q", output, directory)
	}
}

func TestFilterCWDMarkersDoesNotHoldPromptTail(t *testing.T) {
	t.Parallel()
	source, input := io.Pipe()
	defer input.Close()
	filtered := filterCWDMarkers(source, func(string) {})
	prompt := []byte("fanli@host /Users % ")
	go func() { _, _ = input.Write(prompt) }()
	result := make(chan []byte, 1)
	go func() {
		output := make([]byte, len(prompt))
		_, _ = io.ReadFull(filtered, output)
		result <- output
	}()
	select {
	case output := <-result:
		if string(output) != string(prompt) {
			t.Fatalf("output=%q want=%q", output, prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt tail was held waiting for another PTY write")
	}
}

func TestRedactInlineCredentials(t *testing.T) {
	t.Parallel()
	input := `copy via ops:secret@host:22 and user:pass@127.0.0.1:1080`
	output := redact(input)
	if strings.Contains(output, "secret") || strings.Contains(output, "pass@") {
		t.Fatalf("secret remained in %q", output)
	}
}

func TestRedactPreservesFormattingAndCoversAllVaultSecretForms(t *testing.T) {
	t.Parallel()
	input := "line one\n" +
		`uhome:"complex password!"@host:22 user:p%40ss@127.0.0.1:1080` + "\n" +
		"###sudo密码\nsudo secret\npassword=plain-secret\n" +
		"-----BEGIN OPENSSH PRIVATE KEY-----\nprivate-body\n-----END OPENSSH PRIVATE KEY-----\nline last"
	output := redact(input)
	for _, secret := range []string{"complex password", "p%40ss", "sudo secret", "plain-secret", "private-body"} {
		if strings.Contains(output, secret) {
			t.Fatalf("secret %q remained in:\n%s", secret, output)
		}
	}
	if strings.Count(output, "\n") != strings.Count(input, "\n") || !strings.HasPrefix(output, "line one\n") || !strings.HasSuffix(output, "\nline last") {
		t.Fatalf("formatting changed:\n%s", output)
	}
}

func TestHistoryRestartsRedactedAndQueueStateDoesNotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), vault.FileName)
	password := "restart-test-password"
	app := New(path)
	defer app.queue.Close()
	if _, err := app.CreateVault("restart hint", password, password); err != nil {
		t.Fatal(err)
	}
	_, err := app.queue.Submit(jobs.Job{Description: `copy via user:"history secret"@host:22`, Run: func(_ context.Context, emit func(jobs.Update)) error {
		emit(jobs.Update{Message: "password=message-secret"})
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		app.mu.RLock()
		count := len(app.document.History)
		app.mu.RUnlock()
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completed history was not persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	_, err = app.queue.Submit(jobs.Job{ID: "running-secret", Description: "password=running-secret", Run: func(context.Context, func(jobs.Update)) error { close(started); <-release; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	_, err = app.queue.Submit(jobs.Job{ID: "pending-secret", Description: "password=pending-secret", Run: func(context.Context, func(jobs.Update)) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.save(); err != nil {
		t.Fatal(err)
	}
	_, persisted, err := vault.Open(path, []byte(password))
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.History) != 1 {
		t.Fatalf("queue state leaked into history snapshot: %#v", persisted.History)
	}
	encoded := persisted.History[0].Operation + "\n" + persisted.History[0].Message
	if strings.Contains(encoded, "history secret") || strings.Contains(encoded, "message-secret") {
		t.Fatalf("persisted history contains credentials: %s", encoded)
	}
	if strings.Contains(encoded, "running-secret") || strings.Contains(encoded, "pending-secret") {
		t.Fatalf("running or pending queue state persisted: %s", encoded)
	}
}

func TestLocalListAndDirectoryDropTarget(t *testing.T) {
	directory := t.TempDir()
	leftDirectory := filepath.Join(directory, "left")
	rightDirectory := filepath.Join(directory, "right")
	if err := os.MkdirAll(filepath.Join(leftDirectory, "folder"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rightDirectory, "target"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftDirectory, "note.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	app := New(filepath.Join(directory, "test.vault"))
	defer app.Lock()
	if _, err := app.CreateVault("test hint", "test-password", "test-password"); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	app.panes[LeftPane] = &paneState{name: "本机", path: leftDirectory, endpoint: endpoint.NewLocal()}
	app.panes[RightPane] = &paneState{name: "本机", path: rightDirectory, endpoint: endpoint.NewLocal()}
	app.mu.Unlock()

	listing, err := app.List(LeftPane, "本机", leftDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Entries) != 2 || !listing.Entries[0].Directory {
		t.Fatalf("unexpected listing: %+v", listing.Entries)
	}
	preview, err := app.PrepareDrop(LeftPane, filepath.Join(leftDirectory, "note.txt"), RightPane, filepath.Join(rightDirectory, "target"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(rightDirectory, "target", "note.txt")
	if preview.TargetPath != want || preview.Conflict {
		t.Fatalf("preview=%+v want=%q", preview, want)
	}
	if err := os.WriteFile(want, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}
	preview, err = app.PrepareDrop(LeftPane, filepath.Join(leftDirectory, "note.txt"), RightPane, filepath.Join(rightDirectory, "target"))
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Conflict {
		t.Fatal("expected conflict")
	}
}

func TestTerminalChangeDirectoryUsesAbsoluteQuotedPath(t *testing.T) {
	local := endpoint.NewLocal()
	path, err := local.Abs(context.Background(), ".")
	if err != nil || path == "" {
		t.Fatalf("path=%q err=%v", path, err)
	}
	if got := shellQuote("/tmp/a'b"); got != `'/tmp/a'\''b'` {
		t.Fatalf("quote=%q", got)
	}
	command := terminalCDCommand("/tmp/a'b")
	if command != "cd -- '/tmp/a'\\''b'; printf '\\033[2K\\r'\n" {
		t.Fatalf("command=%q", command)
	}
}
