// Package boundedbuf retains a synchronized diagnostic tail, not an unbounded log.
package boundedbuf

import "sync"

type Buffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 16384
	n := len(p)
	if n >= limit {
		b.data = append(b.data[:0], p[n-limit:]...)
		return n, nil
	}
	if len(b.data)+n > limit {
		b.data = b.data[len(b.data)+n-limit:]
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *Buffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return string(b.data) }
func (b *Buffer) Len() int       { b.mu.Lock(); defer b.mu.Unlock(); return len(b.data) }
