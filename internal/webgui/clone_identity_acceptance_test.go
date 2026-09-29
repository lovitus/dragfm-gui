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

// Only the cloned machine-id is injected. Both SSH fingerprints and every
// filesystem operation still come from the real disposable SSH fixtures.
type reviewedClonedMachine struct{ *endpoint.Remote }

func (r reviewedClonedMachine) Identity(ctx context.Context) (endpoint.Identity, error) {
	identity, err := r.Remote.Identity(ctx)
	if err == nil {
		identity.MachineID = "9c7e046825ab4d16b10723683c7d582e"
	}
	return identity, err
}

func TestHostedClonedMachineIDCannotRedirectCopyOrMove(t *testing.T) {
	source, target, _ := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	sourceID, err := source.Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := target.Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sourceID.Fingerprint == "" || targetID.Fingerprint == "" || sourceID.Fingerprint == targetID.Fingerprint {
		t.Fatal("fixture setup: two distinct pinned SSH identities are required")
	}

	sourceRoot := remoteTempDir(t, ctx, source)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := source.Remove(cleanup, sourceRoot, true); err != nil {
			t.Errorf("source fixture cleanup: %v", err)
		}
	})
	targetRoot := remoteTempDir(t, ctx, target)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := target.Remove(cleanup, targetRoot, true); err != nil {
			t.Errorf("target fixture cleanup: %v", err)
		}
	})
	// A same-named empty directory on the wrong host makes an erroneous
	// source-side rename succeed. Without it, ENOENT would hide the bug by
	// triggering the correct streaming fallback.
	if _, err := source.Stat(ctx, targetRoot); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("fixture setup: target's unique temporary path must not exist on source")
	}
	if err := source.MkdirAll(ctx, targetRoot, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := source.Remove(cleanup, targetRoot, true); err != nil {
			t.Errorf("source shadow fixture cleanup: %v", err)
		}
	})

	for _, test := range []struct {
		name string
		move bool
	}{
		{name: "copy", move: false},
		{name: "move", move: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			from := source.Join(sourceRoot, test.name+".txt")
			to := target.Join(targetRoot, test.name+".txt")
			contents := "must reach the actual target filesystem: " + test.name
			writeRemoteFile(t, ctx, source, from, contents)
			op := transfer.Operation{
				Source: reviewedClonedMachine{source}, Destination: reviewedClonedMachine{target},
				SourcePath: from, TargetPath: to, Move: test.move,
			}
			report, err := transfer.Preflight(ctx, op)
			if err != nil {
				t.Fatal(err)
			}
			if report.SameMachine {
				t.Error("conflicting SSH fingerprints were classified as the same machine")
			}
			result, err := transfer.Run(ctx, op)
			if err != nil {
				t.Fatalf("cross-host %s failed: %v", test.name, err)
			}
			assertRemoteFile(t, ctx, target, to, contents)
			if _, err := source.Stat(ctx, to); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("transfer wrote the target path on the wrong host: %v", err)
			}
			if test.move {
				if !result.Moved || result.SourceKept {
					t.Errorf("verified move result is inconsistent: %+v", result)
				}
				if _, err := source.Stat(ctx, from); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("verified move did not remove the original: %v", err)
				}
			} else {
				assertRemoteFile(t, ctx, source, from, contents)
			}
		})
	}
}
