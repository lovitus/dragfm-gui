//go:build !windows

package endpoint

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Use actual SSH authentication/subsystem acceptance and the real SFTP client.
// The server consumes INIT but never sends VERSION; cancellation, not an early
// protocol rejection, must release DialSSH/Fork. The fixture releases stalled
// channels during cleanup even when run against the unfixed product.
func TestSFTPInitializationHonorsCancellation(t *testing.T) {
	for _, operation := range []string{"dial", "fork"} {
		t.Run(operation, func(t *testing.T) {
			server, route := startIntegrationSSHServer(t, true)
			var original *Remote
			if operation == "fork" {
				var err error
				original, err = DialSSH(context.Background(), "existing-browser", "", route)
				if err != nil {
					t.Fatal(err)
				}
				defer original.Close()
			}
			server.stallSFTPInit.Store(true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				var remote *Remote
				var err error
				if original == nil {
					remote, err = DialSSH(ctx, "stalled-initialization", "", route)
				} else {
					remote, err = original.Fork(ctx)
				}
				if remote != nil {
					_ = remote.Close()
				}
				done <- err
			}()
			select {
			case <-server.sftpInitSeen:
				cancel()
			case err := <-done:
				t.Fatalf("fixture did not reach INIT/VERSION negotiation: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("fixture did not receive actual SFTP INIT")
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled SFTP initialization lost cancellation: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("SFTP initialization ignored cancellation after INIT")
			}
			if original != nil {
				if original.IsClosed() {
					t.Fatal("cancelling a task fork closed the browsing connection")
				}
				check, stop := context.WithTimeout(context.Background(), 2*time.Second)
				defer stop()
				if _, err := original.Stat(check, t.TempDir()); err != nil {
					t.Fatalf("original browsing connection is not usable: %v", err)
				}
			}
		})
	}
}
