//go:build integration && !windows

package webgui

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

func TestHostedIndependentSSHFilesystemsCanMoveSameAbsolutePath(t *testing.T) {
	source, target, _ := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root := remoteTempDir(t, ctx, source)
	if _, err := target.Stat(ctx, root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("unique source fixture path must not exist in the independent target")
	}
	if err := target.MkdirAll(ctx, root, 0750); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, ep := range []*endpoint.Remote{source, target} {
			if _, err := ep.Stat(cleanup, root); err == nil {
				if err := ep.Remove(cleanup, root, true); err != nil {
					t.Errorf("scoped fixture cleanup: %v", err)
				}
			}
		}
	})
	const contents = "same spelling, independent filesystems"
	writeRemoteFile(t, ctx, source, source.Join(root, "data"), contents)
	writeRemoteFile(t, ctx, target, target.Join(root, "unrelated"), "keep existing destination")
	result, err := transfer.Run(ctx, transfer.Operation{Source: source, Destination: target, SourcePath: root, TargetPath: root, Move: true, Overwrite: true})
	if err != nil || !result.Moved {
		t.Fatalf("independent same-path move rejected: %+v %v", result, err)
	}
	assertRemoteFile(t, ctx, target, target.Join(root, "data"), contents)
	assertRemoteFile(t, ctx, target, target.Join(root, "unrelated"), "keep existing destination")
	if _, err := source.Stat(ctx, root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("verified independent source was not removed: %v", err)
	}
}

// These are ordinary-account operations, not sudo transfers. Fixture setup
// changes only its disposable roots; the transfer itself gets no elevated view.
func TestHostedMoveUsesOnlyNecessaryDirectoryPermissions(t *testing.T) {
	for _, mode := range []string{"shared-parent", "readonly-empty", "search-only-ancestor"} {
		t.Run(mode, func(t *testing.T) {
			source, target, _ := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			for _, ep := range []*endpoint.Remote{source, target} {
				if err := ep.Exec(ctx, `test "$(id -u)" -ne 0`, endpoint.ExecOptions{}); err != nil {
					t.Fatal("fixture must use real non-root SSH accounts", err)
				}
			}
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			opaque := target.Join(targetRoot, "opaque")
			t.Cleanup(func() {
				cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
				defer stop()
				if mode == "shared-parent" {
					if err := target.Exec(cleanup, `sudo -n chown "$(id -u):$(id -g)" -- `+shellQuote(targetRoot), endpoint.ExecOptions{}); err != nil {
						t.Error("scoped shared-directory fixture cleanup", err)
					}
				}
				if mode == "search-only-ancestor" {
					if err := target.Exec(cleanup, `sudo -n chown "$(id -u):$(id -g)" -- `+shellQuote(opaque)+" && chmod 0700 -- "+shellQuote(opaque), endpoint.ExecOptions{}); err != nil {
						t.Error("scoped ancestor fixture cleanup", err)
					}
				}
				for _, item := range []struct {
					ep   *endpoint.Remote
					path string
				}{{source, sourceRoot}, {target, targetRoot}} {
					if err := item.ep.Remove(cleanup, item.path, true); err != nil {
						t.Error("scoped fixture cleanup", err)
					}
				}
			})
			from, to := source.Join(sourceRoot, "payload"), target.Join(targetRoot, "payload")
			const contents = "ordinary transfer requires no unrelated directory privileges"
			switch mode {
			case "shared-parent":
				if err := source.MkdirAll(ctx, from, 0750); err != nil {
					t.Fatal(err)
				}
				writeRemoteFile(t, ctx, source, source.Join(from, "data"), contents)
				command := "sudo -n chown root:root -- " + shellQuote(targetRoot) + " && sudo -n chmod 1777 -- " + shellQuote(targetRoot) +
					" && test -w " + shellQuote(targetRoot) + " && ! test -O " + shellQuote(targetRoot)
				if err := target.Exec(ctx, command, endpoint.ExecOptions{}); err != nil {
					t.Fatal("shared parent must be writable but not owned by the transfer account", err)
				}
			case "readonly-empty":
				if err := source.MkdirAll(ctx, from, 0555); err != nil {
					t.Fatal(err)
				}
				if err := source.Exec(ctx, "test -O "+shellQuote(from)+" && ! test -w "+shellQuote(from)+" && test -w "+shellQuote(sourceRoot), endpoint.ExecOptions{}); err != nil {
					t.Fatal("empty source must be readonly and removable from its writable parent", err)
				}
			case "search-only-ancestor":
				writeRemoteFile(t, ctx, source, from, contents)
				landing := target.Join(opaque, "landing")
				if err := target.MkdirAll(ctx, landing, 0700); err != nil {
					t.Fatal(err)
				}
				command := "sudo -n chown root:root -- " + shellQuote(opaque) + " && sudo -n chmod 0711 -- " + shellQuote(opaque) +
					" && test -x " + shellQuote(opaque) + " && ! test -r " + shellQuote(opaque) + " && test -w " + shellQuote(landing)
				if err := target.Exec(ctx, command, endpoint.ExecOptions{}); err != nil {
					t.Fatal("existing ancestor must be searchable but not readable by transfer account", err)
				}
				to = target.Join(landing, "payload")
			}
			result, err := transfer.Run(ctx, transfer.Operation{Source: source, Destination: target, SourcePath: from, TargetPath: to, Move: true})
			if err != nil || !result.Moved {
				t.Fatalf("legitimate move required unrelated directory permissions: %+v %v", result, err)
			}
			if _, err := source.Stat(ctx, from); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("verified source was not removed: %v", err)
			}
			switch mode {
			case "readonly-empty":
				entry, err := target.Stat(ctx, to)
				if err != nil || !entry.IsDir() || entry.Mode.Perm() != 0555 {
					t.Fatalf("readonly target mode changed: %+v %v", entry, err)
				}
				entries, err := target.List(ctx, to)
				if err != nil || len(entries) != 0 {
					t.Fatalf("empty directory gained files: %+v %v", entries, err)
				}
			case "shared-parent":
				assertRemoteFile(t, ctx, target, target.Join(to, "data"), contents)
			default:
				assertRemoteFile(t, ctx, target, to, contents)
			}
		})
	}
}
