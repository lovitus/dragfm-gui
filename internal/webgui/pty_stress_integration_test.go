//go:build integration && !windows

package webgui

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/vault"
)

func TestHostedReviewedPTYEditingAndPromptStress(t *testing.T) {
	source, _, document := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root := remoteTempDir(t, ctx, source)
	defer source.Remove(context.Background(), root, true)
	directories := []string{source.Join(root, "space directory"), source.Join(root, "quote ' [brackets] $literal")}
	for _, directory := range directories {
		if err := source.MkdirAll(ctx, directory, 0700); err != nil { t.Fatal(err) }
	}
	app := New(filepath.Join(t.TempDir(), vault.FileName))
	defer app.Lock()
	const password = "disposable-pty-master-password"
	if _, err := app.CreateVault("PTY regression fixture", password, password); err != nil { t.Fatal(err) }
	prompts := make(chan terminalCWDModel, 128)
	var outputMu sync.Mutex
	var output strings.Builder
	app.mu.Lock()
	app.document.Keys, app.document.Hosts = document.Keys, document.Hosts
	app.eventSink = func(name string, value any) {
		switch name {
		case "terminal:cwd":
			select { case prompts <- value.(terminalCWDModel): default: t.Error("unexpected prompt flood") }
		case "terminal:data":
			data, err := base64.StdEncoding.DecodeString(value.(terminalDataModel).Data)
			if err != nil { t.Error(err); return }
			outputMu.Lock()
			if output.Len() < 128<<10 { _, _ = output.Write(data) }
			outputMu.Unlock()
		}
	}
	app.mu.Unlock()
	if _, err := app.ChangeEndpoint(LeftPane, source.Name(), "本机"); err != nil { t.Fatal(err) }
	if _, err := app.List(LeftPane, source.Name(), root); err != nil { t.Fatal(err) }
	id, err := app.StartTerminal(LeftPane, root, 24, 100)
	if err != nil { t.Fatal(err) }
	if err := app.TerminalReady(id); err != nil { t.Fatal(err) }
	var sequence uint64
	prompt := func(expected string) {
		t.Helper()
		timer := time.NewTimer(5*time.Second)
		defer timer.Stop()
		for {
			select {
			case event := <-prompts:
				if event.Session == id && event.Sequence > sequence && event.Path == expected { sequence = event.Sequence; return }
			case <-timer.C:
				t.Fatalf("no fresh prompt for %q after sequence %d", expected, sequence)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
	}
	prompt(root)
	for iteration := 0; iteration < 20; iteration++ {
		directory := directories[iteration%len(directories)]
		if err := app.TerminalChangeDirectory(id, directory); err != nil { t.Fatalf("cd %d: %v", iteration, err) }
		prompt(directory)
		name := fmt.Sprintf("cycle-%02d", iteration)
		command := "printf '%s\\n' " + shellQuote(name) + "; : > " + shellQuote(source.Join(directory, name)) + "\r"
		if err := app.TerminalInput(id, command); err != nil { t.Fatal(err) }
		prompt(directory)
		listing, err := app.List(LeftPane, source.Name(), directory)
		if err != nil { t.Fatal(err) }
		found := false
		for _, entry := range listing.Entries { found = found || entry.Name == name }
		if !found { t.Fatalf("command %d did not update the browsable filesystem", iteration) }
		if err := app.TerminalResize(id, 24+iteration%4, 80+iteration); err != nil { t.Fatal(err) }
	}
	current := directories[1]
	unwanted := source.Join(root, "unsubmitted-command-must-not-run")
	if err := app.TerminalInput(id, ": > "+shellQuote(unwanted)); err != nil { t.Fatal(err) }
	if err := app.TerminalChangeDirectory(id, root); err == nil { t.Fatal("navigation injected into edited input") }
	if err := app.TerminalInput(id, "\x15printf '__edited_line_preserved__\\n'\r"); err != nil { t.Fatal(err) }
	prompt(current)
	if _, err := source.Stat(ctx, unwanted); !os.IsNotExist(err) && err != fs.ErrNotExist { t.Fatalf("unsubmitted input executed: %v", err) }
	if err := app.TerminalInput(id, "sleep 30\r"); err != nil { t.Fatal(err) }
	if err := app.TerminalChangeDirectory(id, root); err == nil { t.Fatal("navigation injected into running program") }
	time.Sleep(100*time.Millisecond)
	if err := app.TerminalInput(id, "\x03"); err != nil { t.Fatal(err) }
	prompt(current)
	app.CloseTerminal(id)
	if _, err := app.terminal(id); err == nil { t.Fatal("closed PTY remained accessible") }
	outputMu.Lock()
	text := output.String()
	outputMu.Unlock()
	if strings.Contains(text, "\x1b]777;dragfm-cwd=") || strings.Contains(text, "__dragfm_emit_cwd") { t.Fatal("internal prompt protocol leaked to visible terminal output") }
	t.Log("20 real SSH PTY cd/command/list/resize cycles; quoted paths; editing and running-program cd refusal; Ctrl-U/Ctrl-C preserve shell semantics; prompt protocol filtered")
}
