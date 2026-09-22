package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

func TestPreflightCollectsSnapshotIdentitySpaceAndCapabilities(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("preflight"), 0600); err != nil {
		t.Fatal(err)
	}
	report, err := Preflight(context.Background(), Operation{Source: endpoint.NewLocal(), Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: target})
	if err != nil {
		t.Fatal(err)
	}
	if !report.SameMachine || report.Source.Bytes != 9 || report.SourceCapabilities.Inode == 0 || report.TargetCapabilities.FreeBytes <= 0 || report.SourceCapabilities.Architecture == "" {
		t.Fatalf("incomplete report: %#v", report)
	}
}

type fallbackCapabilityEndpoint struct {
	endpoint.Endpoint
	calls int
}

func (e *fallbackCapabilityEndpoint) Exec(_ context.Context, _ string, options endpoint.ExecOptions) error {
	e.calls++
	if e.calls == 2 {
		_, _ = fmt.Fprint(options.Stdout, "VERSION 7 11\nARCH aarch64\nFREE 4096\nTOOL scp\nTOOL tar\n")
	}
	return nil
}

func TestCapabilityProbeRetriesMissingArchitectureAndTools(t *testing.T) {
	target := &fallbackCapabilityEndpoint{Endpoint: endpoint.NewLocal()}
	capabilities, err := probeCapabilities(context.Background(), target, "/source", "/target")
	if err != nil {
		t.Fatal(err)
	}
	if target.calls != 2 || capabilities.Architecture != "aarch64" || !capabilities.Tools["scp"] || !capabilities.Tools["tar"] {
		t.Fatalf("fallback capabilities=%#v calls=%d", capabilities, target.calls)
	}
}

type shellSilentCapabilityEndpoint struct{ endpoint.Endpoint }

func (shellSilentCapabilityEndpoint) Exec(context.Context, string, endpoint.ExecOptions) error {
	return nil
}

func (shellSilentCapabilityEndpoint) Open(context.Context, string) (io.ReadCloser, error) {
	header := make([]byte, 20)
	copy(header, []byte("\x7fELF"))
	header[5] = 1
	header[18], header[19] = 183, 0
	return io.NopCloser(bytes.NewReader(header)), nil
}

func (shellSilentCapabilityEndpoint) AvailableBytes(context.Context, string) (int64, error) {
	return 8192, nil
}

func (shellSilentCapabilityEndpoint) FileVersion(context.Context, string) (uint64, uint64, error) {
	return 9, 17, nil
}

func TestCapabilityProbeUsesELFAndStatVFSWhenShellDropsOutput(t *testing.T) {
	target := shellSilentCapabilityEndpoint{Endpoint: endpoint.NewLocal()}
	capabilities, err := probeCapabilities(context.Background(), target, "/source", "/target")
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.Architecture != "arm64" || capabilities.FreeBytes != 8192 || capabilities.Device != 9 || capabilities.Inode != 17 || !capabilities.Tools["rsync"] {
		t.Fatalf("fallback capabilities=%#v", capabilities)
	}
}

func TestPreflightRejectsRelativeAndConflict(t *testing.T) {
	t.Parallel()
	local := endpoint.NewLocal()
	if _, err := Preflight(context.Background(), Operation{Source: local, Destination: local, SourcePath: "relative", TargetPath: "also-relative"}); err == nil {
		t.Fatal("relative paths were accepted")
	}
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("target"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Preflight(context.Background(), Operation{Source: local, Destination: local, SourcePath: source, TargetPath: target})
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

type permissionDeniedListEndpoint struct{ endpoint.Endpoint }

func (permissionDeniedListEndpoint) List(context.Context, string) ([]endpoint.Entry, error) {
	return nil, fs.ErrPermission
}

func TestPreflightDefersUnreadableSourceTreeToElevatedStrategy(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "protected-source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	wrapped := permissionDeniedListEndpoint{Endpoint: endpoint.NewLocal()}
	report, err := Preflight(context.Background(), Operation{Source: wrapped, Destination: endpoint.NewLocal(), SourcePath: source, TargetPath: filepath.Join(root, "target")})
	if err != nil {
		t.Fatal(err)
	}
	if !report.SourceInspectionDenied || len(report.Source.Items) != 1 || !report.Source.Items[0].Mode.IsDir() {
		t.Fatalf("unreadable source was not deferred: %#v", report)
	}
}
