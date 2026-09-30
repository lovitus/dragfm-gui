//go:build integration && !windows

package webgui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

func TestHostedNcatMoveRetainsSourceOnSyncFailure(t *testing.T) {
	source, target := systemOnlyFixtureEndpoints(t)
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("sync=%d", failAt), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			defer source.Remove(context.Background(), sourceRoot, true)
			defer target.Remove(context.Background(), targetRoot, true)
			home, err := target.Home(ctx)
			if err != nil {
				t.Fatal(err)
			}
			mode, counter := target.Join(home, ".sync-failure-mode"), target.Join(home, ".sync-attempts")
			for _, name := range []string{mode, counter} {
				if _, err := target.Stat(ctx, name); !errors.Is(err, fs.ErrNotExist) {
					t.Fatal("sync fault fixture must be absent before setup")
				}
				defer target.Remove(context.Background(), name, false)
			}
			writeRemoteFile(t, ctx, target, mode, fmt.Sprintf("%d\n", failAt))
			from, to := source.Join(sourceRoot, "source"), target.Join(targetRoot, "destination")
			writeRemoteFile(t, ctx, source, from, "source must survive a destination durability failure")
			before, err := transfer.Snapshot(ctx, source, from, true)
			if err != nil {
				t.Fatal(err)
			}
			app := New(filepath.Join(t.TempDir(), "unused.vault"))
			defer app.Lock()
			err = app.runSystemNcatTar(ctx, transfer.Operation{Source: source, Destination: target, SourcePath: from, TargetPath: to, Move: true}, strategy.SourcePush, false)
			if err == nil {
				t.Fatal("move succeeded without enforcing the destination sync failure")
			}
			after, readErr := transfer.Snapshot(ctx, source, from, true)
			if readErr != nil || transfer.CompareManifests(before, after, false) != nil {
				t.Fatal("sync failure did not preserve the exact source")
			}
			assertRemoteFile(t, ctx, target, counter, fmt.Sprintf("%d\n", failAt))
			if failAt == 1 {
				if _, err := target.Stat(ctx, to); !errors.Is(err, fs.ErrNotExist) {
					t.Fatal("pre-commit sync failure published a destination")
				}
			} else {
				if transfer.Retryable(err) {
					t.Fatal("post-commit sync failure allowed another transfer attempt")
				}
				assertRemoteFile(t, ctx, target, to, "source must survive a destination durability failure")
			}
			assertSystemNcatClean(t, ctx, source, target, targetRoot)
		})
	}
}
