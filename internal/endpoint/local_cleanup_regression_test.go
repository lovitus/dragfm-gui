//go:build !windows

package endpoint

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Real filesystem permission failure, not a replacement AtomicWriter. The
// old code reports only rename, silently discarding the failed unlink.
func TestLocalAtomicCommitReportsCleanupFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("fixture requires the unprivileged GitHub-hosted test user; root cannot supply permission-failure evidence")
	}
	parent := t.TempDir()
	target := filepath.Join(parent, "existing-directory")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	writer, err := NewLocal().CreateAtomic(context.Background(), target, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "not yet published"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(parent, 0700)
	err = writer.Commit()
	ops := make(map[string]bool)
	var visit func(error)
	visit = func(err error) {
		if err == nil {
			return
		}
		switch fileErr := err.(type) {
		case *os.PathError:
			ops[fileErr.Op] = true
		case *os.LinkError:
			ops[fileErr.Op] = true
		}
		switch wrapped := err.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range wrapped.Unwrap() {
				visit(child)
			}
		case interface{ Unwrap() error }:
			visit(wrapped.Unwrap())
		}
	}
	visit(err)
	if !ops["rename"] || !ops["remove"] {
		t.Fatalf("commit lost rename or cleanup failure: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 2 {
		t.Fatal("permission fixture did not leave exactly the original directory and retained partial", err)
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Fatal("failed commit damaged the original destination", err)
	}
}
