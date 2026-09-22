package agentservice

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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
	if statErr != nil || !os.SameFile(owner, actual) || readErr != nil || len(data) > 4096 || json.Unmarshal(data, &marker) != nil || marker.Version != 1 || marker.Nonce == "" || name != ".dragfm-"+marker.Nonce || marker.Created.IsZero() || marker.Created.After(time.Now().Add(time.Minute)) {
		return fail(errors.New("temporary ownership marker does not match"))
	}
	return child, marker, nil
}

func removeOwnedTemp(path string) error {
	path = filepath.Clean(path)
	if filepath.Dir(path) != "/tmp" {
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
	_ = child.Close()
	return root.RemoveAll(filepath.Base(path))
}

// Mark the running helper, then clean its private installation after all child
// processes and staging paths are stopped. A stale cleaner never removes an
// active helper merely because a transfer has lasted more than 24 hours.
func OwnInstallation() func() {
	dir := selfDirectory()
	if dir == "" {
		return func() {}
	}
	root, err := os.OpenRoot("/tmp")
	if err != nil {
		return func() {}
	}
	defer root.Close()
	child, _, err := validatedTemp(root, filepath.Base(dir))
	if err != nil {
		return func() {}
	}
	file, err := child.OpenFile(".dragfm-agent-pid", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		_, _ = file.WriteString(strconv.Itoa(os.Getpid()))
		_ = file.Close()
	}
	_ = child.Close()
	return func() { _ = removeOwnedTemp(dir) }
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
	for _, entry := range entries {
		candidate := filepath.Join(directory, entry.Name())
		if !entry.IsDir() || candidate == filepath.Clean(keep) || !strings.HasPrefix(entry.Name(), ".dragfm-") {
			continue
		}
		info, err := root.Lstat(entry.Name())
		if err != nil || !sameOwner(expected, info) {
			continue
		}
		child, marker, err := validatedTemp(root, entry.Name())
		if err != nil {
			continue
		}
		active := false
		if file, err := child.Open(".dragfm-agent-pid"); err == nil {
			data, _ := io.ReadAll(io.LimitReader(file, 32))
			_ = file.Close()
			if pid, err := strconv.Atoi(string(data)); err == nil && pid > 0 {
				exe, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
				active = filepath.Dir(exe) == candidate
			}
		}
		_ = child.Close()
		if !active && now.Sub(marker.Created) >= olderThan {
			_ = root.RemoveAll(entry.Name())
		}
	}
	return nil
}
