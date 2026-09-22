//go:build !windows

package endpoint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSFTPReplacementFailurePreservesOldDirectory(t *testing.T) {
	_, route := startIntegrationSSHServer(t, true)
	remote, err := DialSSH(context.Background(), "fixture", route.Hops[0].HostKey.PinnedSHA256, route)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	for _, method := range []string{"rename", "atomic-writer"} {
		t.Run(method, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "old-directory")
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			if method == "rename" {
				source := filepath.Join(root, "source")
				if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
				err = remote.Rename(context.Background(), source, target, true)
			} else {
				writer, createErr := remote.CreateAtomic(context.Background(), target, 0600)
				if createErr != nil {
					t.Fatal(createErr)
				}
				if _, err := writer.Write([]byte("new")); err != nil {
					t.Fatal(err)
				}
				err = writer.Commit()
				_ = writer.Abort()
			}
			if err == nil {
				t.Fatal("incompatible replacement should fail without deleting the directory")
			}
			info, statErr := os.Stat(target)
			if statErr != nil || !info.IsDir() {
				t.Fatalf("old directory was removed: %v", statErr)
			}
			leftovers, err := filepath.Glob(filepath.Join(root, ".dragfm-partial-*"))
			if err != nil || len(leftovers) > 0 {
				t.Fatalf("partial files remained: %v %v", leftovers, err)
			}
		})
	}
}

func TestCancelledLocalMutationsPreserveFiles(t *testing.T) {
	local := NewLocal()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := local.Remove(ctx, source, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled remove=%v", err)
	}
	if err := local.Rename(ctx, source, filepath.Join(root, "target"), true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled rename=%v", err)
	}
	data, err := os.ReadFile(source)
	if err != nil || string(data) != "keep" {
		t.Fatalf("source lost: %q %v", data, err)
	}
}
