package endpoint

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Local struct{}

func NewLocal() *Local { return &Local{} }

func (l *Local) FileVersion(_ context.Context, target string) (uint64, uint64, error) {
	return localFileVersion(target)
}

func (l *Local) Identity(context.Context) (Identity, error) {
	host, _ := os.Hostname()
	return Identity{Kind: LocalKind, Name: host, MachineID: localMachineID()}, nil
}

func (l *Local) Home(context.Context) (string, error) { return os.UserHomeDir() }

func (l *Local) Abs(_ context.Context, value string) (string, error) {
	if strings.HasPrefix(value, "~"+string(filepath.Separator)) || value == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if value == "~" {
			return home, nil
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~"+string(filepath.Separator)))
	}
	return filepath.Abs(value)
}

func (l *Local) Join(parts ...string) string { return filepath.Join(parts...) }
func (l *Local) Dir(value string) string     { return filepath.Dir(value) }

func (l *Local) List(ctx context.Context, directory string) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry, err := localEntry(filepath.Join(directory, item.Name()))
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	sortEntries(entries)
	return entries, nil
}

func (l *Local) Stat(_ context.Context, path string) (Entry, error) { return localEntry(path) }

func (l *Local) Readlink(_ context.Context, path string) (string, error) { return os.Readlink(path) }

func (l *Local) Open(_ context.Context, path string) (io.ReadCloser, error) { return os.Open(path) }

func (l *Local) CreateAtomic(_ context.Context, path string, mode fs.FileMode) (AtomicWriter, error) {
	return newLocalAtomicWriter(path, mode)
}

func (l *Local) MkdirAll(_ context.Context, path string, mode fs.FileMode) error {
	return os.MkdirAll(path, mode.Perm())
}

func (l *Local) Symlink(_ context.Context, target, path string) error {
	return os.Symlink(target, path)
}

func (l *Local) Chmod(_ context.Context, path string, mode fs.FileMode) error {
	return os.Chmod(path, mode.Perm())
}

func (l *Local) Chtimes(_ context.Context, path string, atime, mtime time.Time) error {
	return os.Chtimes(path, atime, mtime)
}

func (l *Local) Remove(_ context.Context, path string, recursive bool) error {
	if recursive {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

func (l *Local) Rename(_ context.Context, source, target string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Lstat(target); err == nil {
			return fs.ErrExist
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return os.Rename(source, target)
}

func (l *Local) CopyNative(ctx context.Context, source, target string, sourceDirectory, merge bool) error {
	if runtime.GOOS == "windows" {
		return errors.New("native cp is unavailable on Windows")
	}
	if merge && sourceDirectory {
		return exec.CommandContext(ctx, "cp", "-a", filepath.Clean(source)+string(filepath.Separator)+".", target).Run()
	}
	return exec.CommandContext(ctx, "cp", "-a", source, target).Run()
}

func (l *Local) Exec(ctx context.Context, command string, options ExecOptions) error {
	shell := "sh"
	args := []string{"-c", command}
	if runtime.GOOS == "windows" {
		shell, args = "cmd.exe", []string{"/d", "/s", "/c", command}
	}
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = options.Directory, options.Stdin, options.Stdout, options.Stderr
	return cmd.Run()
}

func (l *Local) Close() error { return nil }

func localEntry(path string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	entry := Entry{Name: info.Name(), Path: path, Mode: info.Mode(), Size: info.Size(), Modified: info.ModTime()}
	if info.Mode()&fs.ModeSymlink != 0 {
		entry.LinkTarget, _ = os.Readlink(path)
	}
	return entry, nil
}

type localAtomicWriter struct {
	file      *os.File
	temporary string
	target    string
	done      bool
}

func newLocalAtomicWriter(target string, mode fs.FileMode) (*localAtomicWriter, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".dragfm-partial-*")
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, err
	}
	return &localAtomicWriter{file: file, temporary: file.Name(), target: target}, nil
}

func (w *localAtomicWriter) Write(data []byte) (int, error) { return w.file.Write(data) }

func (w *localAtomicWriter) Close() error {
	if w.done {
		return nil
	}
	return w.file.Close()
}

func (w *localAtomicWriter) Commit() error {
	if w.done {
		return errors.New("atomic writer already completed")
	}
	if err := w.file.Sync(); err != nil {
		_ = w.Abort()
		return err
	}
	if err := w.file.Close(); err != nil {
		_ = w.Abort()
		return err
	}
	if err := replaceFile(w.temporary, w.target); err != nil {
		_ = os.Remove(w.temporary)
		w.done = true
		return err
	}
	w.done = true
	return nil
}

func (w *localAtomicWriter) Abort() error {
	if w.done {
		return nil
	}
	w.done = true
	return errors.Join(w.file.Close(), os.Remove(w.temporary))
}

func randomSuffix() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", value[:])
}

func sortEntries(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

func localMachineID() string {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if data, err := os.ReadFile(path); err == nil {
			if value := strings.TrimSpace(string(data)); value != "" {
				return value
			}
		}
	}
	host, _ := os.Hostname()
	return host
}
