// Package activity reports measured transport activity, not fabricated file
// completion. Wire bytes include protocol overhead and are never divided by a
// source-file size to manufacture a progress percentage.
package activity

import (
	"context"
	"net"
	"sync/atomic"
	"time"
)

type stateKey struct{}
type State struct {
	bytes    atomic.Int64
	last     atomic.Int64
	observer func(string, int64)
}

func WithObserver(ctx context.Context, observer func(string, int64)) (context.Context, *State) {
	state := &State{observer: observer}
	return context.WithValue(ctx, stateKey{}, state), state
}
func (s *State) Bytes() int64 { return s.bytes.Load() }
func Report(ctx context.Context, stage string, bytes int64) {
	if s, ok := ctx.Value(stateKey{}).(*State); ok && s.observer != nil {
		s.observer(stage, bytes)
	}
}
func Add(ctx context.Context, n int) {
	if s, ok := ctx.Value(stateKey{}).(*State); ok {
		total := s.bytes.Add(int64(n))
		now, last := time.Now().UnixNano(), s.last.Load()
		if now-last >= int64(500*time.Millisecond) && s.last.CompareAndSwap(last, now) && s.observer != nil {
			s.observer("transport", total)
		}
	}
}

type measuredConn struct {
	net.Conn
	ctx context.Context
}

func Conn(ctx context.Context, connection net.Conn) net.Conn { return &measuredConn{connection, ctx} }
func (c *measuredConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	Add(c.ctx, n)
	return n, err
}
func (c *measuredConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	Add(c.ctx, n)
	return n, err
}
func (c *measuredConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return nil
}
