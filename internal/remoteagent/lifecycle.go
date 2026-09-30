package remoteagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
		}
		// Even a successful CHANNEL_CLOSE write can leave local readers
		// waiting for a peer reply. This is the task's private transport;
		// the browser/PTY has a separate connection and is not closed here.
		if s.remote != nil {
			_ = s.remote.Close()
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
		s.filesMu.Lock()
		fileErr := s.filesErr
		for _, files := range s.files {
			fileErr = errors.Join(fileErr, files.Close())
		}
		s.files = nil
		s.sudoPassword = ""
		s.filesMu.Unlock()
		var cleanupErr error
		if fileErr == nil {
			// Install cancellation before waiting on the protocol mutex. A blocked
			// transfer must not make privileged cleanup (and Lock) wait forever.
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_, cleanupErr = s.call(ctx, "remove-owned-temp", map[string]string{"path": s.Directory}, nil)
			cancel()
		}
		s.abortTransport()
		if s.stdin != nil {
			_ = s.stdin.Close() // Already closed by transport; not exit evidence.
		}
		if err := errors.Join(fileErr, cleanupErr); err != nil {
			s.closeErr = fmt.Errorf("helper cleanup unconfirmed; installation retained for safe recovery: %w", err)
		} else if s.journal != nil {
			// A failed vault write keeps the record, even though the directory
			// is gone. Reconnection can confirm absence and retire it safely.
			s.closeErr = s.journal(s.installation, true)
		}
	})
	return s.closeErr
}

// SSH may write stderr concurrently with an early protocol/handshake failure.
// Keep a bounded tail, with synchronized String, instead of an unguarded Buffer.
type helperOutput struct {
	mu      sync.Mutex
	tail    []byte
	changed chan struct{}
}

func (b *helperOutput) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	defer func() {
		if b.changed != nil {
			close(b.changed)
			b.changed = nil
		}
	}()
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

func (b *helperOutput) waitFor(ctx context.Context, marker string) error {
	for {
		b.mu.Lock()
		if strings.Contains(string(b.tail), marker) {
			b.mu.Unlock()
			return nil
		}
		if b.changed == nil {
			b.changed = make(chan struct{})
		}
		changed := b.changed
		b.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
