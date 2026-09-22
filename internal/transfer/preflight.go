package transfer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

type EndpointCapabilities struct {
	Architecture string
	Tools        map[string]bool
	FreeBytes    int64
	Device       uint64
	Inode        uint64
}

type PreflightReport struct {
	SourcePath, TargetPath string
	Source                 Manifest
	SourceIdentity         endpoint.Identity
	TargetIdentity         endpoint.Identity
	SourceCapabilities     EndpointCapabilities
	TargetCapabilities     EndpointCapabilities
	TargetExists           bool
	SourceInspectionDenied bool
	TargetInspectionDenied bool
	SameMachine            bool
}

// Preflight resolves and validates every stable fact needed by the strategy
// planner before it opens listeners, asks for sudo, or starts a transfer.
func Preflight(ctx context.Context, operation Operation) (PreflightReport, error) {
	if operation.Source == nil || operation.Destination == nil {
		return PreflightReport{}, errors.New("transfer endpoints are required")
	}
	if err := validateOperationPaths(ctx, operation); err != nil {
		return PreflightReport{}, err
	}
	sourcePath, err := operation.Source.Abs(ctx, operation.SourcePath)
	if err != nil {
		return PreflightReport{}, fmt.Errorf("resolve source path: %w", err)
	}
	targetPath, err := operation.Destination.Abs(ctx, operation.TargetPath)
	if err != nil {
		return PreflightReport{}, fmt.Errorf("resolve target path: %w", err)
	}
	if sourcePath != operation.SourcePath || targetPath != operation.TargetPath {
		return PreflightReport{}, errors.New("transfer paths must be absolute and normalized")
	}
	if operation.Source.Dir(sourcePath) == sourcePath {
		return PreflightReport{}, errors.New("refusing to transfer a filesystem root")
	}
	report := PreflightReport{SourcePath: sourcePath, TargetPath: targetPath}
	report.Source, err = Snapshot(ctx, operation.Source, sourcePath, false)
	if err != nil {
		if !errors.Is(err, fs.ErrPermission) {
			return PreflightReport{}, fmt.Errorf("source snapshot: %w", err)
		}
		entry, statErr := operation.Source.Stat(ctx, sourcePath)
		if statErr != nil {
			return PreflightReport{}, fmt.Errorf("source snapshot: %w", err)
		}
		item := ManifestItem{Mode: entry.Mode, Size: entry.Size, ModifiedNS: entry.Modified.UnixNano(), LinkTarget: entry.LinkTarget, SourcePath: sourcePath}
		report.Source = Manifest{Items: []ManifestItem{item}}
		if entry.Mode.IsRegular() {
			report.Source.Bytes = entry.Size
		}
		report.SourceInspectionDenied = true
	}
	report.SourceIdentity, err = operation.Source.Identity(ctx)
	if err != nil {
		return PreflightReport{}, fmt.Errorf("source identity: %w", err)
	}
	report.TargetIdentity, err = operation.Destination.Identity(ctx)
	if err != nil {
		return PreflightReport{}, fmt.Errorf("target identity: %w", err)
	}
	report.SameMachine = report.SourceIdentity.MachineID != "" && report.SourceIdentity.MachineID == report.TargetIdentity.MachineID
	if target, statErr := operation.Destination.Stat(ctx, targetPath); statErr == nil {
		report.TargetExists = true
		if !operation.Overwrite {
			return PreflightReport{}, fs.ErrExist
		}
		if report.Source.Items[0].Mode.IsDir() != target.Mode.IsDir() {
			// An explicit overwrite may replace unlike file types, but it is not a
			// directory merge and needs enough space for the complete source.
			report.TargetExists = true
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		// A later remote-sudo strategy can inspect and commit this path using its
		// elevated helper. Continue probing from an accessible ancestor instead
		// of rejecting the complete plan before approval can be requested.
		if errors.Is(statErr, fs.ErrPermission) {
			report.TargetInspectionDenied = true
		} else {
			return PreflightReport{}, fmt.Errorf("inspect target: %w", statErr)
		}
	}
	targetDirectory, err := existingDirectory(ctx, operation.Destination, operation.Destination.Dir(targetPath))
	if err != nil {
		return PreflightReport{}, err
	}
	report.SourceCapabilities, err = probeCapabilities(ctx, operation.Source, sourcePath, operation.Source.Dir(sourcePath))
	if err != nil {
		return PreflightReport{}, fmt.Errorf("probe source: %w", err)
	}
	report.TargetCapabilities, err = probeCapabilities(ctx, operation.Destination, targetDirectory, targetDirectory)
	if err != nil {
		return PreflightReport{}, fmt.Errorf("probe target: %w", err)
	}
	if report.TargetCapabilities.FreeBytes >= 0 && report.TargetCapabilities.FreeBytes < report.Source.Bytes {
		return PreflightReport{}, fmt.Errorf("目标剩余空间不足：需要 %d bytes，可用 %d bytes", report.Source.Bytes, report.TargetCapabilities.FreeBytes)
	}
	return report, nil
}

func existingDirectory(ctx context.Context, target endpoint.Endpoint, directory string) (string, error) {
	for {
		entry, err := target.Stat(ctx, directory)
		if err == nil {
			if !entry.Mode.IsDir() {
				return "", fmt.Errorf("target parent %q is not a directory", directory)
			}
			return directory, nil
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, fs.ErrPermission) {
			return "", fmt.Errorf("inspect target parent: %w", err)
		}
		parent := target.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("target has no existing parent directory: %s", directory)
		}
		directory = parent
	}
}

func probeCapabilities(ctx context.Context, target endpoint.Endpoint, versionPath, spacePath string) (EndpointCapabilities, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result := EndpointCapabilities{Tools: make(map[string]bool), FreeBytes: -1}
	if runtime.GOOS == "windows" {
		identity, _ := target.Identity(ctx)
		if identity.Kind == endpoint.LocalKind {
			result.Architecture = runtime.GOARCH
			for _, tool := range []string{"rsync", "scp", "tar", "ncat", "nc", "sudo"} {
				_, err := exec.LookPath(tool)
				result.Tools[tool] = err == nil
			}
			return result, nil
		}
	}
	command := "set -f; " +
		"if stat -c '%d %i' -- " + quotePOSIX(versionPath) + " >/dev/null 2>&1; then printf 'VERSION '; stat -c '%d %i' -- " + quotePOSIX(versionPath) + "; " +
		"elif stat -f '%d %i' -- " + quotePOSIX(versionPath) + " >/dev/null 2>&1; then printf 'VERSION '; stat -f '%d %i' -- " + quotePOSIX(versionPath) + "; fi; " +
		"df -Pk -- " + quotePOSIX(spacePath) + " 2>/dev/null | awk 'NR > 1 && $4 ~ /^[0-9]+$/ { printf \"FREE %.0f\\n\", $4 * 1024 }'; " +
		"printf 'ARCH '; uname -m 2>/dev/null || true; " +
		"for t in rsync scp tar ncat nc sudo; do if command -v \"$t\" >/dev/null 2>&1; then printf 'TOOL %s\\n' \"$t\"; fi; done"
	var output bytes.Buffer
	if err := target.Exec(ctx, command, endpoint.ExecOptions{Stdout: &output, Stderr: &output}); err != nil {
		return result, fmt.Errorf("capability command: %w: %s", err, strings.TrimSpace(output.String()))
	}
	parseOutput := func(value string) {
		for _, line := range strings.Split(value, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			switch fields[0] {
			case "VERSION":
				if len(fields) == 3 {
					result.Device, _ = strconv.ParseUint(fields[1], 10, 64)
					result.Inode, _ = strconv.ParseUint(fields[2], 10, 64)
				}
			case "FREE":
				if len(fields) == 2 {
					if value, parseErr := strconv.ParseInt(fields[1], 10, 64); parseErr == nil && value >= 0 {
						result.FreeBytes = value
					}
				}
			case "ARCH":
				if len(fields) == 2 {
					result.Architecture = fields[1]
				}
			case "TOOL":
				if len(fields) == 2 {
					result.Tools[fields[1]] = true
				}
			}
		}
	}
	parseOutput(output.String())
	// Some restricted or old SSH command wrappers accept the compound probe but
	// suppress its stdout after one unsupported utility invocation. Architecture
	// and tool presence are safety-critical inputs for helper selection, so retry
	// only missing fields with a deliberately simple POSIX command.
	if result.Architecture == "" || len(result.Tools) == 0 || result.FreeBytes < 0 {
		fallbackCommand := "printf 'ARCH '; uname -m 2>/dev/null; " +
			"if stat -c '%d %i' -- " + quotePOSIX(versionPath) + " >/dev/null 2>&1; then printf 'VERSION '; stat -c '%d %i' -- " + quotePOSIX(versionPath) + "; " +
			"elif stat -f '%d %i' -- " + quotePOSIX(versionPath) + " >/dev/null 2>&1; then printf 'VERSION '; stat -f '%d %i' -- " + quotePOSIX(versionPath) + "; fi; " +
			"df -Pk -- " + quotePOSIX(spacePath) + " 2>/dev/null | awk 'NR > 1 && $4 ~ /^[0-9]+$/ { printf \"FREE %.0f\\n\", $4 * 1024 }'; " +
			"for t in rsync scp tar ncat nc sudo; do command -v \"$t\" >/dev/null 2>&1 && printf 'TOOL %s\\n' \"$t\"; done"
		var lastFallbackErr error
		for attempt := 0; attempt < 3; attempt++ {
			var fallback bytes.Buffer
			lastFallbackErr = target.Exec(ctx, fallbackCommand, endpoint.ExecOptions{Stdout: &fallback, Stderr: &fallback})
			if lastFallbackErr == nil {
				parseOutput(fallback.String())
				if result.Architecture != "" && result.FreeBytes >= 0 {
					break
				}
			}
			if attempt < 2 {
				timer := time.NewTimer(75 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return result, ctx.Err()
				case <-timer.C:
				}
			}
		}
		if lastFallbackErr != nil && result.Architecture == "" {
			// Continue into the shell-independent fallbacks below. Restricted
			// command wrappers may reject compound commands while SFTP still works.
		}
	}
	if result.Architecture == "" {
		result.Architecture, _ = probeELFArchitecture(ctx, target)
	}
	if result.FreeBytes < 0 {
		if provider, ok := target.(interface {
			AvailableBytes(context.Context, string) (int64, error)
		}); ok {
			if value, probeErr := provider.AvailableBytes(ctx, spacePath); probeErr == nil {
				result.FreeBytes = value
			}
		}
	}
	if len(result.Tools) == 0 {
		for _, tool := range []string{"rsync", "scp", "tar", "ncat", "nc", "sudo"} {
			result.Tools[tool] = target.Exec(ctx, "command -v "+quotePOSIX(tool)+" >/dev/null 2>&1", endpoint.ExecOptions{}) == nil
		}
	}
	if result.Inode == 0 {
		if provider, ok := target.(interface {
			FileVersion(context.Context, string) (uint64, uint64, error)
		}); ok {
			result.Device, result.Inode, _ = provider.FileVersion(ctx, versionPath)
		}
	}
	if result.Architecture == "" {
		return result, errors.New("remote architecture probe returned no value")
	}
	if result.FreeBytes < 0 {
		return result, errors.New("remote free-space probe returned no value")
	}
	if result.Inode == 0 {
		return result, errors.New("file identity probe returned no inode")
	}
	return result, nil
}

func probeELFArchitecture(ctx context.Context, target endpoint.Endpoint) (string, error) {
	var lastErr error
	for _, candidate := range []string{"/bin/sh", "/usr/bin/env"} {
		reader, err := target.Open(ctx, candidate)
		if err != nil {
			lastErr = err
			continue
		}
		header := make([]byte, 20)
		_, readErr := io.ReadFull(reader, header)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || string(header[:4]) != "\x7fELF" {
			lastErr = errors.Join(readErr, closeErr, errors.New("not an ELF executable"))
			continue
		}
		var order binary.ByteOrder
		switch header[5] {
		case 1:
			order = binary.LittleEndian
		case 2:
			order = binary.BigEndian
		default:
			lastErr = errors.New("unknown ELF byte order")
			continue
		}
		switch order.Uint16(header[18:20]) {
		case 3:
			return "386", nil
		case 40:
			return "arm", nil
		case 62:
			return "amd64", nil
		case 183:
			return "arm64", nil
		default:
			lastErr = errors.New("unsupported ELF machine")
		}
	}
	return "", lastErr
}

func quotePOSIX(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
