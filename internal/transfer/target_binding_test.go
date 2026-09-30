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

// The real target filesystem performs the copy/hash/fsync. Only the external
// namespace change is scheduled at the synchronization boundary, making a
// deterministic security regression instead of relying on a racing goroutine.
type rebindingTarget struct {
	namedEndpoint
	alias, replacement string
	change             bool
	observed           bool
}

type finalHashMutationSource struct {
	namedEndpoint
	synced  *bool
	mutate  func() error
	changed bool
}

func (s *finalHashMutationSource) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	if *s.synced && !s.changed {
		s.changed = true
		if err := s.mutate(); err != nil {
			return nil, err
		}
	}
	return s.Endpoint.Open(ctx, path)
}

func TestMoveKeepsSourceWhenVerifiedTargetChildIsRebound(t *testing.T) {
	for _, child := range []string{"unchanged", "directory", "file"} {
		t.Run(child, func(t *testing.T) {
			root := t.TempDir()
			from, to := filepath.Join(root, "source"), filepath.Join(root, "target")
			if err := os.MkdirAll(filepath.Join(from, "part"), 0750); err != nil {
				t.Fatal(err)
			}
			const contents = "a verified root does not pin a newly copied child"
			if err := os.WriteFile(filepath.Join(from, "part", "data"), []byte(contents), 0640); err != nil {
				t.Fatal(err)
			}
			local := endpoint.NewLocal()
			target := &rebindingTarget{namedEndpoint: namedEndpoint{local, "target"}}
			retained := filepath.Join(root, "retained-copy")
			source := &finalHashMutationSource{namedEndpoint: namedEndpoint{local, "source"}, synced: &target.observed}
			source.mutate = func() error {
				if child == "unchanged" {
					return nil
				}
				relative := "part"
				if child == "file" {
					relative = filepath.Join(relative, "data")
				}
				// A normal owner changes only a child entry, after target fsync,
				// during the final source hash. The target root stays in place.
				if err := os.Rename(filepath.Join(to, relative), retained); err != nil {
					return err
				}
				return os.Symlink(filepath.Join(from, relative), filepath.Join(to, relative))
			}
			result, err := Run(context.Background(), Operation{Source: source, Destination: target, SourcePath: from, TargetPath: to, Move: true})
			if !source.changed {
				t.Fatalf("move never reached source hash after target sync: %+v %v", result, err)
			}
			if child == "unchanged" {
				if err != nil || !result.Moved {
					t.Fatalf("unchanged subtree did not move: %+v %v", result, err)
				}
				if _, err := os.Stat(from); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("verified source was not removed: %v", err)
				}
			} else {
				if err == nil || Retryable(err) || !result.SourceKept {
					t.Fatalf("target child rebind did not preserve source: %+v %v", result, err)
				}
				if data, err := os.ReadFile(filepath.Join(from, "part", "data")); err != nil || string(data) != contents {
					t.Fatalf("source changed after target child replacement: %q %v", data, err)
				}
				if child == "directory" {
					retained = filepath.Join(retained, "data")
				}
				if data, err := os.ReadFile(retained); err != nil || string(data) != contents {
					t.Fatalf("actual copied data changed: %q %v", data, err)
				}
			}
			if data, err := os.ReadFile(filepath.Join(to, "part", "data")); err != nil || string(data) != contents {
				t.Fatalf("user-selected target lost its contents: %q %v", data, err)
			}
		})
	}
}

func (e *rebindingTarget) PhysicalPath(ctx context.Context, path string) (string, error) {
	return e.Endpoint.(interface {
		PhysicalPath(context.Context, string) (string, error)
	}).PhysicalPath(ctx, path)
}

func (e *rebindingTarget) SyncPaths(ctx context.Context, paths []string) error {
	e.observed = true
	if e.change {
		link := e.alias + "-replacement"
		if err := os.Symlink(e.replacement, link); err != nil {
			return err
		}
		if err := os.Rename(link, e.alias); err != nil {
			return err
		}
	}
	return e.namedEndpoint.SyncPaths(ctx, paths)
}

func TestMoveKeepsSourceWhenVerifiedTargetIsRebound(t *testing.T) {
	for _, change := range []bool{false, true} {
		name := "unchanged"
		if change {
			name = "parent-link-switched-to-source"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			sourceDir, destinationDir := filepath.Join(root, "source"), filepath.Join(root, "destination")
			for _, dir := range []string{sourceDir, destinationDir} {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			alias := filepath.Join(root, "alias")
			if err := os.Symlink(destinationDir, alias); err != nil {
				t.Fatal(err)
			}
			from, to := filepath.Join(sourceDir, "data"), filepath.Join(alias, "data")
			const contents = "the user-selected target must still name the verified copy"
			if err := os.WriteFile(from, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			local := endpoint.NewLocal()
			target := &rebindingTarget{namedEndpoint: namedEndpoint{local, "target"}, alias: alias, replacement: sourceDir, change: change}
			result, err := Run(context.Background(), Operation{Source: namedEndpoint{local, "source"}, Destination: target, SourcePath: from, TargetPath: to, Move: true})
			if !target.observed {
				t.Fatalf("copy never reached final synchronization: %+v %v", result, err)
			}
			if change {
				if err == nil || Retryable(err) || !result.SourceKept {
					t.Fatalf("target rebind did not preserve source: %+v %v", result, err)
				}
				for _, path := range []string{from, to} {
					if data, err := os.ReadFile(path); err != nil || string(data) != contents {
						t.Fatalf("source or requested target was lost: %q %v", data, err)
					}
				}
			} else {
				if err != nil || !result.Moved {
					t.Fatalf("unchanged target did not move: %+v %v", result, err)
				}
				if _, err := os.Stat(from); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("verified source was not removed: %v", err)
				}
			}
			if data, err := os.ReadFile(filepath.Join(destinationDir, "data")); err != nil || string(data) != contents {
				t.Fatalf("actual copied destination changed: %q %v", data, err)
			}
		})
	}
}
