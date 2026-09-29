//go:build integration && !windows

package webgui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/agentroute"
	"github.com/lovitus/dragfm-gui/internal/boundedbuf"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/remoteagent"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"golang.org/x/crypto/ssh"
)

// Cancel an actual OpenSSH command only after its ready line. Release stdin
// only after the TERM trap acknowledges the signal, so Exec's cancellation
// branch is established without a timing sleep. Its real exit-status is kept for
// the monitor-specific caller instead of reducing it to context.Canceled.
func TestHostedCancelledCommandRetainsExitStatus(t *testing.T) {
	source, _, _ := fixtureEndpoints(t)
	for _, status := range []string{"23", "77"} {
		t.Run(status, func(t *testing.T) {
			budget, stopBudget := context.WithTimeout(context.Background(), 15*time.Second)
			defer stopBudget()
			ctx, cancel := context.WithCancel(budget)
			defer cancel()
			input, feed := io.Pipe()
			output, stream := io.Pipe()
			signals, signalStream := io.Pipe()
			defer input.Close()
			defer feed.Close()
			defer output.Close()
			defer stream.Close()
			defer signals.Close()
			defer signalStream.Close()
			stop := context.AfterFunc(budget, func() {
				_ = feed.Close()
				_ = output.CloseWithError(budget.Err())
				_ = signals.CloseWithError(budget.Err())
			})
			defer stop()
			done := make(chan error, 1)
			script := "trap 'printf \"signalled\\n\" >&2; IFS= read -r released; exit " + status + "' TERM; printf 'ready\\n'; IFS= read -r unused; exit 99"
			go func() {
				err := source.Exec(ctx, "exec bash --noprofile --norc -c "+shellQuote(script), endpoint.ExecOptions{Stdin: input, Stdout: stream, Stderr: signalStream})
				_ = stream.CloseWithError(err)
				_ = signalStream.CloseWithError(err)
				done <- err
			}()
			if ready, err := bufio.NewReader(output).ReadString('\n'); err != nil || ready != "ready\n" {
				t.Fatalf("real cancellable command did not start: %q %v", ready, err)
			}
			cancel()
			if line, err := bufio.NewReader(signals).ReadString('\n'); err != nil || line != "signalled\n" {
				t.Fatalf("real SSH cancellation did not reach the TERM trap: %q %v", line, err)
			}
			_ = feed.Close()
			err := <-done
			var exited *ssh.ExitError
			want := 23
			if status == "77" {
				want = 77
			}
			if !errors.Is(err, context.Canceled) || !errors.As(err, &exited) || exited.ExitStatus() != want {
				t.Fatalf("cancellation erased the real remote exit status: %v", err)
			}
		})
	}
}

// Real local tools, Paramiko authentication and a peer SSH command exercise
// the existing native carrier. The controlled peer writes a witness and exits
// 127 instead of starting its tool; no transport or monitor is replaced.
func TestHostedSystemNativePeerMonitorExitUnknown(t *testing.T) {
	source, target, document := fixtureEndpoints(t)
	app := New(filepath.Join(t.TempDir(), "unused.vault"))
	app.document = document
	defer app.Lock()
	for _, method := range []strategy.Method{strategy.SCP, strategy.Rsync} {
		t.Run(string(method), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			t.Cleanup(func() {
				_ = source.Remove(context.Background(), sourceRoot, true)
				_ = target.Remove(context.Background(), targetRoot, true)
			})
			payload, destination := source.Join(sourceRoot, "source"), target.Join(targetRoot, "destination")
			writeRemoteFile(t, ctx, source, payload, "source must survive unknown peer exit")
			host, found := document.HostByName(target.Name())
			if !found {
				t.Fatal("fixture peer is absent from its route document")
			}
			route, err := app.peerTransferRoute(ctx, host, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := app.encodeTransferRoute(ctx, route, host, false)
			if err != nil {
				t.Fatal(err)
			}
			var wire agentroute.Route
			if err := json.Unmarshal([]byte(encoded), &wire); err != nil {
				t.Fatal(err)
			}
			lease, err := remoteagent.NewWorkspace(ctx, source, sourceRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := lease.Close(true); err != nil {
					t.Error("fixture workspace could not be safely closed", err)
				}
			}()
			witness := target.Join(targetRoot, "peer-executed")
			peer := ncatOwnedCommand("printf 'completed\\n' > " + shellQuote(witness) + "; exit 127")
			request := systemPoolRequest{Version: 1, Mode: "native", Route: wire, Native: &systemNativeRequest{
				Method: method, Source: payload, Target: destination,
				PeerPrefix: []string{"bash", "--noprofile", "--norc", "-c", peer, "dragfm"},
			}}
			input, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			var diagnostic boundedbuf.Buffer
			program := "exec 6<&0; exec python3 -I -B -c " + shellQuote(systemNativeProgram+"\n"+systemPoolStream)
			err = source.Exec(ctx, lease.Command(ncatOwnedCommand(program)), endpoint.ExecOptions{Stdin: bytes.NewReader(input), Stderr: &diagnostic})
			// This is independent of the command deadline and verifies that a
			// setup/authentication failure cannot count as the intended red.
			verify, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			assertRemoteFile(t, verify, target, witness, "completed\n")
			assertRemoteFile(t, verify, source, payload, "source must survive unknown peer exit")
			if _, statErr := target.Stat(verify, destination); !errors.Is(statErr, fs.ErrNotExist) {
				t.Fatal("failed peer command published a destination", statErr)
			}
			var status *ssh.ExitError
			if ctx.Err() != nil || !errors.As(err, &status) || status.ExitStatus() != 77 {
				t.Fatalf("native carrier erased peer monitor uncertainty: %v; %s", err, diagnostic.String())
			}
		})
	}
}
