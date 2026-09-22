package endpoint

import (
	"context"
	"io"
	"io/fs"
	"time"
)

type Kind string

const (
	LocalKind Kind = "local"
	SSHKind   Kind = "ssh"
)

type Entry struct {
	Name       string
	Path       string
	Mode       fs.FileMode
	Size       int64
	Modified   time.Time
	LinkTarget string
}

func (e Entry) IsDir() bool { return e.Mode.IsDir() }

type Identity struct {
	Kind        Kind
	Name        string
	MachineID   string
	Fingerprint string
}

type ExecOptions struct {
	Directory string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

type AtomicWriter interface {
	io.WriteCloser
	Commit() error
	Abort() error
}

type PTYSession interface {
	Input() io.WriteCloser
	Output() io.Reader
	Resize(rows, columns uint) error
	Wait() error
	Close() error
}

type PTYProvider interface {
	OpenPTY(context.Context, string, string, uint, uint) (PTYSession, error)
}

// NativeCopier is implemented by POSIX endpoints that can copy two paths on
// the same machine without routing file data through the controller.
type NativeCopier interface {
	CopyNative(context.Context, string, string, bool, bool) error
}

type Endpoint interface {
	Identity(context.Context) (Identity, error)
	Home(context.Context) (string, error)
	Abs(context.Context, string) (string, error)
	Join(...string) string
	Dir(string) string
	List(context.Context, string) ([]Entry, error)
	Stat(context.Context, string) (Entry, error)
	Readlink(context.Context, string) (string, error)
	Open(context.Context, string) (io.ReadCloser, error)
	CreateAtomic(context.Context, string, fs.FileMode) (AtomicWriter, error)
	MkdirAll(context.Context, string, fs.FileMode) error
	Symlink(context.Context, string, string) error
	Chmod(context.Context, string, fs.FileMode) error
	Chtimes(context.Context, string, time.Time, time.Time) error
	Remove(context.Context, string, bool) error
	Rename(context.Context, string, string, bool) error
	Exec(context.Context, string, ExecOptions) error
	Close() error
}
