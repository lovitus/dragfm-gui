//go:build integration && !windows

package webgui

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"golang.org/x/crypto/ssh"
)

// Observe the original Exec result before the pair classifies/filters it.
// No command, context, option or returned error is replaced by this adapter.
type ncatObservedExec struct {
	endpoint.Endpoint
	result chan error
}

func (e *ncatObservedExec) Exec(ctx context.Context, command string, options endpoint.ExecOptions) error {
	err := e.Endpoint.Exec(ctx, command, options)
	e.result <- err
	return err
}

// Real OpenSSH TERM/exit-status handshakes reproduce the pair's cancellation
// race. Controlled shell children test only the error boundary, not a host-key
// handshake, TCP archive transfer or the product monitor's signal trap.
func TestHostedNcatPeerCancellationPreservesSafetyCause(t *testing.T) {
	source, target, _ := fixtureEndpoints(t)
	for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
		for _, scenario := range []struct {
			name   string
			status int
			routed bool
		}{
			{"routed-78", 78, true},
			{"monitor-77", 77, true},
			{"ordinary-23", 23, true},
			{"unrouted-78", 78, false},
		} {
			t.Run(string(direction)+"/"+scenario.name, func(t *testing.T) {
				parent, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				listener, peer := target, source
				if direction == strategy.TargetPull {
					listener, peer = source, target
				}
				listenerRoot, peerRoot := remoteTempDir(t, parent, listener), remoteTempDir(t, parent, peer)
				t.Cleanup(func() {
					cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
					defer stop()
					if err := listener.Remove(cleanup, listenerRoot, true); err != nil {
						t.Error("listener fixture cleanup", err)
					}
					if err := peer.Remove(cleanup, peerRoot, true); err != nil {
						t.Error("peer fixture cleanup", err)
					}
				})
				fail := listener.Join(listenerRoot, "fail")
				ready, ack := peer.Join(peerRoot, "ready"), peer.Join(peerRoot, "ack")
				release, waiting := peer.Join(peerRoot, "release"), peer.Join(peerRoot, "waiting")
				if err := listener.Exec(parent, "umask 077; command mkfifo -- "+shellQuote(fail), endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
				if err := peer.Exec(parent, "umask 077; command mkfifo -- "+shellQuote(ready)+" "+shellQuote(ack)+" "+shellQuote(release)+" "+shellQuote(waiting), endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
				listenScript := "exec 7<>" + shellQuote(fail) + "; printf 'Listening on fixture\\n' >&2; IFS= read -r failure <&7; exit 23"
				stopScript := "printf 'signalled\\n' > " + shellQuote(ack) + "; IFS= read -r released <&8; exit " + strconv.Itoa(scenario.status)
				peerScript := "exec 7<>" + shellQuote(waiting) + "; exec 8<>" + shellQuote(release) + "; trap " + shellQuote(stopScript) + " TERM; printf 'ready\\n' > " + shellQuote(ready) + "; IFS= read -r unused <&7; exit 99"
				listenCommand := "exec bash --noprofile --norc -c " + shellQuote(listenScript)
				peerCommand := "exec bash --noprofile --norc -c " + shellQuote(peerScript)
				observedListener := &ncatObservedExec{Endpoint: listener, result: make(chan error, 1)}
				observedPeer := &ncatObservedExec{Endpoint: peer, result: make(chan error, 1)}
				var send, receive endpoint.Endpoint = observedPeer, observedListener
				sender, receiver := peerCommand, listenCommand
				if direction == strategy.TargetPull {
					send, receive = observedListener, observedPeer
					sender, receiver = listenCommand, peerCommand
				}
				var input []byte
				if scenario.routed {
					input = []byte("controlled route marker") // Only selects the original routed boundary.
				}
				done := make(chan error, 1)
				go func() {
					_, err := runNcatPairWithInput(parent, send, receive, sender, receiver, direction, input)
					done <- err
				}()
				// Ensure any Fatal joins the pair before fixture FIFOs are removed.
				joined := false
				defer func() {
					cancel()
					if !joined {
						<-done
					}
				}()
				// Each control command has the still-live parent budget, not the
				// pair's cancelled life. No sleeping/polling or test-side cancel.
				if err := peer.Exec(parent, "IFS= read -r token < "+shellQuote(ready)+"; [ \"$token\" = ready ]", endpoint.ExecOptions{}); err != nil {
					t.Fatal("peer did not establish its TERM trap", err)
				}
				if err := listener.Exec(parent, "printf 'fail\\n' > "+shellQuote(fail), endpoint.ExecOptions{}); err != nil {
					t.Fatal("could not release the ordinary listener failure", err)
				}
				if err := peer.Exec(parent, "IFS= read -r token < "+shellQuote(ack)+"; [ \"$token\" = signalled ]", endpoint.ExecOptions{}); err != nil {
					t.Fatal("pair cancellation did not reach the real peer TERM trap", err)
				}
				if err := peer.Exec(parent, "printf 'released\\n' > "+shellQuote(release), endpoint.ExecOptions{}); err != nil {
					t.Fatal("could not release the cancelled peer", err)
				}
				failure := <-done
				joined = true
				listenerResult, peerResult := <-observedListener.result, <-observedPeer.result
				var listenerStatus, peerStatus *ssh.ExitError
				if parent.Err() != nil || !errors.As(listenerResult, &listenerStatus) || listenerStatus.ExitStatus() != 23 || errors.Is(listenerResult, context.Canceled) {
					t.Fatalf("ordinary listener exit was not established before cancellation: %v; parent=%v", listenerResult, parent.Err())
				}
				if !errors.Is(peerResult, context.Canceled) || !errors.As(peerResult, &peerStatus) || peerStatus.ExitStatus() != scenario.status {
					t.Fatalf("actual peer cancellation/status was not established: got %v, want canceled + %d", peerResult, scenario.status)
				}
				var marker interface{ Retryable() bool }
				stopped := errors.As(failure, &marker) && !marker.Retryable()
				wantStop := scenario.status == 77 || (scenario.status == 78 && scenario.routed)
				if scenario.status == 78 && scenario.routed && !stopped {
					t.Fatalf("pair discarded routed host-key rejection after confirmed peer cancellation: %v", failure)
				}
				if failure == nil || stopped != wantStop || transfer.Retryable(failure) == wantStop || errors.Is(failure, endpoint.ErrCommandExitUnconfirmed) != (scenario.status == 77) || errors.Is(failure, context.Canceled) != wantStop || errors.Is(failure, peerResult) != wantStop || !errors.Is(failure, listenerResult) {
					t.Fatalf("pair changed the confirmed peer safety semantics: stop=%t want=%t; %v", stopped, wantStop, failure)
				}
			})
		}
	}
}
