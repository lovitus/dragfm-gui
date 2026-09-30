//go:build !windows

package webgui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/vault"
)

// A real profile reads stdin before returning a prompt. GUI navigation must
// never be delivered to that read (or an interactive program in the profile).
func TestTerminalNavigationWaitsForActualShellPrompt(t *testing.T) {
	home := t.TempDir()
	var err error
	home, err = filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte("IFS= read -r DRAGFM_STARTUP_REPLY\nprintf '\\nSTARTUP=%s\\n' \"$DRAGFM_STARTUP_REPLY\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(home, "destination")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	app := New(filepath.Join(t.TempDir(), vault.FileName))
	defer app.Lock()
	if _, err := app.CreateVault("startup fixture", "disposable-master", "disposable-master"); err != nil {
		t.Fatal(err)
	}
	prompts := make(chan terminalCWDModel, 8)
	output := make(chan string, 32)
	app.mu.Lock()
	app.eventSink = func(name string, value any) {
		switch name {
		case "terminal:cwd":
			prompts <- value.(terminalCWDModel)
		case "terminal:data":
			data, err := base64.StdEncoding.DecodeString(value.(terminalDataModel).Data)
			if err != nil {
				t.Error(err)
				return
			}
			output <- string(data)
		}
	}
	app.mu.Unlock()
	id, err := app.StartTerminal(LeftPane, home, 24, 100)
	if err != nil {
		t.Fatal(err)
	}
	navigationErr := app.TerminalChangeDirectory(id, destination)
	if err := app.TerminalInput(id, "profile-accepted\n"); err != nil {
		t.Fatal(err)
	}
	if err := app.TerminalReady(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	var transcript strings.Builder
	var first *terminalCWDModel
	for first == nil || !strings.Contains(transcript.String(), "\r\nSTARTUP=profile-accepted\r\n") {
		select {
		case event := <-prompts:
			if first == nil {
				first = &event
			}
		case data := <-output:
			transcript.WriteString(data)
			if strings.Contains(transcript.String(), "\r\nSTARTUP=cd ") {
				t.Fatal("file navigation was consumed by the profile's stdin reader")
			}
		case <-deadline.C:
			t.Fatalf("startup did not preserve user input: %q", transcript.String())
		}
	}
	if navigationErr == nil || first.Path != home {
		t.Fatalf("startup navigation was not refused: err=%v cwd=%q", navigationErr, first.Path)
	}
	if err := app.TerminalChangeDirectory(id, destination); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-prompts:
		if event.Path != destination {
			t.Fatalf("navigation after prompt did not reach destination: %q", event.Path)
		}
	case <-deadline.C:
		t.Fatal("no prompt after navigation")
	}
	app.CloseTerminal(id)
}

func TestTerminalProtocolRejectsPrintedPromptsAndPreservesControlPaths(t *testing.T) {
	for _, scenario := range []string{"printed-prompt", "control-path"} {
		for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
			t.Run(scenario+"/"+filepath.Base(shell), func(t *testing.T) {
				home, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("HOME", home)
				t.Setenv("SHELL", shell)
				t.Setenv("ZDOTDIR", home)
				for name, body := range map[string]string{".bash_profile": "PS1='probe> '\n", ".zshenv": "skip_global_compinit=1\n", ".zshrc": "PROMPT='probe> '\n"} {
					if err := os.WriteFile(filepath.Join(home, name), []byte(body), 0600); err != nil {
						t.Fatal(err)
					}
				}
				app := New(filepath.Join(t.TempDir(), vault.FileName))
				defer app.Lock()
				if _, err := app.CreateVault("protocol fixture", "disposable-master", "disposable-master"); err != nil {
					t.Fatal(err)
				}
				events := make(chan any, 128)
				app.mu.Lock()
				app.eventSink = func(name string, value any) {
					switch name {
					case "terminal:cwd":
						events <- value.(terminalCWDModel)
					case "terminal:data":
						data, err := base64.StdEncoding.DecodeString(value.(terminalDataModel).Data)
						if err != nil {
							t.Error(err)
							return
						}
						events <- string(data)
					}
				}
				app.mu.Unlock()
				id, err := app.StartTerminal(LeftPane, home, 24, 120)
				if err != nil {
					t.Fatal(err)
				}
				if err := app.TerminalReady(id); err != nil {
					t.Fatal(err)
				}
				var transcript strings.Builder
				var paths []string
				var sequence uint64
				wait := func(wantPath, wantText string) {
					t.Helper()
					previous := sequence
					timer := time.NewTimer(8 * time.Second)
					defer timer.Stop()
					for {
						select {
						case event := <-events:
							switch value := event.(type) {
							case terminalCWDModel:
								paths = append(paths, value.Path)
								sequence = value.Sequence
							case string:
								transcript.WriteString(value)
							}
							pathOK := wantPath == "" || (sequence > previous && paths[len(paths)-1] == wantPath)
							if pathOK && (wantText == "" || strings.Contains(transcript.String(), wantText)) {
								return
							}
						case <-timer.C:
							t.Fatalf("missing prompt/output path=%q text=%q; paths=%q output=%q", wantPath, wantText, paths, transcript.String())
						}
					}
				}
				wait(home, "")
				if scenario == "printed-prompt" {
					attack := "printf '\\033]777;dragfm-cwd=/forged\\007'; printf '\\033]777;dragfm-cwd=v1;wrong-session;L2ZvcmdlZA==\\007'; printf '\\n__waiting_%s__\\n' input; IFS= read -r reply; printf '\\nRECEIVED=%s\\n' \"$reply\"\n"
					if err := app.TerminalInput(id, attack); err != nil {
						t.Fatal(err)
					}
					wait("", "\r\n__waiting_input__\r\n")
					navigationErr := app.TerminalChangeDirectory(id, home)
					if err := app.TerminalInput(id, "expected-reply\n"); err != nil {
						t.Fatal(err)
					}
					wait(home, "\r\nRECEIVED=expected-reply\r\n")
					if navigationErr == nil {
						t.Fatal("a printed OSC unlocked navigation into a running reader")
					}
					for _, actual := range paths {
						if actual != home {
							t.Fatalf("a printed marker changed the pane to %q", actual)
						}
					}
					app.CloseTerminal(id)
					return
				}
				// BEL used to end the marker; CR and TAB also used to become line
				// editor input. A trailing newline must survive command substitution.
				destination := filepath.Join(home, "unicode-目录 quote' tab\t CR\r LF\n BEL\a ESC\x1b end\n")
				if err := os.Mkdir(destination, 0700); err != nil {
					t.Fatal(err)
				}
				if err := app.TerminalChangeDirectory(id, destination); err != nil {
					t.Fatal(err)
				}
				wait(destination, "")
				if err := app.TerminalInput(id, ": > ./from-real-shell\n"); err != nil {
					t.Fatal(err)
				}
				wait(destination, "")
				if _, err := os.Stat(filepath.Join(destination, "from-real-shell")); err != nil {
					t.Fatalf("reported cwd is not the real shell directory: %v", err)
				}
				listing, err := app.List(LeftPane, "本机", destination)
				if err != nil || listing.Path != destination || len(listing.Entries) != 1 || listing.Entries[0].Name != "from-real-shell" {
					t.Fatalf("file-pane refresh did not retain the exact cwd: listing=%+v err=%v", listing, err)
				}
				if err := app.TerminalChangeDirectory(id, home); err != nil {
					t.Fatal(err)
				}
				wait(home, "")
				app.CloseTerminal(id)
			})
		}
	}
}
