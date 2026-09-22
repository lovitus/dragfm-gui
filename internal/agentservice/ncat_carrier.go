package agentservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"time"
)

// ncatCarrier transports bytes through an actual ncat subprocess. The network
// connection has already been established directly, through SOCKS, through an
// SSH chain, or through Hans. A one-use loopback bridge keeps all route secrets
// in the helper; ncat argv contains only a loopback address and port. The caller
// runs pinned TLS over the returned connection, so even a racing local process
// can only deny service, not impersonate the authenticated peer or read data.
func ncatCarrier(ctx context.Context, upstream net.Conn) (net.Conn, error) {
	binary, err := exec.LookPath("ncat")
	if err != nil {
		_ = upstream.Close()
		return nil, errors.New("ncat is not installed on the initiating endpoint")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		_ = upstream.Close()
		return nil, err
	}
	life, cancel := context.WithCancel(ctx)
	front, back := net.Pipe()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	command := exec.CommandContext(life, binary, host, port)
	configureChildLifecycle(command)
	command.Stdin, command.Stdout = back, back
	diagnostic := &processOutput{}
	command.Stderr = diagnostic
	command.WaitDelay = time.Second
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			cancel()
			_ = listener.Close()
			_ = upstream.Close()
			_ = front.Close()
			_ = back.Close()
		})
	}
	stop := context.AfterFunc(life, cleanup)
	if err := command.Start(); err != nil {
		stop()
		cleanup()
		return nil, fmt.Errorf("start ncat: %w", err)
	}
	done := make(chan struct{})
	go func() { _ = command.Wait(); cleanup(); close(done) }()
	go func() {
		socket, err := listener.Accept()
		_ = listener.Close()
		if err != nil {
			cleanup()
			return
		}
		defer socket.Close()
		toPeer := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, socket)
			if tcp, ok := upstream.(interface{ CloseWrite() error }); ok {
				_ = tcp.CloseWrite()
			}
			close(toPeer)
		}()
		_, _ = io.Copy(socket, upstream)
		_ = socket.Close()
		_ = upstream.Close()
		<-toPeer
	}()
	return &carrierConn{Conn: front, stop: stop, cleanup: cleanup, done: done}, nil
}

type carrierConn struct {
	net.Conn
	stop    func() bool
	cleanup func()
	done    <-chan struct{}
}

func (c *carrierConn) Close() error {
	c.stop()
	// EOF on stdin is a graceful drain, not an immediate SIGKILL. Killing ncat
	// immediately after writing TLS close_notify can discard its buffered bytes.
	_ = c.Conn.Close()
	select {
	case <-c.done:
		c.cleanup()
		return nil
	case <-time.After(2 * time.Second):
		c.cleanup()
		select {
		case <-c.done:
			return nil
		case <-time.After(time.Second):
			return errors.New("ncat shutdown timed out")
		}
	}
}
