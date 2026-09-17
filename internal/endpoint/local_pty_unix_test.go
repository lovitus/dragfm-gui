//go:build !windows

package endpoint

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalPTYEmitsCWDMarker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := NewLocal().OpenPTY(ctx, t.TempDir(), "/bin/bash", 24, 80)
	if err != nil {
		t.Skipf("local PTY unavailable: %v", err)
	}
	defer session.Close()
	result := make(chan []byte, 1)
	go func() {
		buffer := make([]byte, 8192)
		var output []byte
		for len(output) < 64*1024 {
			count, readErr := session.Output().Read(buffer)
			output = append(output, buffer[:count]...)
			if bytes.Contains(output, []byte("\x1b]777;dragfm-cwd=")) || readErr != nil {
				break
			}
		}
		result <- output
	}()
	select {
	case output := <-result:
		if !bytes.Contains(output, []byte("\x1b]777;dragfm-cwd=")) {
			t.Fatalf("cwd marker missing from %q", output)
		}
		if bytes.Contains(output, []byte("__dragfm_emit_cwd")) {
			t.Fatalf("cwd bootstrap leaked into terminal: %q", output)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestLocalPTYLoadsLoginAndInteractiveEnvironmentWithoutBootstrapLeak(t *testing.T) {
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		if _, err := os.Stat(shell); err != nil {
			continue
		}
		t.Run(filepath.Base(shell), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			for name, body := range map[string]string{
				".bash_profile": "export DRAGFM_LOGIN_ENV=loaded\n",
				".bashrc":       "PS1='dragfm-test> '\n",
				".zprofile":     "export DRAGFM_LOGIN_ENV=loaded\n",
				".zshrc":        "PROMPT='dragfm-test> '\n",
			} {
				if err := os.WriteFile(filepath.Join(home, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := NewLocal().OpenPTY(ctx, home, shell, 24, 100)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			markerSeen := make(chan struct{}, 1)
			result := make(chan []byte, 1)
			go func() {
				buffer := make([]byte, 4096)
				var output []byte
				notified := false
				for len(output) < 128*1024 {
					count, readErr := session.Output().Read(buffer)
					output = append(output, buffer[:count]...)
					if !notified && bytes.Contains(output, []byte("\x1b]777;dragfm-cwd=")) {
						notified = true
						markerSeen <- struct{}{}
					}
					if bytes.Contains(output, []byte("LOGIN=loaded")) || readErr != nil {
						break
					}
				}
				result <- output
			}()
			select {
			case <-markerSeen:
				_, _ = session.Input().Write([]byte("printf 'LOGIN=%s\\n' \"$DRAGFM_LOGIN_ENV\"\n"))
			case <-ctx.Done():
				t.Fatal("cwd hook did not start")
			}
			select {
			case output := <-result:
				if !bytes.Contains(output, []byte("LOGIN=loaded")) {
					t.Fatalf("login environment missing from %q", output)
				}
				if bytes.Contains(output, []byte("__dragfm_emit_cwd")) {
					t.Fatalf("cwd bootstrap leaked into terminal: %q", output)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}
