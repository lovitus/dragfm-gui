// Package agentlease keeps an installation alive across independently running
// file channels and inherited child processes. Closing the controller or the
// helper's own descriptor does not release a child's copy of the shared lock.
package agentlease

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
)

type key struct{}

type Lease struct {
	mu        sync.Mutex
	root      *os.Root
	shared    *os.File
	exclusive *os.File
	closing   bool
}

// Acquire takes ownership of root, including on failure.
func Acquire(ctx context.Context, root *os.Root) (context.Context, *Lease, error) {
	file, err := root.Open(".")
	if err == nil {
		err = lock(file, false)
	}
	if err != nil {
		if file != nil {
			_ = file.Close()
		}
		_ = root.Close()
		return ctx, nil, err
	}
	lease := &Lease{root: root, shared: file}
	return context.WithValue(ctx, key{}, lease), lease, nil
}

// Attach preserves existing descriptor positions (Hans' secret is fd 3).
// The service must finish admitting/starting children before Guard begins.
func Attach(ctx context.Context, command *exec.Cmd) error {
	lease, _ := ctx.Value(key{}).(*Lease)
	if lease == nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closing || lease.shared == nil {
		return errors.New("helper installation is closing")
	}
	command.ExtraFiles = append(command.ExtraFiles, lease.shared)
	return nil
}

// Guard closes only our shared descriptor, then locks a NEW open file
// description exclusively. Upgrading the inherited descriptor itself would
// upgrade the children's lock too, falsely claiming they have stopped.
// Retain the exclusive lock through partial and installation cleanup.
func Guard(ctx context.Context) error {
	lease, _ := ctx.Value(key{}).(*Lease)
	if lease == nil {
		return nil // In-process service, not an uploaded installation.
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.root == nil {
		return errors.New("helper installation lease is closed")
	}
	lease.closing = true
	if lease.exclusive != nil {
		return nil
	}
	if lease.shared != nil {
		if err := lease.shared.Close(); err != nil {
			return err
		}
		lease.shared = nil
	}
	file, err := lease.root.Open(".")
	if err != nil {
		return err
	}
	if err := lock(file, true); err != nil {
		_ = file.Close()
		return err
	}
	lease.exclusive = file
	return nil
}

// Matches ties removal to the inode opened at process startup, not a valid
// marker in a replacement directory with the same path.
func Matches(ctx context.Context, info os.FileInfo) bool {
	lease, _ := ctx.Value(key{}).(*Lease)
	if lease == nil {
		return false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.root == nil || lease.exclusive == nil {
		return false
	}
	original, err := lease.root.Stat(".")
	return err == nil && os.SameFile(original, info)
}

func (lease *Lease) Close() error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	lease.closing = true
	var failures []error
	for _, file := range []*os.File{lease.shared, lease.exclusive} {
		if file != nil {
			failures = append(failures, file.Close())
		}
	}
	lease.shared, lease.exclusive = nil, nil
	if lease.root != nil {
		failures = append(failures, lease.root.Close())
		lease.root = nil
	}
	return errors.Join(failures...)
}
