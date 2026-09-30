package endpoint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
	"time"
)

// BorrowCommandFilesystem uses the existing Linux POSIX metadata/path methods
// with an approved, task-owned executor. It does not upload a server or close
// the browsing connection. Exec must return actual SSH exit evidence; the
// owner remains responsible for its remote lease and partial journal.
func (r *Remote) BorrowCommandFilesystem(exec func(context.Context, string, ExecOptions) error, version func(context.Context, string) (uint64, uint64, error), partial func(context.Context, string, bool) error, owner func(context.Context, string, uint32, uint32) error) *Remote {
	life, cancel := context.WithCancel(context.Background())
	return &Remote{name: r.name, fingerprint: r.fingerprint, client: r.client,
		connectionHost: r.connectionHost, fileVersion: version, partial: partial, fileOwner: owner,
		commands: &commandFilesystem{exec: exec, life: life, cancel: cancel}}
}

type commandFilesystem struct {
	exec   func(context.Context, string, ExecOptions) error
	life   context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	active sync.WaitGroup
	unsafe error
}

func (f *commandFilesystem) context(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(f.life, cancel)
	return ctx, func() { stop(); cancel() }
}

func (f *commandFilesystem) run(ctx context.Context, script string, options ExecOptions) error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return errors.New("command filesystem is closed")
	}
	f.active.Add(1)
	f.mu.Unlock()
	defer f.active.Done()
	ctx, cancel := f.context(ctx)
	defer cancel()
	err := f.exec(ctx, script, options)
	if errors.Is(err, ErrCommandExitUnconfirmed) {
		f.mu.Lock()
		f.unsafe = errors.Join(f.unsafe, err)
		f.mu.Unlock()
	}
	return err
}

func (f *commandFilesystem) close() error {
	f.mu.Lock()
	f.closed = true // No Add may race with the following Wait.
	f.cancel()
	f.mu.Unlock()
	done := make(chan struct{})
	go func() { f.active.Wait(); close(done) }()
	select {
	case <-done:
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.unsafe
	case <-time.After(4 * time.Second):
		return fmt.Errorf("command filesystem shutdown: %w", ErrCommandExitUnconfirmed)
	}
}

// Each file has its own SSH command. A fixed-length random readiness frame is
// emitted only AFTER the file descriptor opens and sudo consumes its framing.
// No file byte is sent until the caller has pinned the real partial inode.
// Pipes bound memory and let cancellation unblock SSH's stdin/stdout copiers.
type commandFileStream struct {
	input  *io.PipeWriter
	output *io.PipeReader
	cancel context.CancelFunc
	done   chan struct{}
	err    error // Published by closing done.
}

// Deliberate local cancellation must unblock the pipe without masking a real
// SSH exit status as a copier error in ssh.Session.Wait. Continue draining
// output and present stdin EOF; Exec still waits for remote exit/TERM/KILL.
type commandStreamOutput struct {
	ctx  context.Context
	pipe *io.PipeWriter
}

func (w commandStreamOutput) Write(data []byte) (int, error) {
	n, err := w.pipe.Write(data)
	if err != nil && w.ctx.Err() != nil {
		return len(data), nil
	}
	return n, err
}

type commandStreamInput struct {
	ctx  context.Context
	pipe *io.PipeReader
}

func (r commandStreamInput) Read(data []byte) (int, error) {
	n, err := r.pipe.Read(data)
	if err != nil && r.ctx.Err() != nil {
		return n, io.EOF
	}
	return n, err
}

func (r *Remote) startCommandFile(ctx context.Context, target string, writing bool) (*commandFileStream, error) {
	ctx, cancel := r.commands.context(ctx)
	s := &commandFileStream{cancel: cancel, done: make(chan struct{})}
	output, stdout := io.Pipe()
	s.output = output
	var input *io.PipeReader
	if writing {
		input, s.input = io.Pipe()
	}
	ready := "dragfm-file-" + randomSuffix() + "\n"
	open := "exec 8<" + shellQuote(target) + " || exit; "
	copy := "exec cat <&8"
	if writing {
		open = "umask 077; set -C; exec 8>" + shellQuote(target) + " || exit; "
		copy = "exec cat >&8"
	}
	script := open + "printf '%s' " + shellQuote(ready) + "; " + copy
	stop := context.AfterFunc(ctx, func() {
		_ = output.CloseWithError(ctx.Err())
		if input != nil {
			_ = input.CloseWithError(ctx.Err())
		}
	})
	go func() {
		options := ExecOptions{Stdout: commandStreamOutput{ctx: ctx, pipe: stdout}}
		if input != nil {
			options.Stdin = commandStreamInput{ctx: ctx, pipe: input}
		}
		s.err = r.Exec(ctx, script, options)
		stop()
		_ = stdout.CloseWithError(s.err)
		if input != nil {
			_ = input.CloseWithError(s.err)
		}
		close(s.done)
		cancel()
	}()
	frame := make([]byte, len(ready))
	_, err := io.ReadFull(output, frame)
	if err == nil && string(frame) != ready {
		err = errors.New("unexpected file readiness frame")
	}
	if err != nil {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		return nil, errors.Join(fmt.Errorf("open remote file %q: %w", target, err), s.wait(stopCtx))
	}
	if writing {
		// cat's stdout goes to fd8. Drain any trailing diagnostics from a
		// session wrapper so SSH completion cannot block on an unread pipe.
		go func() { _, _ = io.Copy(io.Discard, output); _ = output.Close() }()
	}
	return s, nil
}

func (s *commandFileStream) wait(ctx context.Context) error {
	select {
	case <-s.done:
		return s.err
	case <-ctx.Done():
		s.cancel()
	}
	// The executor uses the existing bounded TERM/Wait/KILL SSH path.
	select {
	case <-s.done:
		return s.err
	case <-time.After(4 * time.Second):
		return errors.Join(ctx.Err(), ErrCommandExitUnconfirmed)
	}
}

type commandFileReader struct {
	*commandFileStream
	parent context.Context
	once   sync.Once
	err    error
}

func (r *Remote) openCommandFile(ctx context.Context, target string) (io.ReadCloser, error) {
	stream, err := r.startCommandFile(ctx, target, false)
	if err != nil {
		return nil, err
	}
	return &commandFileReader{commandFileStream: stream, parent: ctx}, nil
}

func (r *commandFileReader) Read(data []byte) (int, error) { return r.output.Read(data) }
func (r *commandFileReader) Close() error {
	r.once.Do(func() {
		select {
		case <-r.done:
			r.err = r.commandFileStream.err
		default:
			// Preflight deliberately opens then closes without reading. Its
			// expected cancellation is not an I/O failure, but unknown exit is.
			r.cancel()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := r.wait(ctx)
			if errors.Is(err, ErrCommandExitUnconfirmed) || r.parent.Err() != nil {
				r.err = errors.Join(r.parent.Err(), err)
			}
		}
		_ = r.output.Close()
	})
	return r.err
}

type commandAtomicWriter struct {
	*commandFileStream
	remote             *Remote
	ctx                context.Context
	temporary, target  string
	mode               fs.FileMode
	done, closed       bool
	closeErr, abortErr error
}

func (r *Remote) createCommandFile(ctx context.Context, temporary, target string, mode fs.FileMode) (AtomicWriter, error) {
	if r.partial != nil {
		if err := r.partial(ctx, temporary, true); err != nil {
			return nil, err
		}
	}
	stream, err := r.startCommandFile(ctx, temporary, true)
	if err != nil {
		return nil, err
	}
	if r.partial != nil {
		if err := r.partial(ctx, temporary, true); err != nil {
			// Ownership was not durably pinned. Stop writing, retain the
			// intent and let the owner report recovery rather than blind rm.
			_ = stream.input.Close()
			return nil, errors.Join(err, stream.wait(ctx))
		}
	}
	return &commandAtomicWriter{commandFileStream: stream, remote: r, ctx: ctx, temporary: temporary, target: target, mode: mode}, nil
}

func (w *commandAtomicWriter) Write(data []byte) (int, error) { return w.input.Write(data) }
func (w *commandAtomicWriter) Close() error {
	if !w.closed {
		w.closed = true
		w.closeErr = w.input.Close()
	}
	return w.closeErr
}
func (w *commandAtomicWriter) Commit() error {
	if w.done {
		return errors.New("atomic writer already completed")
	}
	if err := errors.Join(w.Close(), w.wait(w.ctx)); err != nil {
		return errors.Join(err, w.Abort())
	}
	script := fmt.Sprintf("chmod %04o -- %s && sync -f -- %s", w.mode.Perm(), shellQuote(w.temporary), shellQuote(w.temporary))
	err := w.remote.Exec(w.ctx, script, ExecOptions{})
	if err == nil {
		err = w.remote.Rename(w.ctx, w.temporary, w.target, true)
	}
	if err != nil {
		if errors.Is(err, ErrCommandExitUnconfirmed) {
			w.done = true
			return fmt.Errorf("commit unconfirmed; retained partial %q: %w", w.temporary, err)
		}
		// Rename may succeed but its journal retirement fail. The owner
		// keeps that error; Abort must not remove the published target.
		return errors.Join(err, w.Abort())
	}
	w.done = true
	return nil
}

func (w *commandAtomicWriter) PrepareStaged() (string, error) {
	if w.done {
		return w.temporary, errors.New("atomic writer already completed")
	}
	return w.temporary, errors.Join(w.Close(), w.wait(w.ctx))
}
func (w *commandAtomicWriter) Abort() error {
	if w.done {
		return w.abortErr
	}
	w.done = true
	closeErr := w.Close()
	stopCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	exitErr := w.wait(stopCtx)
	stop()
	if errors.Is(exitErr, ErrCommandExitUnconfirmed) {
		w.abortErr = fmt.Errorf("writer exit unconfirmed; retained partial %q: %w", w.temporary, errors.Join(closeErr, exitErr))
		return w.abortErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	removeErr := w.remote.Remove(ctx, w.temporary, false)
	w.abortErr = errors.Join(closeErr, exitErr, removeErr)
	return w.abortErr
}
