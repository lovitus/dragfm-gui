package agentservice

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/agentproto"
)

func TestHansChildFailureIsObservedAndRedacted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX child fixture")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "hans-fixture")
	script := "#!/bin/sh\nfor arg in \"$@\"; do [ \"$arg\" = --show-identity ] && { echo fixture-fingerprint; exit 0; }; done\nprintf 'fixture failure: '; cat <&3\nexit 23\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	service := New()
	_, failure := service.startHans(agentproto.Request{Action: "hans-server-start", Options: map[string]string{
		"binary": binary, "identity": filepath.Join(root, "key"), "network": "10.251.1.0", "lease": filepath.Join(root, "leases"), "job": "early-exit",
	}, Secret: map[string]string{"passphrase": "fixture-secret-must-not-leak"}})
	defer service.stopProcess("early-exit")
	deadline := time.Now().Add(2 * time.Second)
	for failure == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		failure = service.processStatus("early-exit")
	}
	if failure == nil || !strings.Contains(failure.Error(), "fixture failure") {
		t.Fatalf("missing failure diagnostics: %v", failure)
	}
	if strings.Contains(failure.Error(), "fixture-secret-must-not-leak") {
		t.Fatal("child diagnostics exposed the descriptor secret")
	}
	if err := service.stopProcess("early-exit"); err != nil {
		t.Fatal(err)
	}
}

func TestProcessDiagnosticsRemainBounded(t *testing.T) {
	var output processOutput
	_, _ = output.Write([]byte(strings.Repeat("x", 100*processOutputLimit)))
	_, _ = output.Write([]byte("tail"))
	text := output.text(nil)
	if len(text) != processOutputLimit || !strings.HasSuffix(text, "tail") {
		t.Fatal("diagnostics buffer exceeded its limit or lost the tail")
	}
}

func TestSOCKSReadinessDoesNotAcceptAnOpenListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		var greeting [3]byte
		if _, err := io.ReadFull(conn, greeting[:]); err != nil {
			return
		}
		if _, err := conn.Write([]byte{5, 0}); err != nil {
			return
		}
		var request [10]byte
		if _, err := io.ReadFull(conn, request[:]); err != nil {
			return
		}
		_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
	}()
	if err := probeSOCKSTCP(context.Background(), listener.Addr().String(), "10.251.1.1:22"); err == nil {
		t.Fatal("a SOCKS listener that rejects CONNECT was treated as a working tunnel")
	}
	<-done
}

func TestSOCKSReadinessCancellationClosesBlockedHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := probeSOCKSTCP(ctx, listener.Addr().String(), "10.251.1.1:22"); err == nil {
		t.Fatal("blocked handshake succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("readiness did not honor cancellation")
	}
	<-done
}
