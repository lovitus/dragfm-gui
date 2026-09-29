package endpoint

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"time"
)

// SFTP has no per-request cancellation API. Only a request actually in flight
// arms transport cancellation; idle open files do not tear down the connection.
// This ensures Lock cannot wait forever for a silent peer. A closed endpoint is
// recognized by the application's existing reconnect/generation guard.
func (r *Remote) watchIO(ctx context.Context) func() {
	if r.commands != nil {
		// Command-backed views cancel their own sessions and pipes. A read
		// timeout must not close the shared SSH identity or prevent cleanup.
		return func() {}
	}
	stop := context.AfterFunc(ctx, func() { _ = r.Close() })
	return func() { stop() }
}

func (r *Remote) Open(ctx context.Context, target string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stop := r.watchIO(ctx)
	reader, err := r.open(ctx, target)
	stop()
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), err)
		}
		return nil, err
	}
	return &contextReader{ReadCloser: reader, remote: r, ctx: ctx}, nil
}

type contextReader struct {
	io.ReadCloser
	remote *Remote
	ctx    context.Context
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	stop := r.remote.watchIO(r.ctx)
	defer stop()
	n, err := r.ReadCloser.Read(data)
	if err != nil && r.ctx.Err() != nil {
		err = errors.Join(r.ctx.Err(), err)
	}
	return n, err
}
func (r *contextReader) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stop := r.remote.watchIO(ctx)
	defer stop()
	return r.ReadCloser.Close()
}

func (r *Remote) CreateAtomic(ctx context.Context, target string, mode fs.FileMode) (AtomicWriter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stop := r.watchIO(ctx)
	writer, err := r.createAtomic(ctx, target, mode)
	stop()
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), err)
		}
		return nil, err
	}
	return &contextAtomicWriter{AtomicWriter: writer, remote: r, ctx: ctx}, nil
}

type contextAtomicWriter struct {
	AtomicWriter
	remote *Remote
	ctx    context.Context
}

func (w *contextAtomicWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	stop := w.remote.watchIO(w.ctx)
	defer stop()
	n, err := w.AtomicWriter.Write(data)
	if err != nil && w.ctx.Err() != nil {
		err = errors.Join(w.ctx.Err(), err)
	}
	return n, err
}
func (w *contextAtomicWriter) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stop := w.remote.watchIO(ctx)
	defer stop()
	return w.AtomicWriter.Close()
}
func (w *contextAtomicWriter) Commit() error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	stop := w.remote.watchIO(w.ctx)
	defer stop()
	err := w.AtomicWriter.Commit()
	if err != nil && w.ctx.Err() != nil {
		err = errors.Join(w.ctx.Err(), err)
	}
	return err
}

func (w *contextAtomicWriter) PrepareStaged() (string, error) {
	prepared, ok := w.AtomicWriter.(StagedWriter)
	if !ok {
		return "", errors.ErrUnsupported
	}
	stop := w.remote.watchIO(w.ctx)
	defer stop()
	path, err := prepared.PrepareStaged()
	return path, errors.Join(w.ctx.Err(), err)
}
func (w *contextAtomicWriter) Abort() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stop := w.remote.watchIO(ctx)
	defer stop()
	return w.AtomicWriter.Abort()
}
