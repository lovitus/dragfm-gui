//go:build !windows

package agentservice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Real filesystem and kernel locks, using the pre-existing cleaner API. The
// baseline must fail on legacy live-directory preservation or unlocked-v2
// removal, not on a missing symbol. Hosted execution is still required.
func TestCleanupStaleTempsHonorsLiveDirectoryLease(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(map[int]string{1: "legacy-system-writer", 2: "leased-system-writer"}[version], func(t *testing.T) {
			root := t.TempDir()
			nonce := "42e4a69803b73f26b0e7a660a942c6c1"
			directory := filepath.Join(root, ".dragfm-"+nonce)
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(map[string]any{"version": version, "created": time.Now().Add(-48 * time.Hour), "nonce": nonce})
			if err := os.WriteFile(filepath.Join(directory, ".dragfm-owner-v1"), data, 0600); err != nil {
				t.Fatal(err)
			}
			payload := filepath.Join(directory, "archive.tar.gz")
			if err := os.WriteFile(payload, []byte("in-flight archive"), 0600); err != nil {
				t.Fatal(err)
			}
			if version == 1 {
				// A conclusively absent helper PID still says nothing about
				// orphaned descendants. The baseline incorrectly deletes this.
				if err := os.WriteFile(filepath.Join(directory, ".dragfm-agent-pid"), []byte("2147483647"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			lock, err := os.Open(directory)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := unix.Flock(int(lock.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			if err := cleanupOwnedTemps(root, "", 24*time.Hour); err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(payload); err != nil || string(data) != "in-flight archive" {
				t.Fatalf("live workspace was erased: %v", err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
			if err := cleanupOwnedTemps(root, "", 24*time.Hour); err != nil {
				t.Fatal(err)
			}
			_, err = os.Stat(directory)
			if version == 2 && !os.IsNotExist(err) {
				t.Fatalf("unlocked expired workspace was not removed: %v", err)
			}
			if version == 1 && err != nil {
				t.Fatalf("legacy workspace without exit evidence was erased: %v", err)
			}
		})
	}
}
