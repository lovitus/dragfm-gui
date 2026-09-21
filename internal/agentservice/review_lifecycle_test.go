package agentservice

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/agentproto"
)

func TestServeReportsRemoteActivityAndCleansOnControlEOF(t *testing.T) {
	front, back := net.Pipe()
	defer front.Close()
	defer back.Close()
	done := make(chan error, 1)
	go func() {
		p, err := agentproto.Server(back, back)
		if err != nil {
			done <- err
			return
		}
		done <- Serve(context.Background(), p)
	}()
	protocol, err := agentproto.Client(front, front)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	partial := filepath.Join(root, "file.dragfm-partial-0123456789abcdef")
	request := agentproto.Request{Version: 1, ID: "listen", Action: "listen-receive", Options: map[string]string{"job": "fixture", "path": partial, "bind": "127.0.0.1"}, Secret: map[string]string{"token": "fixture-secret"}}
	if err := protocol.Send(request); err != nil {
		t.Fatal(err)
	}
	var reply agentproto.Response
	if err := protocol.Receive(&reply); err != nil || !reply.OK {
		t.Fatalf("listen: %+v %v", reply, err)
	}
	port, err := strconv.Atoi(reply.Values["port"])
	if err != nil || port < 20000 || port > 60999 {
		t.Fatalf("port out of policy: %d %v", port, err)
	}
	if reply.Values["addresses"] != "127.0.0.1" {
		t.Fatal("listener not concretely bound")
	}
	if err := os.WriteFile(partial, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := protocol.Send(agentproto.Request{Version: 1, ID: "wait", Action: "wait", Options: map[string]string{"job": "fixture", "progress": "true"}}); err != nil {
		t.Fatal(err)
	}
	if err := front.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := protocol.Receive(&reply); err != nil || !reply.Progress || reply.ID != "wait" {
		t.Fatalf("real helper heartbeat missing: %+v %v", reply, err)
	}
	_ = front.Close()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("EOF did not cancel active helper")
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("owned partial survived EOF: %v", err)
	}
}

func TestOwnedPartialRejectsPreexistingPath(t *testing.T) {
	target := filepath.Join(t.TempDir(), "file.dragfm-partial-0123456789abcdef")
	if err := os.WriteFile(target, []byte("not-owned"), 0600); err != nil {
		t.Fatal(err)
	}
	service := New()
	if err := service.trackPartial(target); !os.IsExist(err) {
		t.Fatalf("existing file adopted: %v", err)
	}
	service.Close()
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "not-owned" {
		t.Fatalf("unowned target lost: %q %v", data, err)
	}
}

func TestRealNcatCarriesPinnedTLSAndStops(t *testing.T) {
	if _, err := exec.LookPath("ncat"); err != nil {
		t.Skip("ncat fixture tool unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	source, target := filepath.Join(t.TempDir(), "source"), filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(source, []byte("authenticated-over-real-ncat"), 0600); err != nil {
		t.Fatal(err)
	}
	receiver := NewWithContext(ctx)
	defer receiver.Close()
	reply := receiver.Handle(agentproto.Request{Version: 1, ID: "listen", Action: "listen-receive", Options: map[string]string{"job": "ncat", "path": target, "bind": "127.0.0.1", "ack": "true"}, Secret: map[string]string{"token": "secret-never-in-argv"}})
	if !reply.OK {
		t.Fatal(reply.Error)
	}
	sender := NewWithContext(ctx)
	defer sender.Close()
	sent := sender.Handle(agentproto.Request{Version: 1, ID: "connect", Action: "connect-send", Options: map[string]string{"address": net.JoinHostPort(reply.Values["addresses"], reply.Values["port"]), "path": source, "carrier": "ncat", "ack": "true"}, Secret: map[string]string{"token": "secret-never-in-argv", "pin": reply.Values["pin"]}})
	if !sent.OK {
		t.Fatal(sent.Error)
	}
	if err := receiver.wait(ctx, "ncat"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "authenticated-over-real-ncat" {
		t.Fatalf("payload %q: %v", data, err)
	}
}

func TestCleanupRemovesOwnedReadOnlyTreeWithoutFollowingLinks(t *testing.T) {
	root := t.TempDir()
	partial := filepath.Join(root, "x.dragfm-partial-0123456789abcdef")
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	service := New()
	if err := service.trackPartial(partial); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(partial, "readonly"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "readonly", "file"), []byte("partial"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(partial, "link")); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(partial, "readonly"), 0000)
	os.Chmod(partial, 0500)
	service.Close()
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("readonly partial survived: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Fatal("cleanup followed symlink")
	}
}
