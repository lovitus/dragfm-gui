//go:build integration && !windows

package webgui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

// The controller logs in through relay -> peer. The actual transfer initiator
// can reach peer but cannot reach relay. A saved deep session remains one
// endpoint; it must not force a controller-only hop into direct transmission.
func TestHostedPeerDirectDoesNotReplayControllerJump(t *testing.T) {
	for _, system := range []bool{false, true} {
		t.Run(fmt.Sprintf("system=%t", system), func(t *testing.T) {
			if system {
				for _, name := range []string{"DRAGFM_E2E_SOURCE_SSH", "DRAGFM_E2E_TARGET_SSH"} {
					if os.Getenv(name) == "" {
						t.Skip("disposable SSH fixtures are not configured")
					}
					t.Setenv(name, os.Getenv(name)+"-system")
				}
			}
			source, target, document := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			relayName := os.Getenv("DRAGFM_E2E_RELAY_SSH")
			if relayName == "" {
				t.Fatal("real relay fixture required")
			}
			relay, keys := openSSHRoute(t, relayName)
			document.Keys = append(document.Keys, keys...)
			peerIndex, initiator, deepPeer := 1, source, target
			want := "direct · source-push · scp"
			if system {
				peerIndex, initiator, deepPeer = 0, target, source
				want = "direct · target-pull · rsync"
			}
			peer := &document.Hosts[peerIndex]
			jump := relay.Hops[0]
			peer.RouteSpec = fmt.Sprintf(`%s@%s:%d %s@%s:%d --keys "%s,%s"`, jump.User, jump.Host, jump.Port, peer.User, peer.Address, peer.Port, keys[0].Name, peer.KeyIDs[0])
			peer.HopFingerprints = []string{jump.HostKey.PinnedSHA256, peer.HostFingerprint}
			// No speculative pool connections are needed for this direct-route test.
			for i := range document.Hosts {
				document.Hosts[i].NoRelay = true
			}
			rule := fmt.Sprintf("OUTPUT -d %s -p tcp --dport %d -j REJECT --reject-with tcp-reset", shellQuote(jump.Host), jump.Port)
			if err := initiator.Exec(ctx, "sudo -n iptables -I "+rule, endpoint.ExecOptions{}); err != nil {
				t.Fatal(err)
			}
			defer initiator.Exec(context.Background(), "sudo -n iptables -D "+rule, endpoint.ExecOptions{})
			// A bounded actual TCP attempt verifies the firewall fixture, not a
			// synthetic dial failure. The subsequent queue must still use direct.
			probe := fmt.Sprintf("python3 -c %s", shellQuote(fmt.Sprintf("import socket; socket.create_connection((%q, %d), 2)", jump.Host, jump.Port)))
			if err := initiator.Exec(ctx, probe, endpoint.ExecOptions{}); err == nil {
				t.Fatal("fixture did not block initiator-to-controller-jump TCP")
			}
			if system {
				// Ensure only target-pull can reach the other endpoint's SSH.
				pushRule := "OUTPUT -d " + shellQuote(target.ConnectionHost()) + " -p tcp --dport 22 -j REJECT --reject-with tcp-reset"
				if err := source.Exec(ctx, "sudo -n iptables -I "+pushRule, endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
				defer source.Exec(context.Background(), "sudo -n iptables -D "+pushRule, endpoint.ExecOptions{})
			} else {
				info, err := deepPeer.Stat(ctx, "/usr/bin/rsync")
				if err != nil {
					t.Fatal(err)
				}
				if err := deepPeer.Exec(ctx, "sudo -n chmod 0644 /usr/bin/rsync", endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
				defer deepPeer.Exec(context.Background(), fmt.Sprintf("sudo -n chmod %04o /usr/bin/rsync", info.Mode.Perm()), endpoint.ExecOptions{})
			}
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			defer source.Remove(context.Background(), sourceRoot, true)
			defer target.Remove(context.Background(), targetRoot, true)
			from := source.Join(sourceRoot, "direct.txt")
			const contents = "direct peer route, not controller relay"
			writeRemoteFile(t, ctx, source, from, contents)
			app := unlockedTestApp(t)
			done := make(chan JobUpdateModel, 1)
			app.mu.Lock()
			app.document = document.Clone()
			app.eventSink = func(name string, value any) {
				if name == "challenge" {
					c := value.(ChallengeModel)
					if c.Kind == "confirm" {
						app.SkipChallenge(c.ID)
					} else {
						app.ResolveChallenge(c.ID, false, "", false)
					}
				}
				if name == "job:update" {
					job := value.(JobUpdateModel)
					if job.State == "succeeded" || job.State == "failed" || job.State == "cancelled" {
						select {
						case done <- job:
						default:
						}
					}
				}
			}
			app.mu.Unlock()
			if _, err := app.ChangeEndpoint(LeftPane, source.Name(), "本机"); err != nil {
				t.Fatal(err)
			}
			if _, err := app.ChangeEndpoint(RightPane, target.Name(), "本机"); err != nil {
				t.Fatal(err)
			}
			preview, err := app.PrepareDrop(LeftPane, from, RightPane, targetRoot)
			if err != nil {
				t.Fatal(err)
			}
			id, err := app.QueueTransfer(TransferRequest{DropPreview: preview})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-done:
				if result.ID != id || result.State != "succeeded" || result.Method != want {
					t.Fatalf("direct route replayed the controller jump or fell back: state=%s method=%s message=%s", result.State, result.Method, result.Message)
				}
				if strings.Contains(result.Method, "relay") {
					t.Fatal("memory relay must not masquerade as peer direct")
				}
			case <-ctx.Done():
				t.Fatal("peer direct transfer did not finish")
			}
			assertRemoteFile(t, ctx, target, target.Join(targetRoot, "direct.txt"), contents)
			assertRemoteFile(t, ctx, source, from, contents)
		})
	}
}
