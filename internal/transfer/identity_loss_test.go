package transfer

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

// Keep real filesystem/copy/hash/delete operations. Only the identity lookup
// boundary is made unavailable after copying, as a security-state table; this
// is not evidence that a real SSH stat failure has been exercised.
type identityLossSource struct {
	*endpoint.Local
	unavailable bool
	lookupErr   error
}

func (s *identityLossSource) FileVersion(ctx context.Context, name string) (uint64, uint64, error) {
	if s.unavailable {
		return 0, 0, s.lookupErr
	}
	return s.Local.FileVersion(ctx, name)
}

func TestMoveKeepsSourceWhenFinalIdentityCannotBeConfirmed(t *testing.T) {
	for _, tc := range []struct {
		name        string
		unavailable bool
		replace     bool
		lookupErr   error
	}{
		{name: "unchanged"},
		{name: "lookup-error", unavailable: true, lookupErr: errors.New("fixture identity lookup failed")},
		{name: "replacement-and-lookup-error", unavailable: true, replace: true, lookupErr: errors.New("fixture identity lookup failed")},
		{name: "zero-without-error", unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			sourcePath, targetPath := filepath.Join(root, "source"), filepath.Join(root, "target")
			const content = "identical bytes do not prove the original inode still exists"
			if err := os.WriteFile(sourcePath, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			source := &identityLossSource{Local: endpoint.NewLocal()}
			op := Operation{Source: source, Destination: endpoint.NewLocal(), SourcePath: sourcePath, TargetPath: targetPath}
			before, err := Snapshot(ctx, source, sourcePath, true)
			if err != nil || before.RootInode == 0 {
				t.Fatalf("no valid initial identity: %+v %v", before, err)
			}
			if _, err := Run(ctx, op); err != nil {
				t.Fatal(err)
			}
			if tc.replace {
				replacement := filepath.Join(root, "replacement")
				if err := os.WriteFile(replacement, []byte(content), before.Items[0].Mode.Perm()); err != nil {
					t.Fatal(err)
				}
				modified := before.Items[0].ModifiedTime()
				if err := os.Chtimes(replacement, modified, modified); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, sourcePath); err != nil {
					t.Fatal(err)
				}
				device, inode, err := source.FileVersion(ctx, sourcePath)
				if err != nil || (device == before.RootDevice && inode == before.RootInode) {
					t.Fatalf("replacement did not change actual identity: %d/%d %v", device, inode, err)
				}
			}
			source.unavailable, source.lookupErr = tc.unavailable, tc.lookupErr
			op.Move = true
			err = FinishMove(ctx, op, before)
			if tc.unavailable {
				if err == nil || Retryable(err) || !strings.Contains(err.Error(), "已复制但未移动") {
					t.Fatalf("identity loss did not retain source with a non-retryable result: %v", err)
				}
				if tc.lookupErr != nil && !errors.Is(err, tc.lookupErr) {
					t.Fatalf("original identity failure was lost: %v", err)
				}
				data, readErr := os.ReadFile(sourcePath)
				if readErr != nil || string(data) != content {
					t.Fatalf("unconfirmed source was removed or changed: %q %v", data, readErr)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(sourcePath); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("verified unchanged source was not moved: %v", err)
				}
			}
			data, err := os.ReadFile(targetPath)
			if err != nil || string(data) != content {
				t.Fatalf("copied destination changed: %q %v", data, err)
			}
		})
	}
}

func TestSourceManifestDoesNotDowngradeKnownIdentityToUnknown(t *testing.T) {
	item := ManifestItem{Relative: "", Mode: 0600, Size: 1, ModifiedNS: 1, SHA256: "same"}
	before := Manifest{Items: []ManifestItem{item}, RootDevice: 1, RootInode: 10}
	after := Manifest{Items: []ManifestItem{item}}
	if err := CompareManifests(before, after, false); err == nil {
		t.Fatal("missing final inode was treated as matching the original inode")
	}
	if err := CompareManifests(before, after, true); err != nil {
		t.Fatalf("target content verification incorrectly required the source inode: %v", err)
	}
}
