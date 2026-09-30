//go:build !windows

package endpoint

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestOwnedSSHCloseDoesNotReportExpectedTransportEOF(t *testing.T) {
	_, route := startIntegrationSSHServer(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	browser, err := DialSSH(ctx, "browser", "", route)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	directory := t.TempDir()
	for _, preclosed := range []bool{false, true} {
		owned, err := browser.Fork(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owned.List(ctx, directory); err != nil {
			t.Fatal(err)
		}
		if preclosed {
			_ = owned.SSHClient().Close()
		}
		if err := owned.Close(); err != nil {
			t.Errorf("ordinary transport closure was reported as cleanup failure (preclosed=%t): %v", preclosed, err)
		}
		if err := owned.Close(); err != nil {
			t.Errorf("repeated Close retained a spurious transport failure: %v", err)
		}
		if _, err := browser.List(ctx, directory); err != nil {
			t.Fatalf("closing task transport disturbed browser: %v", err)
		}
	}
}

func TestPOSIXMissingAncestorsRemainCreatable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("remote POSIX fallback targets GNU/Linux")
	}
	_, route := startIntegrationSSHServer(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	remote, err := DialSSH(ctx, "no-sftp", "", route)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	if remote.SFTPError() == nil {
		t.Fatal("fixture did not reject the SFTP subsystem")
	}
	root := t.TempDir()
	directory := filepath.Join(root, "missing ' ancestor", "nested", "leaf")
	if _, err := remote.Stat(ctx, directory); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing multi-level path was not ENOENT: %v", err)
	}
	if err := remote.MkdirAll(ctx, directory, 0700); err != nil {
		t.Fatalf("cannot create missing multi-level path: %v", err)
	}
	entry, err := remote.Stat(ctx, directory)
	if err != nil || !entry.IsDir() {
		t.Fatalf("directory creation did not become visible: %+v %v", entry, err)
	}
	file := filepath.Join(directory, "ordinary-file")
	writer, err := remote.CreateAtomic(ctx, file, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Abort()
	if _, err := io.WriteString(writer, "real SSH POSIX content"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(file); err != nil || string(content) != "real SSH POSIX content" {
		t.Fatalf("created file has incorrect contents: %q %v", content, err)
	}
	if _, err := remote.Stat(ctx, filepath.Join(file, "not-a-directory", "leaf")); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("non-directory ancestor incorrectly reported as a creatable missing path: %v", err)
	}
	if os.Geteuid() != 0 {
		// Real inaccessible ancestor, not a manufactured permission error.
		if err := os.Chmod(directory, 0000); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(directory, 0700)
		if _, err := remote.Stat(ctx, filepath.Join(directory, "hidden", "leaf")); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("inaccessible ancestor lost permission error: %v", err)
		}
	}
}
