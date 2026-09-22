//go:build !windows

package endpoint

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStalledSFTPRequestIsCancelled(t *testing.T) {
	_, route := startIntegrationSSHServer(t, true)
	upstream := net.JoinHostPort(route.Hops[0].Host, strconv.Itoa(route.Hops[0].Port))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var pause atomic.Bool
	stop := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		client, err := listener.Accept()
		if err != nil {
			return
		}
		server, err := net.Dial("tcp", upstream)
		if err != nil {
			_ = client.Close()
			return
		}
		defer client.Close()
		defer server.Close()
		go func() { <-stop; _ = client.Close(); _ = server.Close() }()
		workers.Add(1)
		go func() { defer workers.Done(); _, _ = io.Copy(client, server) }()
		buffer := make([]byte, 32768)
		for {
			n, readErr := client.Read(buffer)
			if readErr != nil {
				return
			}
			for pause.Load() {
				select {
				case <-stop:
					return
				case <-time.After(5 * time.Millisecond):
				}
			}
			if _, err := server.Write(buffer[:n]); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { close(stop); _ = listener.Close(); workers.Wait() })
	route.Hops[0].Port = listener.Addr().(*net.TCPAddr).Port
	remote, err := DialSSH(context.Background(), "stalled", route.Hops[0].HostKey.PinnedSHA256, route)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	pause.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := remote.List(ctx, t.TempDir()); done <- err }()
	select {
	case err := <-done:
		if err == nil || ctx.Err() == nil || !remote.IsClosed() {
			t.Fatalf("stalled request not cancelled: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SFTP ignored cancellation")
	}
}

func TestCancelledIdleReaderDoesNotCloseOtherSessions(t *testing.T) {
	_, route := startIntegrationSSHServer(t, true)
	remote, err := DialSSH(context.Background(), "fixture", route.Hops[0].HostKey.PinnedSHA256, route)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	file := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader, err := remote.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := reader.Read(make([]byte, 4)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read: %v", err)
	}
	_ = reader.Close()
	if remote.IsClosed() {
		t.Fatal("idle reader cancellation closed an unrelated active endpoint")
	}
	if _, err := remote.Stat(context.Background(), file); err != nil {
		t.Fatal(err)
	}
}
