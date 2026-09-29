//go:build !windows

package endpoint

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// Real cat, SSH exit-status and channel-close exchange. The server observes
// the close acknowledgement before the client closes the file wrapper, so
// old EOF misclassification is deterministic rather than a timing assertion.
func TestPOSIXReadCloseKeepsRemoteExitResult(t *testing.T) {
	for _, exists := range []bool{true, false} {
		name := "missing"
		if exists {
			name = "complete"
		}
		t.Run(name, func(t *testing.T) {
			server, route := startIntegrationSSHServer(t, false)
			server.observeExecClose.Store(true)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			remote, err := DialSSH(ctx, "posix-reader", "", route)
			if err != nil {
				t.Fatal(err)
			}
			defer remote.Close()
			file := filepath.Join(t.TempDir(), "data")
			if exists {
				if err := os.WriteFile(file, []byte("complete remote contents\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			reader, err := remote.Open(ctx, file)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-server.execClosed:
			case <-ctx.Done():
				t.Fatal("fixture did not observe the real channel-close acknowledgement")
			}
			for attempt := 0; attempt < 2; attempt++ {
				closeErr := reader.Close()
				if exists {
					if closeErr != nil || string(data) != "complete remote contents\n" {
						t.Fatalf("successful POSIX read reported close failure: %v; data=%q", closeErr, data)
					}
				} else {
					var exit *ssh.ExitError
					if !errors.As(closeErr, &exit) || exit.ExitStatus() == 0 {
						t.Fatalf("real remote cat failure was lost on Close %d: %v", attempt+1, closeErr)
					}
				}
			}
			// A file's completed read must not invalidate other channels.
			if _, err := remote.Home(ctx); err != nil {
				t.Fatalf("file closure damaged browsing transport: %v", err)
			}
		})
	}
}
