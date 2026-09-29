package agentservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lovitus/dragfm-gui/internal/agentlease"
)

type ownershipMarker struct {
	Version int       `json:"version"`
	Created time.Time `json:"created"`
	Nonce   string    `json:"nonce"`
}

func selfDirectory() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	if filepath.Dir(dir) != "/tmp" || !strings.HasPrefix(filepath.Base(dir), ".dragfm-") {
		return ""
	}
	return dir
}

func validatedTemp(root *os.Root, name string) (*os.Root, ownershipMarker, error) {
	var marker ownershipMarker
	if !strings.HasPrefix(name, ".dragfm-") || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return nil, marker, errors.New("unscoped temporary path")
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, marker, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, marker, errors.New("temporary directory is not private")
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, marker, err
	}
	fail := func(err error) (*os.Root, ownershipMarker, error) { _ = child.Close(); return nil, marker, err }
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return fail(errors.New("temporary directory changed"))
	}
	owner, err := child.Lstat(".dragfm-owner-v1")
	if err != nil || !owner.Mode().IsRegular() || owner.Mode().Perm()&0077 != 0 || !sameOwner(info, owner) {
		return fail(errors.New("temporary ownership marker is invalid"))
	}
	file, err := child.Open(".dragfm-owner-v1")
	if err != nil {
		return fail(err)
	}
	actual, statErr := file.Stat()
	data, readErr := io.ReadAll(io.LimitReader(file, 4097))
	_ = file.Close()
	if statErr != nil || !os.SameFile(owner, actual) || readErr != nil || len(data) > 4096 || json.Unmarshal(data, &marker) != nil || (marker.Version != 1 && marker.Version != 2) || marker.Nonce == "" || name != ".dragfm-"+marker.Nonce || marker.Created.IsZero() || marker.Created.After(time.Now().Add(time.Minute)) {
		return fail(errors.New("temporary ownership marker does not match"))
	}
	return child, marker, nil
}

func removeOwnedTemp(ctx context.Context, path string) error {
	path = filepath.Clean(path)
	if filepath.Dir(path) != "/tmp" || path != selfDirectory() {
		return errors.New("refusing to remove unscoped temporary path")
	}
	root, err := os.OpenRoot("/tmp")
	if err != nil {
		return err
	}
	defer root.Close()
	child, _, err := validatedTemp(root, filepath.Base(path))
	if err != nil {
		return err
	}
	defer child.Close()
	opened, err := child.Stat(".")
	if err != nil {
		return err
	}
	current, err := root.Lstat(filepath.Base(path))
	if err != nil || !os.SameFile(opened, current) || !agentlease.Matches(ctx, opened) {
		return errors.New("helper installation changed before removal")
	}
	return root.RemoveAll(filepath.Base(path))
}

// All modes, including independent --sftp channels, acquire a lease BEFORE
// handling input. The returned release never deletes files: Serve must first
// prove quiescence. Inherited child descriptors survive a helper crash.
func OwnInstallation(ctx context.Context) (context.Context, func() error, error) {
	dir := selfDirectory()
	if dir == "" {
		return ctx, func() error { return nil }, nil
	}
	root, err := os.OpenRoot("/tmp")
	if err != nil {
		return ctx, nil, err
	}
	defer root.Close()
	child, _, err := validatedTemp(root, filepath.Base(dir))
	if err != nil {
		return ctx, nil, err
	}
	ctx, lease, err := agentlease.Acquire(ctx, child)
	if err != nil {
		return ctx, nil, err
	}
	original, statErr := child.Stat(".")
	current, pathErr := root.Lstat(filepath.Base(dir))
	if statErr != nil || pathErr != nil || !os.SameFile(original, current) {
		_ = lease.Close()
		return ctx, nil, errors.New("helper installation changed while acquiring lease")
	}
	file, err := child.OpenFile(".dragfm-agent-pid", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		_, _ = file.WriteString(strconv.Itoa(os.Getpid()))
		_ = file.Close()
	}
	return ctx, lease.Close, nil
}

func cleanupOwnedTemps(directory, keep string, olderThan time.Duration) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	var expected os.FileInfo
	if own := selfDirectory(); own != "" {
		expected, _ = os.Lstat(own)
	}
	if expected == nil {
		expected, _ = os.Stat(directory)
	}
	now := time.Now()
	var failures []error
	for _, entry := range entries {
		candidate := filepath.Join(directory, entry.Name())
		if !entry.IsDir() || candidate == filepath.Clean(keep) || !strings.HasPrefix(entry.Name(), ".dragfm-") {
			continue
		}
		info, err := root.Lstat(entry.Name())
		if err != nil || (!sameOwner(expected, info) && !effectiveOwner(info)) {
			continue
		}
		child, marker, err := validatedTemp(root, entry.Name())
		if err != nil {
			continue
		}
		if now.Sub(marker.Created) < olderThan {
			_ = child.Close()
			continue
		}
		if marker.Version == 2 {
			// The process type is irrelevant: system pipelines inherit this
			// directory lease too. Never turn unsupported flock into permission
			// to delete. Keep the descriptor locked until removal is complete.
			lock, lockErr := lockStaleDirectory(child)
			if lockErr == nil {
				current, statErr := root.Lstat(entry.Name())
				if statErr == nil && os.SameFile(info, current) {
					if err := root.RemoveAll(entry.Name()); err != nil {
						failures = append(failures, err)
					}
				}
				_ = lock.Close()
			} else if !errors.Is(lockErr, errDirectoryLeased) {
				failures = append(failures, lockErr)
			}
			_ = child.Close()
			continue
		}
		// A v1 helper PID being absent/reused does not prove that its Hans,
		// rsync or independent SFTP processes exited. It has no lease contract;
		// keep it, even when its PID is conclusively dead.
		_ = child.Close()
	}
	return errors.Join(failures...)
}
