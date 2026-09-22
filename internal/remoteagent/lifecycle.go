package remoteagent

import (
	"context"
	"errors"
	"io/fs"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

func (s *Session) CallContext(ctx context.Context, action string, options, secret map[string]string) (map[string]string, error) {
	if s == nil {
		return nil, errors.New("remote helper is not running")
	}
	select {
	case <-s.done:
		return nil, errors.New("remote helper is closed")
	default:
	}
	if ctx == nil {
		ctx = context.Background()
	}
	merged, cancel := context.WithCancel(ctx)
	defer cancel()
	if s.ctx != nil {
		stop := context.AfterFunc(s.ctx, cancel)
		defer stop()
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
	}
	return s.call(merged, action, options, secret)
}

func (s *Session) abortTransport() {
	if s.ssh == nil {
		return
	}
	s.abortOnce.Do(func() {
		closed := make(chan struct{})
		go func() {
			if channel, ok := s.ssh.(interface{ Signal(ssh.Signal) error }); ok {
				signalled := make(chan struct{})
				go func() { _ = channel.Signal(ssh.SIGTERM); close(signalled) }()
				select {
				case <-signalled:
				case <-time.After(100 * time.Millisecond):
				}
			}
			_ = s.ssh.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(300 * time.Millisecond):
			// A peer that does not consume channel writes can block channel Close too.
			// Closing the owning transport unblocks those writes; browsing reconnects.
			if s.remote != nil {
				_ = s.remote.Close()
			}
		}
	})
}

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		if s.done != nil {
			close(s.done)
		}
		var cleanupErr error
		if s.elevated {
			// Install cancellation before waiting on the protocol mutex. A blocked
			// transfer must not make privileged cleanup (and Lock) wait forever.
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, cleanupErr = s.call(ctx, "remove-owned-temp", map[string]string{"path": s.Directory}, nil)
			cancel()
		}
		s.abortTransport()
		var stdinErr, removeErr error
		if s.stdin != nil {
			stdinErr = s.stdin.Close()
		}
		if s.remote != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			removeErr = s.remote.Remove(ctx, s.Directory, true)
			if errors.Is(removeErr, fs.ErrNotExist) {
				removeErr = nil
			}
			cancel()
		}
		s.closeErr = errors.Join(cleanupErr, stdinErr, removeErr)
	})
	return s.closeErr
}

// SSH may write stderr concurrently with an early protocol/handshake failure.
// Keep a bounded tail, with synchronized String, instead of an unguarded Buffer.
type helperOutput struct {
	mu   sync.Mutex
	tail []byte
}

func (b *helperOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 16 << 10
	n := len(data)
	if n >= limit {
		b.tail = append(b.tail[:0], data[n-limit:]...)
		return n, nil
	}
	if len(b.tail)+n > limit {
		b.tail = b.tail[len(b.tail)+n-limit:]
	}
	b.tail = append(b.tail, data...)
	return n, nil
}
func (b *helperOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.tail) }
