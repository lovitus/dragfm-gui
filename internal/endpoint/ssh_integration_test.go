//go:build !windows

package endpoint

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type integrationSSHServer struct {
	listener net.Listener
	config   *ssh.ServerConfig
	sftp     bool
	wg       sync.WaitGroup
}

func startIntegrationSSHServer(t *testing.T, enableSFTP bool) (*integrationSSHServer, connector.Route) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(metadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if metadata.User() == "tester" && string(password) == "test-password" {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
		KeyboardInteractiveCallback: func(metadata ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := challenge("", "", []string{"Password:"}, []bool{false})
			if err == nil && metadata.User() == "tester" && len(answers) == 1 && answers[0] == "test-password" {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
	}
	serverConfig.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &integrationSSHServer{listener: listener, config: serverConfig, sftp: enableSFTP}
	server.wg.Add(1)
	go server.serve()
	t.Cleanup(func() {
		_ = listener.Close()
		server.wg.Wait()
	})
	address := listener.Addr().(*net.TCPAddr)
	route := connector.Route{Timeout: 3 * time.Second, Hops: []connector.Hop{{
		Host: "127.0.0.1", Port: address.Port, User: "tester",
		Credentials: connector.Credentials{Password: "test-password"},
		HostKey:     connector.HostKeyPolicy{PinnedSHA256: ssh.FingerprintSHA256(signer.PublicKey())},
	}}}
	return server, route
}

func (s *integrationSSHServer) serve() {
	defer s.wg.Done()
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			serverConnection, channels, requests, err := ssh.NewServerConn(connection, s.config)
			if err != nil {
				_ = connection.Close()
				return
			}
			defer serverConnection.Close()
			go ssh.DiscardRequests(requests)
			for incoming := range channels {
				if incoming.ChannelType() != "session" {
					_ = incoming.Reject(ssh.UnknownChannelType, "unsupported channel")
					continue
				}
				channel, channelRequests, acceptErr := incoming.Accept()
				if acceptErr != nil {
					continue
				}
				s.wg.Add(1)
				go func(channel ssh.Channel, channelRequests <-chan *ssh.Request) {
					defer s.wg.Done()
					s.handleSession(channel, channelRequests)
				}(channel, channelRequests)
			}
		}()
	}
}

func (s *integrationSSHServer) handleSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	ptyRequested := false
	for request := range requests {
		switch request.Type {
		case "pty-req":
			ptyRequested = true
			_ = request.Reply(true, nil)
		case "window-change", "env":
			_ = request.Reply(true, nil)
		case "subsystem":
			if !s.sftp || parseSSHString(request.Payload) != "sftp" {
				_ = request.Reply(false, nil)
				continue
			}
			_ = request.Reply(true, nil)
			server, err := sftp.NewServer(channel)
			if err == nil {
				err = server.Serve()
				_ = server.Close()
			}
			return
		case "exec":
			_ = request.Reply(true, nil)
			command := parseSSHString(request.Payload)
			status := runIntegrationSSHCommand(channel, command, ptyRequested)
			var payload [4]byte
			binary.BigEndian.PutUint32(payload[:], uint32(status))
			_, _ = channel.SendRequest("exit-status", false, payload[:])
			return
		default:
			_ = request.Reply(false, nil)
		}
	}
}

func parseSSHString(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	size := int(binary.BigEndian.Uint32(payload[:4]))
	if size < 0 || 4+size > len(payload) {
		return ""
	}
	return string(payload[4 : 4+size])
}

func runIntegrationSSHCommand(channel ssh.Channel, command string, usePTY bool) int {
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Env = append(os.Environ(), "SHELL=/bin/bash", "TERM=xterm-256color")
	if !usePTY {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = channel, channel, channel.Stderr()
		if err := cmd.Run(); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				return exit.ExitCode()
			}
			return 255
		}
		return 0
	}
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 100})
	if err != nil {
		return 255
	}
	go func() {
		_, _ = io.Copy(file, channel)
		_ = cmd.Process.Signal(os.Interrupt)
	}()
	go func() { _, _ = io.Copy(channel, file) }()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case err = <-waited:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		_ = file.Close()
		return 255
	}
	_ = file.Close()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		return 255
	}
	return 0
}

func TestRemoteSFTPAndSafePOSIXFallback(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "sftp", false: "posix-fallback"}[enabled], func(t *testing.T) {
			if !enabled && runtime.GOOS != "linux" {
				t.Skip("POSIX fallback targets first-version Linux remotes")
			}
			_, route := startIntegrationSSHServer(t, enabled)
			remote, err := DialSSH(context.Background(), "fixture", "fixture-pin", route)
			if err != nil {
				t.Fatal(err)
			}
			defer remote.Close()
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "name with spaces"), []byte("content"), 0640); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("name with spaces", filepath.Join(root, "link")); err != nil {
				t.Fatal(err)
			}
			entries, err := remote.List(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 || entries[0].Name != "link" || entries[0].LinkTarget != "name with spaces" || entries[1].Name != "name with spaces" {
				t.Fatalf("unexpected entries: %#v", entries)
			}
			if enabled && remote.SFTPError() != nil {
				t.Fatalf("SFTP unexpectedly unavailable: %v", remote.SFTPError())
			}
			if !enabled && remote.SFTPError() == nil {
				t.Fatal("SFTP rejection did not activate POSIX fallback")
			}
		})
	}
}

func TestRemotePinnedHostKeyRejectsChange(t *testing.T) {
	_, route := startIntegrationSSHServer(t, true)
	remote, err := DialSSH(context.Background(), "fixture", "fixture-pin", route)
	if err != nil {
		t.Fatalf("correct pin rejected: %v", err)
	}
	_ = remote.Close()
	route.Hops[0].HostKey.PinnedSHA256 = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if _, err := DialSSH(context.Background(), "fixture", "wrong-pin", route); err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("changed host key was not blocked: %v", err)
	}
}

func TestRemotePTYLoadsEnvironmentWithoutVisibleBootstrapAndTracksCWD(t *testing.T) {
	_, route := startIntegrationSSHServer(t, true)
	remote, err := DialSSH(context.Background(), "fixture", "fixture-pin", route)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	home := t.TempDir()
	profile := filepath.Join(home, ".bash_profile")
	bashrc := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(profile, []byte("export DRAGFM_REMOTE_LOGIN=loaded\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bashrc, []byte("PS1='remote-test> '\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	probeDirectory := "/tmp/.dragfm-shell-probe-" + randomSuffix()
	if err := remote.MkdirAll(context.Background(), probeDirectory, 0700); err != nil {
		t.Fatalf("probe mkdir: %v", err)
	}
	if _, err := remote.sftp.Stat(probeDirectory); err != nil {
		t.Fatalf("probe stat after mkdir: %v", err)
	}
	writer, err := remote.CreateAtomic(context.Background(), filepath.Join(probeDirectory, "probe"), 0600)
	if err != nil {
		t.Fatalf("probe create: %v", err)
	}
	_, _ = writer.Write([]byte("probe"))
	if err := writer.Commit(); err != nil {
		t.Fatalf("probe commit: %v", err)
	}
	_ = remote.Remove(context.Background(), probeDirectory, true)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	session, err := remote.OpenPTY(ctx, home, "/bin/bash", 24, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result := make(chan []byte, 1)
	go func() {
		buffer := make([]byte, 4096)
		var output []byte
		for len(output) < 128*1024 {
			count, readErr := session.Output().Read(buffer)
			output = append(output, buffer[:count]...)
			if bytes.Contains(output, []byte("REMOTE=loaded")) || readErr != nil {
				break
			}
			if bytes.Contains(output, []byte("\x1b]777;dragfm-cwd=")) && !bytes.Contains(output, []byte("printf 'REMOTE=")) {
				_, _ = session.Input().Write([]byte("printf 'REMOTE=%s\\n' \"$DRAGFM_REMOTE_LOGIN\"\n"))
			}
		}
		result <- output
	}()
	select {
	case output := <-result:
		if !bytes.Contains(output, []byte("REMOTE=loaded")) {
			t.Fatalf("remote login environment missing: %q", output)
		}
		for _, leaked := range []string{"__dragfm_emit_cwd", "dragfm-shell-", "exec /bin/bash -l"} {
			if strings.Contains(string(output), leaked) {
				t.Fatalf("remote bootstrap leaked (%s): %q", leaked, output)
			}
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
