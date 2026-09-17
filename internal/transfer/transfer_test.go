package transfer

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

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

func TestCopyDirectoryMergeAndMoveVerification(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0750); err != nil {
		t.Fatal(err)
	}
	wantedDirectoryTime := time.Unix(1_600_000_000, 0)
	if err := os.WriteFile(filepath.Join(source, "nested", "data"), []byte("verified"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(source, "nested"), wantedDirectoryTime, wantedDirectoryTime); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "existing"), []byte("kept"), 0600); err != nil {
		t.Fatal(err)
	}
	local := endpoint.NewLocal()
	result, err := Run(context.Background(), Operation{Source: local, Destination: local, SourcePath: source, TargetPath: target, Move: true, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Moved || result.Verification != "sha256" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := os.Stat(source); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("source still exists: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(target, "nested", "data"))
	if err != nil || string(data) != "verified" {
		t.Fatalf("copied=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(target, "existing")); err != nil {
		t.Fatalf("merge removed existing target: %v", err)
	}
	nestedInfo, err := os.Stat(filepath.Join(target, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if nestedInfo.Mode().Perm() != 0750 || nestedInfo.ModTime().Unix() != wantedDirectoryTime.Unix() {
		t.Fatalf("directory metadata not preserved: mode=%v mtime=%v err=%v", nestedInfo.Mode().Perm(), nestedInfo.ModTime(), err)
	}
}

func TestConflictDoesNotOverwrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Operation{Source: endpoint.NewLocal(), Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target})
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestSameMachineMoveUsesRename(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("native"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Operation{Source: endpoint.NewLocal(), Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target, Move: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verification != "same-machine-rename" || !result.Moved {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestSameMachineCopyUsesCP(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("native-copy"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Operation{Source: endpoint.NewLocal(), Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target})
	if err != nil {
		t.Fatal(err)
	}
	if result.Verification != "same-machine-cp" || !result.Copied || result.Moved {
		t.Fatalf("unexpected result: %#v", result)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "native-copy" {
		t.Fatalf("target=%q err=%v", data, err)
	}
}

func TestCancelledCopyLeavesNoPartial(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(source, make([]byte, 1024*1024), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, Operation{Source: endpoint.NewLocal(), Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("target should not exist: %v", err)
	}
}

type cancellingEndpoint struct {
	endpoint.Endpoint
	cancel context.CancelFunc
}

func (e cancellingEndpoint) Open(ctx context.Context, target string) (io.ReadCloser, error) {
	reader, err := e.Endpoint.Open(ctx, target)
	if err != nil {
		return nil, err
	}
	return &cancelAfterFirstRead{ReadCloser: reader, cancel: e.cancel}, nil
}

type cancelAfterFirstRead struct {
	io.ReadCloser
	cancel context.CancelFunc
	done   bool
}

func (r *cancelAfterFirstRead) Read(buffer []byte) (int, error) {
	count, err := r.ReadCloser.Read(buffer)
	if !r.done && count > 0 {
		r.done = true
		r.cancel()
	}
	return count, err
}

func TestCancelledControllerDirectoryRelayRemovesStagingRoot(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "large"), make([]byte, 1024*1024), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	sourceEndpoint := cancellingEndpoint{Endpoint: endpoint.NewLocal(), cancel: cancel}
	_, err := Run(ctx, Operation{Source: sourceEndpoint, Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("final target exists after cancellation: %v", err)
	}
	matches, err := filepath.Glob(target + ".dragfm-partial-*")
	if err != nil || len(matches) != 0 {
		t.Fatalf("staging roots remain: %v, %v", matches, err)
	}
}

func TestCompareManifestsDetectsRootReplacementWithSameContentMetadata(t *testing.T) {
	item := ManifestItem{Relative: "", Mode: 0600, Size: 4, ModifiedNS: 123, SHA256: "same"}
	expected := Manifest{Items: []ManifestItem{item}, RootDevice: 1, RootInode: 10}
	actual := Manifest{Items: []ManifestItem{item}, RootDevice: 1, RootInode: 11}
	if err := CompareManifests(expected, actual, false); err == nil {
		t.Fatal("source inode replacement was accepted")
	}
	if err := CompareManifests(expected, actual, true); err != nil {
		t.Fatalf("target comparison must not require the source inode: %v", err)
	}
}

func TestAtomicDirectoryMergeKeepsContentsWithoutExtraSourceLayer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native cp is POSIX-only")
	}
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "new"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "old"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), Operation{Source: endpoint.NewLocal(), Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target, Overwrite: true})
	if err != nil || result.Verification != "atomic-write" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	for _, name := range []string{"new", "old"} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Fatalf("merged file %s missing: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "source")); !os.IsNotExist(err) {
		t.Fatalf("copy created an extra source directory: %v", err)
	}
}
