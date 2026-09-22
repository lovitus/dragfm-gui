package transfer

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

type namedEndpoint struct {
	endpoint.Endpoint
	name string
}

func (e namedEndpoint) Identity(context.Context) (endpoint.Identity, error) {
	return endpoint.Identity{MachineID: e.name}, nil
}

type mutatingTarget struct {
	namedEndpoint
	onRead func()
}

func (e mutatingTarget) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	e.onRead()
	return e.Endpoint.Open(ctx, path)
}

func TestSourceManifestRejectsAddedAndDuplicateEntries(t *testing.T) {
	item := ManifestItem{Relative: "", Mode: fs.ModeDir | 0700}
	before := Manifest{Items: []ManifestItem{item}}
	for _, extra := range []ManifestItem{{Relative: "new", Mode: 0600}, item} {
		after := Manifest{Items: []ManifestItem{item, extra}}
		if err := CompareManifests(before, after, false); err == nil {
			t.Fatal("changed source was accepted")
		}
	}
	merged := Manifest{Items: []ManifestItem{item, {Relative: "old", Mode: 0600}}}
	if err := CompareManifests(before, merged, true); err != nil {
		t.Fatalf("destination directory merge rejected: %v", err)
	}
}

func TestControllerMoveRetainsNewSourceEntry(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "data"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	mutated := false
	op := Operation{Source: namedEndpoint{endpoint.NewLocal(), "source"}, Destination: namedEndpoint{endpoint.NewLocal(), "target"}, SourcePath: source, TargetPath: target, Move: true}
	op.Progress = func(p Progress) {
		if p.Stage != "copy" || p.BytesDone == 0 || mutated {
			return
		}
		mutated = true
		if err := os.WriteFile(filepath.Join(source, "new"), []byte("must survive"), 0600); err != nil {
			t.Fatal(err)
		}
		// Do not let a directory mtime check conceal the missing entry-set check.
		if err := os.Chtimes(source, original.ModTime(), original.ModTime()); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Run(context.Background(), op)
	if !mutated || !errors.Is(err, ErrSourceChanged) || !result.SourceKept {
		t.Fatalf("changed source not protected: %+v %v", result, err)
	}
	data, err := os.ReadFile(filepath.Join(source, "new"))
	if err != nil || string(data) != "must survive" {
		t.Fatalf("new source data lost: %q %v", data, err)
	}
}

func TestControllerMoveRechecksSourceAfterTargetHash(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	destination := mutatingTarget{namedEndpoint{endpoint.NewLocal(), "target"}, func() {
		if changed {
			return
		}
		changed = true
		if err := os.WriteFile(source, []byte("after!"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(source, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
	}}
	result, err := Run(context.Background(), Operation{Source: namedEndpoint{endpoint.NewLocal(), "source"}, Destination: destination, SourcePath: source, TargetPath: target, Move: true})
	if !changed || !errors.Is(err, ErrSourceChanged) || !result.SourceKept {
		t.Fatalf("source changed during target hashing: %+v %v", result, err)
	}
	data, readErr := os.ReadFile(source)
	if readErr != nil || string(data) != "after!" {
		t.Fatalf("changed source lost: %q %v", data, readErr)
	}
}

type failingSymlink struct{ endpoint.Endpoint }

func (failingSymlink) Symlink(context.Context, string, string) error { return fs.ErrPermission }
func TestFailedSymlinkMergeKeepsOldDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	err := copyItem(context.Background(), Operation{Destination: failingSymlink{endpoint.NewLocal()}, Overwrite: true}, ManifestItem{Mode: fs.ModeSymlink, LinkTarget: "new"}, path, &Progress{})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err=%v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep" {
		t.Fatalf("old destination destroyed: %q %v", data, err)
	}
}

type failingMetadata struct{ namedEndpoint }

func (failingMetadata) Chtimes(context.Context, string, time.Time, time.Time) error {
	return fs.ErrPermission
}
func TestControllerCopyPropagatesMetadataFailure(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Operation{Source: namedEndpoint{endpoint.NewLocal(), "source"}, Destination: failingMetadata{namedEndpoint{endpoint.NewLocal(), "target"}}, SourcePath: source, TargetPath: target})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("metadata failure hidden: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("uncommitted root remained: %v", err)
	}
}
