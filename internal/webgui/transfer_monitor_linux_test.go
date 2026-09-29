package webgui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/boundedbuf"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

// Reproduce the real monitor's wait bookkeeping failure, not a replacement
// shell/wait implementation. glibc's documented freed-memory perturbation is
// confined to this child process; no runner or product environment is changed.
func TestTransferMonitorReapsCompletedJob(t *testing.T) {
	for _, delayed := range []bool{false, true} {
		for _, status := range []int{0, 23, 127} {
			name := "fast/" + strconv.Itoa(status)
			prefix := ""
			if delayed {
				name = "waiting/" + strconv.Itoa(status)
				prefix = "sleep 0.1; "
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				command := prefix + "printf 'owned-child-completed\\n'; exit " + strconv.Itoa(status)
				child := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", ncatOwnedCommand(command))
				child.Env = append(os.Environ(), "BASH_ENV=", "ENV=", "MALLOC_PERTURB_=127", "GLIBC_TUNABLES=glibc.malloc.tcache_count=0")
				child.WaitDelay = time.Second
				var output bytes.Buffer
				var diagnostic boundedbuf.Buffer
				child.Stdout, child.Stderr = &output, &diagnostic
				err := child.Run()
				if output.String() != "owned-child-completed\n" {
					t.Fatalf("child fixture did not execute: %v; %s", err, diagnostic.String())
				}
				if ctx.Err() != nil {
					t.Fatalf("owned transfer monitor did not reap its completed child: %v; %s", err, diagnostic.String())
				}
				want := status
				if status == 127 {
					want = 77 // Cannot distinguish tool exit from missing job.
				}
				if (want == 0 && err != nil) || (want != 0 && err == nil) || child.ProcessState.ExitCode() != want {
					t.Fatalf("owned transfer monitor status = %d, want %d: %v; %s", child.ProcessState.ExitCode(), want, err, diagnostic.String())
				}
			})
		}
	}
}

// Exercise the actual SSH Exec -> ncat pair error boundary without a network
// transfer: controlled child programs terminate at listener/peer startup.
// The complete tar+ncat move and STOP/SSH-loss flows remain in hosted E2E.
func TestNcatMonitorUncertainExitIsNotRetryable(t *testing.T) {
	remote := moveGateRemote(t, t.TempDir())
	for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
		for _, listenerFails := range []bool{false, true} {
			phase := "peer"
			if listenerFails {
				phase = "listener"
			}
			t.Run(string(direction)+"/"+phase, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				listener := ncatOwnedCommand("printf '%s\\n' 'Listening on fixture' >&2; sleep 0.1; printf '%s\\n' '" + strings.Repeat("0", 64) + " -'")
				peer := ncatOwnedCommand("exit 127")
				if listenerFails {
					listener, peer = peer, listener
				}
				sender, receiver := peer, listener
				if direction == strategy.TargetPull {
					sender, receiver = listener, peer
				}
				_, err := runNcatPair(ctx, remote, remote, sender, receiver, direction)
				if ctx.Err() != nil {
					t.Fatalf("monitor result was replaced by the fixture deadline: %v", err)
				}
				if !errors.Is(err, endpoint.ErrCommandExitUnconfirmed) || transfer.Retryable(err) {
					t.Fatalf("uncertain monitor exit permitted cleanup/retry: %v", err)
				}
			})
		}
	}
}
