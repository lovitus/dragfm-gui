package agentservice

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/agentproto"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

func TestHansPassphraseUsesInheritedDescriptor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Hans server is Linux-only")
	}
	root := t.TempDir()
	capture := filepath.Join(root, "captured")
	binary := filepath.Join(root, "fake-hans")
	script := "#!/bin/sh\n" +
		"for value in \"$@\"; do [ \"$value\" = \"--show-identity\" ] && { echo 'fingerprint test-pin'; exit 0; }; done\n" +
		"previous=\npassfile=\nfor value in \"$@\"; do [ \"$previous\" = \"--passphrase-file\" ] && passfile=\"$value\"; previous=\"$value\"; done\n" +
		"cat \"$passfile\" > " + shellQuoteForTest(capture) + "\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	service := New()
	fingerprint, err := service.startHans(agentproto.Request{
		Action:  "hans-server-start",
		Options: map[string]string{"binary": binary, "identity": filepath.Join(root, "identity"), "job": "hans-test", "network": "10.120.4.0", "lease": filepath.Join(root, "lease")},
		Secret:  map[string]string{"passphrase": "descriptor-only-secret"},
	})
	if err != nil || fingerprint != "test-pin" {
		t.Fatalf("fingerprint=%q err=%v", fingerprint, err)
	}
	defer service.stopProcess("hans-test")
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, readErr := os.ReadFile(capture)
		if readErr == nil && string(data) == "descriptor-only-secret\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fake Hans did not receive descriptor secret: data=%q err=%v", data, readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestArchiveRoundTripPreservesTreeAndSymlink(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "file"), []byte("encrypted-stream"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nested/file", filepath.Join(source, "link")); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := sendArchive(&archive, source); err != nil {
		t.Fatal(err)
	}
	if err := receiveArchive(&archive, target); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(target, "nested", "file"))
	if err != nil || string(data) != "encrypted-stream" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	link, err := os.Readlink(filepath.Join(target, "link"))
	if err != nil || link != "nested/file" {
		t.Fatalf("link=%q err=%v", link, err)
	}
}

func TestPinnedTLSDirectStream(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("tls-stream"), 0600); err != nil {
		t.Fatal(err)
	}
	receiver := New()
	listen := receiver.Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "listen", Action: "listen-receive", Options: map[string]string{"job": "one", "path": target, "bind": "127.0.0.1"}, Secret: map[string]string{"token": "one-use-secret"}})
	if !listen.OK {
		t.Fatal(listen.Error)
	}
	port, err := strconv.Atoi(listen.Values["port"])
	if err != nil {
		t.Fatal(err)
	}
	sender := New()
	connected := sender.Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "send", Action: "connect-send", Options: map[string]string{"address": "127.0.0.1:" + strconv.Itoa(port), "path": source}, Secret: map[string]string{"token": "one-use-secret", "pin": listen.Values["pin"]}})
	if !connected.OK {
		t.Fatal(connected.Error)
	}
	waited := receiver.Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "wait", Action: "wait", Options: map[string]string{"job": "one"}})
	if !waited.OK {
		t.Fatal(waited.Error)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "tls-stream" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func TestPinnedTLSDirectStreamViaAuthenticatedSOCKS(t *testing.T) {
	proxy := startTestSOCKS5(t, "proxy-user", "proxy-pass")
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "target")
	if err := os.WriteFile(source, []byte("tls-via-socks"), 0600); err != nil {
		t.Fatal(err)
	}
	receiver := New()
	listen := receiver.Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "listen", Action: "listen-receive", Options: map[string]string{"job": "socks", "path": target, "bind": "127.0.0.1"}, Secret: map[string]string{"token": "socks-token"}})
	if !listen.OK {
		t.Fatal(listen.Error)
	}
	connected := New().Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "send", Action: "connect-send", Options: map[string]string{"address": "127.0.0.1:" + listen.Values["port"], "path": source}, Secret: map[string]string{"token": "socks-token", "pin": listen.Values["pin"], "socks_address": proxy, "socks_username": "proxy-user", "socks_password": "proxy-pass"}})
	if !connected.OK {
		t.Fatal(connected.Error)
	}
	if waited := receiver.Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "wait", Action: "wait", Options: map[string]string{"job": "socks"}}); !waited.OK {
		t.Fatal(waited.Error)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "tls-via-socks" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

func startTestSOCKS5(t *testing.T, username, password string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			client, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer client.Close()
				var greeting [2]byte
				if _, err := io.ReadFull(client, greeting[:]); err != nil || greeting[0] != 5 {
					return
				}
				methods := make([]byte, int(greeting[1]))
				if _, err := io.ReadFull(client, methods); err != nil {
					return
				}
				_, _ = client.Write([]byte{5, 2})
				var auth [2]byte
				if _, err := io.ReadFull(client, auth[:]); err != nil || auth[0] != 1 {
					return
				}
				user := make([]byte, int(auth[1]))
				if _, err := io.ReadFull(client, user); err != nil {
					return
				}
				var passLength [1]byte
				if _, err := io.ReadFull(client, passLength[:]); err != nil {
					return
				}
				pass := make([]byte, int(passLength[0]))
				if _, err := io.ReadFull(client, pass); err != nil || string(user) != username || string(pass) != password {
					_, _ = client.Write([]byte{1, 1})
					return
				}
				_, _ = client.Write([]byte{1, 0})
				var request [4]byte
				if _, err := io.ReadFull(client, request[:]); err != nil || request[0] != 5 || request[1] != 1 {
					return
				}
				var host string
				switch request[3] {
				case 1:
					value := make([]byte, 4)
					_, _ = io.ReadFull(client, value)
					host = net.IP(value).String()
				case 3:
					var length [1]byte
					_, _ = io.ReadFull(client, length[:])
					value := make([]byte, int(length[0]))
					_, _ = io.ReadFull(client, value)
					host = string(value)
				default:
					return
				}
				var portBytes [2]byte
				if _, err := io.ReadFull(client, portBytes[:]); err != nil {
					return
				}
				upstream, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(portBytes[:])))))
				if err != nil {
					return
				}
				defer upstream.Close()
				_, _ = client.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				go func() { _, _ = io.Copy(upstream, client) }()
				_, _ = io.Copy(client, upstream)
			}()
		}
	}()
	return listener.Addr().String()
}

func TestDataChannelAuthenticationNeverConsumesPayloadBytes(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	payload := []byte{0x1f, 0x8b, 0x08, 0x00, 'd', 'a', 't', 'a'}
	written := make(chan error, 1)
	acknowledged := make(chan error, 1)
	go func() {
		_, err := client.Write(append([]byte("one-use-token\n"), payload...))
		written <- err
	}()
	go func() {
		var acknowledgement [1]byte
		_, err := io.ReadFull(client, acknowledgement[:])
		acknowledged <- err
	}()
	if err := authenticateDataChannel(server, "one-use-token"); err != nil {
		t.Fatal(err)
	}
	actual := make([]byte, len(payload))
	if _, err := io.ReadFull(server, actual); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, payload) {
		t.Fatalf("payload changed: %x want %x", actual, payload)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if err := <-acknowledged; err != nil {
		t.Fatal(err)
	}
}

func TestReceiveArchiveRejectsExistingRoot(t *testing.T) {
	root := t.TempDir()
	var archive bytes.Buffer
	if err := sendArchive(&archive, root); err != nil {
		t.Fatal(err)
	}
	if err := receiveArchive(&archive, root); err == nil {
		t.Fatal("existing target accepted")
	}
}

func TestElevatedCommitMergesAndReturnsStrongManifest(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	partial := filepath.Join(root, ".partial")
	if err := os.MkdirAll(filepath.Join(target, "kept"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "kept", "old"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(partial, "kept"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "kept", "new"), []byte("new"), 0640); err != nil {
		t.Fatal(err)
	}
	service := New()
	commit := service.Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "commit", Action: "path-commit", Options: map[string]string{"partial": partial, "target": target, "overwrite": "true"}})
	if !commit.OK {
		t.Fatal(commit.Error)
	}
	for _, name := range []string{"old", "new"} {
		if _, err := os.Stat(filepath.Join(target, "kept", name)); err != nil {
			t.Fatalf("merged entry %s missing: %v", name, err)
		}
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("partial remains after commit: %v", err)
	}
	response := service.Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "manifest", Action: "filesystem-manifest", Options: map[string]string{"path": target}})
	if !response.OK {
		t.Fatal(response.Error)
	}
	var manifest transfer.Manifest
	if err := json.Unmarshal([]byte(response.Values["manifest"]), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Items) != 4 || manifest.Items[2].SHA256 == "" || manifest.Items[3].SHA256 == "" {
		t.Fatalf("unexpected strong manifest: %#v", manifest)
	}
}

func TestCommitRefusesOverwriteWithoutExplicitChoice(t *testing.T) {
	root := t.TempDir()
	partial, target := filepath.Join(root, ".partial"), filepath.Join(root, "target")
	if err := os.WriteFile(partial, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	response := New().Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "commit", Action: "path-commit", Options: map[string]string{"partial": partial, "target": target, "overwrite": "false"}})
	if response.OK {
		t.Fatal("existing target was overwritten without approval")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "old" {
		t.Fatalf("target changed: %q %v", data, err)
	}
}

func TestCleanupStaleTempsRequiresValidExpiredOwnershipMarker(t *testing.T) {
	root := t.TempDir()
	makeDirectory := func(name string, marker any, mode os.FileMode) string {
		directory := filepath.Join(root, name)
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if marker != nil {
			data, _ := json.Marshal(marker)
			if err := os.WriteFile(filepath.Join(directory, ".dragfm-owner-v1"), data, mode); err != nil {
				t.Fatal(err)
			}
		}
		return directory
	}
	old := ownershipMarker{Version: 1, Created: time.Now().Add(-48 * time.Hour), Nonce: "old"}
	recent := ownershipMarker{Version: 1, Created: time.Now(), Nonce: "recent"}
	valid := makeDirectory(".dragfm-old", old, 0600)
	keep := makeDirectory(".dragfm-keep", ownershipMarker{Version: 1, Created: old.Created, Nonce: "keep"}, 0600)
	invalid := makeDirectory(".dragfm-invalid", ownershipMarker{Version: 1, Created: old.Created, Nonce: "different"}, 0600)
	worldReadable := makeDirectory(".dragfm-world", ownershipMarker{Version: 1, Created: old.Created, Nonce: "world"}, 0644)
	recentPath := makeDirectory(".dragfm-recent", recent, 0600)
	if err := cleanupOwnedTemps(root, keep, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(valid); !os.IsNotExist(err) {
		t.Fatalf("valid stale directory remains: %v", err)
	}
	for _, preserved := range []string{keep, invalid, worldReadable, recentPath} {
		if _, err := os.Stat(preserved); err != nil {
			t.Fatalf("unsafe cleanup removed %s: %v", preserved, err)
		}
	}
}

func TestCancelledServiceClosesPendingListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	service := NewWithContext(ctx)
	job := "cancel-listener"
	response := service.Handle(agentproto.Request{
		Version: agentproto.ProtocolVersion,
		ID:      "listen",
		Action:  "listen-receive",
		Options: map[string]string{"job": job, "path": filepath.Join(t.TempDir(), "incoming")},
		Secret:  map[string]string{"token": "one-use-token"},
	})
	if !response.OK {
		t.Fatalf("start listener: %#v", response)
	}
	cancel()
	wait := service.Handle(agentproto.Request{Version: agentproto.ProtocolVersion, ID: "wait", Action: "wait", Options: map[string]string{"job": job}})
	if wait.OK || !strings.Contains(wait.Error, context.Canceled.Error()) {
		t.Fatalf("cancelled listener response=%#v", wait)
	}
}

func TestSOCKSDialHonorsContextWhileProxyIsSilent(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = dialSOCKSContext(ctx, listener.Addr().String(), "127.0.0.1:22", "user", "password")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SOCKS cancellation error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("SOCKS cancellation took %s", elapsed)
	}
	select {
	case connection := <-accepted:
		_ = connection.Close()
	case <-time.After(time.Second):
		t.Fatal("silent proxy was not contacted")
	}
}
