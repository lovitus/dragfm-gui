//go:build integration && !windows

package webgui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

func TestHostedSameSSHDChrootsCannotRedirectMove(t *testing.T) {
	name := os.Getenv("DRAGFM_E2E_SOURCE_SSH")
	if name == "" {
		t.Skip("disposable SSH fixtures are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	route, _ := openSSHRoute(t, name)
	route.Hops[0].User = "chroota"
	source, err := endpoint.DialSSH(ctx, "chroot-source", "", route)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	route.Hops[0].User = "chrootb"
	target, err := endpoint.DialSSH(ctx, "chroot-target", "", route)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	sourceID, err := source.Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := target.Identity(ctx)
	if err != nil || sourceID.MachineID == "" || sourceID.MachineID != targetID.MachineID || sourceID.Fingerprint == "" || sourceID.Fingerprint != targetID.Fingerprint {
		t.Fatalf("fixture requires same real machine ID and authenticated SSH key: id-present=%t/%t id-equal=%t key-present=%t key-equal=%t error=%v", sourceID.MachineID != "", targetID.MachineID != "", sourceID.MachineID == targetID.MachineID, sourceID.Fingerprint != "", sourceID.Fingerprint == targetID.Fingerprint, err)
	}
	root := "/data/move-" + randomTransferToken(8)
	for _, remote := range []*endpoint.Remote{source, target} {
		if err := remote.MkdirAll(ctx, root, 0700); err != nil {
			t.Fatal(err)
		}
		defer remote.Remove(context.Background(), root, true)
	}
	from, to := source.Join(root, "source"), target.Join(root, "target")
	const contents = "same server must not mean same filesystem view"
	writeRemoteFile(t, ctx, source, from, contents)
	if _, err := target.Stat(ctx, from); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("fixture accounts do not have separate real pathname roots")
	}
	// The same target parent also exists in the source jail. An incorrect
	// source-side rename therefore succeeds instead of hiding behind ENOENT.
	result, err := transfer.Run(ctx, transfer.Operation{Source: source, Destination: target, SourcePath: from, TargetPath: to, Move: true})
	if err != nil || !result.Moved {
		t.Fatalf("cross-view verified move failed: %+v %v", result, err)
	}
	if _, err := source.Stat(ctx, to); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("same SSH host key redirected move into the source account's shadow target")
	}
	assertRemoteFile(t, ctx, target, to, contents)
	if _, err := source.Stat(ctx, from); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("verified cross-view move did not remove source")
	}
}
