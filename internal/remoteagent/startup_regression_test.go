//go:build linux

package remoteagent

import (
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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/agentproto"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// A real SSH peer accepts the exec request and consumes the actual protocol
// hello, then loses its outbound traffic (including reciprocal CHANNEL_CLOSE).
// The client can still write CLOSE. This is the read-stall window, not a mock
// Endpoint or a slow TCP dial. Ordinary filesystem/probe commands run for real.
func TestOwnedHelperCancellationUnblocksSilentSSH(t *testing.T) {
	for _, mode := range []string{"control-start", "file-start-cancel", "file-start-close", "control-call", "system-start"} {
		t.Run(mode, func(t *testing.T) {
			var recordMu sync.Mutex
			var records []config.WorkspaceRecord
			// Registered before the peer cleanup, so removal runs only AFTER
			// fixture commands/connections have stopped. Every path came from
			// this test's own creation journal; no temporary-directory scan.
			t.Cleanup(func() {
				recordMu.Lock()
				defer recordMu.Unlock()
				for _, record := range records {
					if filepath.Dir(record.Path) != "/tmp" || !strings.HasPrefix(filepath.Base(record.Path), ".dragfm-") {
						t.Error("invalid fixture cleanup path")
						continue
					}
					if err := os.RemoveAll(record.Path); err != nil {
						t.Error(err)
					}
				}
			})
			route, reached := silentStartupPeer(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			browser, err := endpoint.DialSSH(ctx, "browser", "", route)
			if err != nil {
				t.Fatal(err)
			}
			defer browser.Close()
			owned, err := browser.Fork(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer owned.Close()
			call, stop := context.WithCancel(ctx)
			defer stop()
			result := make(chan error, 1)
			closed := make(chan error, 1)
			var helper *Session
			retired := false
			journal := func(record config.WorkspaceRecord, remove bool) error {
				recordMu.Lock()
				defer recordMu.Unlock()
				if remove {
					retired = true
				} else {
					records = append(records, record)
				}
				return nil
			}
			if strings.HasPrefix(mode, "file-") || mode == "control-call" {
				channel, err := owned.SSHClient().NewSession()
				if err != nil {
					t.Fatal(err)
				}
				input, err := channel.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				output, err := channel.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := channel.Start("dragfm-fixture-control"); err != nil {
					t.Fatal(err)
				}
				protocol, err := agentproto.Client(output, input)
				if err != nil {
					t.Fatal(err)
				}
				helper = &Session{Protocol: protocol, remote: owned, ssh: channel, stdin: input,
					ctx: ctx, done: make(chan struct{}), Directory: t.TempDir(), journal: journal}
				defer helper.Close()
			}
			go func() {
				switch mode {
				case "control-start":
					session, err := Start(call, owned, runtime.GOARCH, journal)
					if session != nil {
						err = errors.Join(err, session.Close())
					}
					result <- err
				case "system-start":
					files, err := OpenSystemFiles(call, owned, false, "", journal)
					if files != nil {
						err = errors.Join(err, files.Close())
					}
					result <- err
				case "control-call":
					_, err := helper.CallContext(call, "probe", nil, nil)
					result <- err
				default:
					files, err := helper.OpenFiles(call)
					if files != nil {
						err = errors.Join(err, files.Close())
					}
					result <- err
				}
			}()
			select {
			case <-reached:
			case err := <-result:
				t.Fatalf("fixture did not reach actual protocol input: %v", err)
			case <-ctx.Done():
				t.Fatal("fixture never reached protocol input")
			}
			if mode == "file-start-close" {
				go func() { closed <- helper.Close() }()
			} else {
				stop()
			}
			select {
			case err := <-result:
				if err == nil || transfer.Retryable(err) {
					t.Fatalf("silent startup lost its terminal error: %v", err)
				}
			case <-time.After(5 * time.Second):
				// Release the old implementation's read before reporting red;
				// do not hang the package until its global test timeout.
				_ = owned.Close()
				<-result
				t.Fatal("silent SSH read survived cancellation/Close")
			}
			if mode == "file-start-close" {
				select {
				case err := <-closed:
					if err == nil {
						t.Fatal("unconfirmed file startup reported successful cleanup")
					}
				case <-time.After(time.Second):
					t.Fatal("Close still waits for file startup lock")
				}
			}
			recordMu.Lock()
			created := append([]config.WorkspaceRecord(nil), records...)
			removed := retired
			recordMu.Unlock()
			if removed {
				t.Fatal("unknown exit retired the recovery record")
			}
			if (mode == "control-start" || mode == "system-start") && len(created) == 0 {
				t.Fatal("fixture did not exercise a real journaled installation")
			}
			for _, record := range created {
				if _, err := browser.Stat(ctx, record.Path); err != nil {
					t.Fatalf("unknown startup removed its installation: %v", err)
				}
			}
			probe := filepath.Join(t.TempDir(), "browser-remains-usable")
			if err := os.WriteFile(probe, []byte("browser"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := browser.Stat(ctx, probe); err != nil {
				t.Fatalf("cancellation damaged browsing: %v", err)
			}
		})
	}
}

type silentPeerConn struct {
	net.Conn
	paused  atomic.Bool
	release <-chan struct{}
}

func (c *silentPeerConn) Write(data []byte) (int, error) {
	if c.paused.Load() {
		<-c.release
	}
	return c.Conn.Write(data)
}

func silentStartupPeer(t *testing.T, mode string) (connector.Route, <-chan struct{}) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	policy := &ssh.ServerConfig{NoClientAuth: true}
	policy.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	life, cancel := context.WithCancel(context.Background())
	release, reached := make(chan struct{}), make(chan struct{})
	var reachedOnce sync.Once
	var mu sync.Mutex
	connections := make(map[net.Conn]bool)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			socket, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections[socket] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				peer := &silentPeerConn{Conn: socket, release: release}
				connection, channels, requests, err := ssh.NewServerConn(peer, policy)
				if err != nil {
					return
				}
				defer connection.Close()
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					if incoming.ChannelType() != "session" {
						_ = incoming.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					channel, requests, err := incoming.Accept()
					if err != nil {
						continue
					}
					wg.Add(1)
					go func() {
						defer wg.Done()
						defer channel.Close()
						stall := func() { peer.paused.Store(true); reachedOnce.Do(func() { close(reached) }); <-release }
						for request := range requests {
							var command struct{ Value string }
							if ssh.Unmarshal(request.Payload, &command) != nil {
								_ = request.Reply(false, nil)
								continue
							}
							if request.Type == "subsystem" && command.Value == "sftp" {
								_ = request.Reply(true, nil)
								server, err := sftp.NewServer(channel)
								if err == nil {
									_ = server.Serve()
									_ = server.Close()
								}
								return
							}
							if request.Type != "exec" {
								_ = request.Reply(false, nil)
								continue
							}
							_ = request.Reply(true, nil)
							if command.Value == "dragfm-fixture-control" {
								protocol, err := agentproto.Server(channel, channel)
								if err != nil {
									return
								}
								var request agentproto.Request
								if protocol.Receive(&request) == nil {
									stall()
								}
								return
							}
							helper := strings.HasPrefix(command.Value, "exec '") && strings.HasSuffix(command.Value, "/dragfm-agent'")
							files := strings.HasSuffix(command.Value, "'--sftp'") || (mode == "system-start" && strings.Contains(command.Value, "sftp-server") && strings.Contains(command.Value, "flock -s"))
							if helper || files {
								size := len(agentproto.Magic) + 32
								if files {
									size = 9
								} // uint32 length + SSH_FXP_INIT + uint32 version
								input := make([]byte, size)
								if _, err := io.ReadFull(channel, input); err != nil {
									return
								}
								if files {
									if binary.BigEndian.Uint32(input[:4]) != 5 || input[4] != 1 {
										return
									}
								} else if string(input[:len(agentproto.Magic)]) != agentproto.Magic {
									return
								}
								stall()
								return
							}
							process := exec.CommandContext(life, "/bin/sh", "-c", command.Value)
							process.Stdin, process.Stdout, process.Stderr = channel, channel, channel.Stderr()
							process.WaitDelay = time.Second
							err = process.Run()
							status := uint32(0)
							if err != nil {
								status = 1
								if exit, ok := err.(*exec.ExitError); ok {
									status = uint32(exit.ExitCode())
								}
							}
							var payload [4]byte
							binary.BigEndian.PutUint32(payload[:], status)
							_, _ = channel.SendRequest("exit-status", false, payload[:])
							return
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		close(release)
		mu.Lock()
		for connection := range connections {
			_ = connection.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	return connector.Route{Hops: []connector.Hop{{Host: host, Port: port, User: "fixture", Credentials: connector.Credentials{Password: "disposable"}, HostKey: connector.HostKeyPolicy{PinnedSHA256: ssh.FingerprintSHA256(signer.PublicKey())}}}}, reached
}
