package endpoint

import (
	"context"
	"io"
	"io/fs"
	"strings"
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
	UID, GID   uint32
	OwnerKnown bool
}

func (e Entry) IsDir() bool { return e.Mode.IsDir() }

type Identity struct {
	Kind        Kind
	Name        string
	MachineID   string
	Fingerprint string
	// Matching host keys do not establish a common pathname namespace:
	// sshd can chroot different accounts on the very same machine.
	Principal string
	RootView  string
}

// SameMachine permits same-filesystem optimizations only when the available
// identity evidence agrees. A machine-id alone can be copied with a disk image.
// Mixed local/SSH endpoints lack a common verified SSH identity and take the
// cross-machine path, where a move is verified before its source is removed.
func SameMachine(left, right Identity) bool {
	if left.MachineID == "" || left.MachineID != right.MachineID || left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case LocalKind:
		return true
	case SSHKind:
		if left.Principal == "" || left.Principal != right.Principal || left.RootView == "" || left.RootView != right.RootView {
			return false
		}
		leftKey := strings.TrimPrefix(strings.TrimSpace(left.Fingerprint), "SHA256:")
		rightKey := strings.TrimPrefix(strings.TrimSpace(right.Fingerprint), "SHA256:")
		return leftKey != "" && leftKey == rightKey
	default:
		return false
	}
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

// StagedWriter exposes an exclusively created, unpublished temporary file
// only after its writer has acknowledged close/exit. Commit is NOT called by
// an overlap probe. Abort still owns cleanup; callers must verify ownership
// if a prepared path could have been rebound before asking to remove it.
type StagedWriter interface {
	AtomicWriter
	PrepareStaged() (string, error)
}

type PTYSession interface {
	// CWDNonce binds prompt notifications to this shell, not arbitrary terminal
	// output. It is not a boundary against code running as the shell's user.
	CWDNonce() string
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
