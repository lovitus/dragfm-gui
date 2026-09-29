package webgui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/configtext"
	"github.com/lovitus/dragfm-gui/internal/routespec"
	"github.com/lovitus/dragfm-gui/internal/vault"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Real loopback SSH/SFTP and direct-tcpip are used here. Only the listener's
// allowed forwarding destination differs from the controller's view, as it
// does when the target is reachable only from a relay. No user account, SSH
// agent, external address or credential is used by this regression fixture.
func passwordRouteFixture(t *testing.T, password string, forwarded map[string]string, executors ...func(ssh.Channel, string) int) (string, string) {
	t.Helper()
	if len(executors) > 1 {
		t.Fatal("fixture requires at most one isolated command executor")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	policy := &ssh.ServerConfig{PasswordCallback: func(meta ssh.ConnMetadata, supplied []byte) (*ssh.Permissions, error) {
		if meta.User() == "tester" && string(supplied) == password {
			return nil, nil
		}
		return nil, errors.New("fixture authentication rejected")
	}}
	policy.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	var wg sync.WaitGroup
	var mu sync.Mutex
	connections := make(map[net.Conn]bool)
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
				defer func() { _ = socket.Close(); mu.Lock(); delete(connections, socket); mu.Unlock() }()
				_ = socket.SetDeadline(time.Now().Add(30 * time.Second))
				connection, channels, requests, err := ssh.NewServerConn(socket, policy)
				if err != nil {
					return
				}
				defer connection.Close()
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					if incoming.ChannelType() == "direct-tcpip" {
						var request struct {
							Host       string
							Port       uint32
							Origin     string
							OriginPort uint32
						}
						if ssh.Unmarshal(incoming.ExtraData(), &request) != nil {
							_ = incoming.Reject(ssh.Prohibited, "invalid fixture request")
							continue
						}
						address := forwarded[net.JoinHostPort(request.Host, strconv.Itoa(int(request.Port)))]
						if address == "" {
							_ = incoming.Reject(ssh.Prohibited, "not a fixture destination")
							continue
						}
						upstream, err := net.DialTimeout("tcp", address, time.Second)
						if err != nil {
							_ = incoming.Reject(ssh.ConnectionFailed, "fixture unavailable")
							continue
						}
						channel, requests, err := incoming.Accept()
						if err != nil {
							_ = upstream.Close()
							continue
						}
						go ssh.DiscardRequests(requests)
						wg.Add(2)
						go func() { defer wg.Done(); _, _ = io.Copy(upstream, channel); _ = upstream.Close(); _ = channel.Close() }()
						go func() { defer wg.Done(); _, _ = io.Copy(channel, upstream); _ = channel.Close(); _ = upstream.Close() }()
						continue
					}
					if incoming.ChannelType() != "session" {
						_ = incoming.Reject(ssh.UnknownChannelType, "unsupported fixture channel")
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
						for request := range requests {
							if request.Type == "exec" && len(executors) == 1 {
								var payload struct{ Command string }
								if ssh.Unmarshal(request.Payload, &payload) != nil {
									_ = request.Reply(false, nil)
									continue
								}
								_ = request.Reply(true, nil)
								status := executors[0](channel, payload.Command)
								_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(status)}))
								return
							}
							var subsystem struct{ Name string }
							if request.Type != "subsystem" || ssh.Unmarshal(request.Payload, &subsystem) != nil || subsystem.Name != "sftp" {
								_ = request.Reply(false, nil)
								continue
							}
							_ = request.Reply(true, nil)
							options := []sftp.ServerOption{sftp.WithServerWorkingDirectory(home)}
							if len(executors) != 0 {
								// Match the production helper's actual filesystem
								// semantics. WithServerWorkingDirectory also prefixes
								// relative SYMLINK targets, changing their contents.
								options = nil
							}
							server, err := sftp.NewServer(channel, options...)
							if err == nil {
								_ = server.Serve()
								_ = server.Close()
							}
							return
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		for connection := range connections {
			_ = connection.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return listener.Addr().String(), ssh.FingerprintSHA256(signer.PublicKey())
}

func TestComposedSSHRouteCredentialsBelongToOriginalSession(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	for _, mode := range []string{"session-only", "saved", "failed-authentication"} {
		t.Run(mode, func(t *testing.T) {
			targetAddress, targetPin := passwordRouteFixture(t, "target-password", nil)
			// A real SSH listener with a different key makes direct access fail
			// closed; the relay forwards this address to the authentic target.
			outsideView, _ := passwordRouteFixture(t, "not-the-target", nil)
			relayAddress, relayPin := passwordRouteFixture(t, "relay-password", map[string]string{outsideView: targetAddress})
			app := unlockedTestApp(t)
			target := config.Host{ID: "target", Name: "target", RouteSpec: fmt.Sprintf(`tester:"target-password"@%s`, outsideView), HopFingerprints: []string{targetPin}}
			relay := config.Host{ID: "relay", Name: "relay", RouteSpec: fmt.Sprintf(`tester:"old-relay-password"@%s`, relayAddress), HopFingerprints: []string{relayPin}}
			app.mu.Lock()
			app.document.Hosts = []config.Host{target, relay}
			app.sessionSSH[relay.ID] = true
			app.mu.Unlock()
			if err := app.save(); err != nil {
				t.Fatal(err)
			}
			before := app.document.Clone()
			promptCount := 0
			app.eventSink = func(event string, value any) {
				if event != "challenge" {
					return
				}
				challenge := value.(ChallengeModel)
				if challenge.Kind != "password" {
					app.ResolveChallenge(challenge.ID, false, "", false)
					return
				}
				promptCount++
				password, accepted := "relay-password", true
				if mode == "failed-authentication" {
					password, accepted = "rejected-password", promptCount == 1
				}
				app.ResolveChallenge(challenge.ID, accepted, password, mode != "session-only")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			remote, _, err := app.connectRemote(ctx, target.Name, "本机")
			if remote != nil {
				defer remote.Close()
			}
			if mode == "failed-authentication" {
				if err == nil {
					t.Fatal("rejected fixture password authenticated")
				}
				app.mu.RLock()
				after := app.document.Clone()
				cached := len(app.runtimePasswords)
				app.mu.RUnlock()
				if !reflect.DeepEqual(before, after) || cached != 0 {
					t.Fatal("rejected password altered the vault or session credentials")
				}
				_, persisted, openErr := vault.Open(app.vaultPath, []byte("test master password"))
				if openErr != nil {
					t.Fatal(openErr)
				}
				if !reflect.DeepEqual(before.Hosts, persisted.Hosts) {
					t.Fatal("failed authentication was persisted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if promptCount != 1 {
				t.Fatalf("unexpected password prompt count: %d", promptCount)
			}
			for _, expected := range []struct {
				host     config.Host
				password string
			}{{target, "target-password"}, {relay, "relay-password"}} {
				route, err := app.routeForHost(expected.host)
				if err != nil {
					t.Fatal(err)
				}
				if route.Hops[0].Credentials.Password != expected.password {
					t.Fatal("composed route wrote a password into another session")
				}
			}
			_, persisted, err := vault.Open(app.vaultPath, []byte("test master password"))
			if err != nil {
				t.Fatal(err)
			}
			wantRelay := "old-relay-password"
			if mode == "saved" {
				wantRelay = "relay-password"
			}
			for _, expected := range []struct{ id, password string }{{"target", "target-password"}, {"relay", wantRelay}} {
				host := persisted.HostByID(expected.id)
				hops, err := routespec.ParseSSH(host.RouteSpec, nil)
				if err != nil {
					t.Fatal(err)
				}
				if hops[0].Credentials.Password != expected.password || len(host.HopPasswords) != 0 {
					t.Fatal("reopening vault changed credential owner or used a hidden overlay")
				}
			}
			if mode == "saved" && strings.Contains(configtext.Markdown(persisted), "old-relay-password") {
				t.Fatal("configuration editor still shows the obsolete password")
			}
		})
	}
}
