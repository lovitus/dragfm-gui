package transfer

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

type brokenNativeSource struct{ *endpoint.Local }

func (brokenNativeSource) CopyNative(context.Context, string, string, bool, bool) error {
	return errors.New("copy failed")
}
func (brokenNativeSource) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("read failed")
}

type deniedRename struct{ endpoint.Endpoint }

func (deniedRename) Rename(context.Context, string, string, bool) error { return fs.ErrPermission }

func TestNativeCopyFailurePreservesOldDestination(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Operation{Source: brokenNativeSource{endpoint.NewLocal()}, Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target, Overwrite: true})
	if err == nil {
		t.Fatal("expected source failure")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "old" {
		t.Fatalf("old destination lost: %q %v", data, err)
	}
}

func TestRenameFailureNeverDeletesOldDestination(t *testing.T) {
	root := t.TempDir()
	target, staged := filepath.Join(root, "target"), filepath.Join(root, "partial")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	err := commitStagedRoot(context.Background(), Operation{Destination: deniedRename{endpoint.NewLocal()}, TargetPath: target, Overwrite: true}, staged, ManifestItem{})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err=%v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "old" {
		t.Fatalf("destination changed: %q %v", data, err)
	}
}

func TestSelfCopyAndNestedDestinationAreRejectedWithoutChanges(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(source, "file")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{source, filepath.Join(source, "nested")} {
		op := Operation{Source: endpoint.NewLocal(), Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target, Overwrite: true}
		if _, err := Run(context.Background(), op); err == nil {
			t.Fatalf("unsafe target accepted: %s", target)
		}
		if _, err := Preflight(context.Background(), op); err == nil {
			t.Fatalf("unsafe preflight accepted: %s", target)
		}
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "keep" {
		t.Fatalf("source changed: %q %v", data, err)
	}
}
