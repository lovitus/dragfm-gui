package transfer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

type reviewEndpoint struct {
	endpoint.Endpoint
	id       string
	truncate bool
}

func (e reviewEndpoint) Identity(context.Context) (endpoint.Identity, error) {
	return endpoint.Identity{MachineID: e.id}, nil
}
func (e reviewEndpoint) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	r, err := e.Endpoint.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	if e.truncate {
		return struct {
			io.Reader
			io.Closer
		}{io.LimitReader(r, 2), r}, nil
	}
	return r, nil
}

func TestReviewDirectoryCopyRejectsSymlinkParent(t *testing.T) {
	root := t.TempDir()
	source, target, outside := filepath.Join(root, "source"), filepath.Join(root, "target"), filepath.Join(root, "outside")
	for _, p := range []string{filepath.Join(source, "nested"), target, outside} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "file"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(target, "nested")); err != nil {
		t.Fatal(err)
	}
	local := endpoint.NewLocal()
	_, err := Run(context.Background(), Operation{Source: reviewEndpoint{Endpoint: local, id: "source"}, Destination: reviewEndpoint{Endpoint: local, id: "target"}, SourcePath: source, TargetPath: target, Move: true, Overwrite: true})
	if err == nil || Retryable(err) {
		t.Fatalf("unsafe merge was accepted/retryable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "file")); !os.IsNotExist(err) {
		t.Fatalf("outside target modified: %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source lost: %v", err)
	}
}

func TestReviewReadOnlyDirectoriesArePopulatedBeforeModesRestored(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	nested := filepath.Join(source, "nested")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "file"), []byte("data"), 0400); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{nested, source} {
		if err := os.Chmod(p, 0500); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, p := range []string{source, nested, target, filepath.Join(target, "nested")} {
			_ = os.Chmod(p, 0700)
		}
	})
	local := endpoint.NewLocal()
	_, err := Run(context.Background(), Operation{Source: reviewEndpoint{Endpoint: local, id: "source"}, Destination: reviewEndpoint{Endpoint: local, id: "target"}, SourcePath: source, TargetPath: target})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "nested", "file"))
	if err != nil || string(data) != "data" {
		t.Fatalf("data %q: %v", data, err)
	}
	for _, p := range []string{target, filepath.Join(target, "nested")} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != 0500 {
			t.Fatalf("mode not restored: %v %v", info, err)
		}
	}
}

func TestReviewShortCopyCannotReplaceDestination(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	local := endpoint.NewLocal()
	_, err := Run(context.Background(), Operation{Source: reviewEndpoint{Endpoint: local, id: "source", truncate: true}, Destination: reviewEndpoint{Endpoint: local, id: "target"}, SourcePath: source, TargetPath: target, Overwrite: true})
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("short copy not detected: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "original" {
		t.Fatalf("old target lost: %q %v", data, err)
	}
}
