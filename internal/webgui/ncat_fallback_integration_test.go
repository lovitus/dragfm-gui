//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/remoteagent"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

// The regression uses the baseline runNcatTar API and real SSH/filesystems.
// Old code fails because its purported system fallback starts the rejected
// helper, not because a new test depends on a symbol missing in the baseline.
func TestHostedNcatWithoutExecutableHelperRetriesBlockedPorts(t *testing.T) {
	source, target := systemOnlyFixtureEndpoints(t)
	for _, candidate := range []struct {
		direction strategy.Direction
		move      bool
		exhaust   bool
	}{
		{strategy.SourcePush, false, false},
		{strategy.TargetPull, true, false},
		{strategy.SourcePush, true, true},
	} {
		t.Run(string(candidate.direction), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			t.Cleanup(func() {
				_ = source.Remove(context.Background(), sourceRoot, true)
				_ = target.Remove(context.Background(), targetRoot, true)
			})
			name := "archive '目录\nwith newline"
			op := transfer.Operation{Source: source, Destination: target, SourcePath: source.Join(sourceRoot, name), TargetPath: target.Join(targetRoot, "renamed directory"), Move: candidate.move}
			if err := source.MkdirAll(ctx, op.SourcePath, 0750); err != nil {
				t.Fatal(err)
			}
			payload := source.Join(op.SourcePath, "-file '名字\n.txt")
			writeRemoteFile(t, ctx, source, payload, "direct archive contents\n")
			outside := source.Join(sourceRoot, "outside.txt")
			writeRemoteFile(t, ctx, source, outside, "do not follow or delete this referent")
			if err := source.Symlink(ctx, "../outside.txt", source.Join(op.SourcePath, "link")); err != nil {
				t.Fatal(err)
			}
			modified := time.Unix(1700000000, 0)
			for _, item := range []string{payload, op.SourcePath} {
				if err := source.Chtimes(ctx, item, modified, modified); err != nil {
					t.Fatal(err)
				}
			}
			before, err := transfer.Snapshot(ctx, source, op.SourcePath, true)
			if err != nil {
				t.Fatal(err)
			}
			preflight := mustPreflight(t, ctx, op)
			for _, host := range []struct {
				remote *endpoint.Remote
				arch   string
			}{{source, preflight.SourceCapabilities.Architecture}, {target, preflight.TargetCapabilities.Architecture}} {
				if err := host.remote.Exec(ctx, "rm -f -- \"$HOME/.helper-denied\"", endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
				helper, err := remoteagent.Start(ctx, host.remote, host.arch)
				if err == nil {
					_ = helper.Close()
					t.Fatal("fixture did not deny execution of the uploaded helper")
				}
				home, err := host.remote.Home(ctx)
				if err != nil {
					t.Fatal(err)
				}
				// Verify actual policy execution, not any arbitrary Start error
				// such as a missing embedded artifact or SSH authentication error.
				assertRemoteFile(t, ctx, host.remote, host.remote.Join(home, ".helper-denied"), "denied\n")
			}
			initiator, peer := source, target
			if candidate.direction == strategy.TargetPull {
				initiator, peer = target, source
			}
			rejectCount := rejectNcatDataConnections(t, ctx, initiator, peer, candidate.exhaust)
			var mu sync.Mutex
			var ports []int
			observed, _ := activity.WithObserver(ctx, func(stage string, _ int64) {
				if !strings.HasPrefix(stage, "ncat-port/") {
					return
				}
				port, _ := strconv.Atoi(strings.TrimPrefix(stage, "ncat-port/"))
				mu.Lock()
				ports = append(ports, port)
				mu.Unlock()
			})
			transferErr := runNcatTar(observed, op, candidate.direction)
			if transferErr != nil && !candidate.exhaust {
				t.Fatalf("system fallback with helper execution denied and first data connection rejected: %v", transferErr)
			}
			mu.Lock()
			attempted := append([]int(nil), ports...)
			mu.Unlock()
			if len(attempted) < 2 || len(attempted) > 5 {
				t.Fatalf("expected connection-level retries within five ports, got %v", attempted)
			}
			seen := make(map[int]bool)
			for _, port := range attempted {
				if port < 20000 || port > 60999 || seen[port] {
					t.Fatalf("retry reused a port or left the approved range: %v", attempted)
				}
				seen[port] = true
			}
			if rejectCount() == 0 {
				t.Fatal("fixture did not reject a real TCP data connection")
			}
			if candidate.exhaust {
				if transferErr == nil || len(attempted) != 5 {
					t.Fatal("all five blocked ports did not terminate with failure")
				}
				after, err := transfer.Snapshot(ctx, source, op.SourcePath, true)
				if err != nil {
					t.Fatal(err)
				}
				if err := transfer.CompareManifests(before, after, false); err != nil {
					t.Fatal("failed move changed its source")
				}
				if _, err := target.Stat(ctx, op.TargetPath); !errors.Is(err, fs.ErrNotExist) {
					t.Fatal("failed transfer committed a destination")
				}
				assertSystemNcatClean(t, ctx, source, target, targetRoot)
				return
			}
			after, err := transfer.Snapshot(ctx, target, op.TargetPath, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(before.Items) != len(after.Items) {
				t.Fatal("directory entries changed during archive transfer")
			}
			if err := transfer.CompareManifests(before, after, true); err != nil {
				t.Fatal(err)
			}
			for i, entry := range before.Items {
				if entry.Mode&fs.ModeSymlink == 0 && (after.Items[i].Mode.Perm() != entry.Mode.Perm() || after.Items[i].ModifiedNS != entry.ModifiedNS) {
					t.Fatal("system archive transfer lost file/directory mode or mtime")
				}
			}
			_, err = source.Stat(ctx, op.SourcePath)
			if candidate.move && !errors.Is(err, fs.ErrNotExist) || !candidate.move && err != nil {
				t.Fatal("copy/move source preservation does not match the requested operation")
			}
			assertRemoteFile(t, ctx, source, outside, "do not follow or delete this referent")
			assertSystemNcatClean(t, ctx, source, target, targetRoot)
			t.Log("real SSH policy denied helpers; first TCP data connection rejected; new port transferred exact tree and cleaned owned resources")
		})
	}
}

func TestHostedNcatCancellationPreservesSourceAndBrowser(t *testing.T) {
	source, target := systemOnlyFixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
	t.Cleanup(func() {
		_ = source.Remove(context.Background(), sourceRoot, true)
		_ = target.Remove(context.Background(), targetRoot, true)
	})
	op := transfer.Operation{Source: source, Destination: target, SourcePath: source.Join(sourceRoot, "cancel.bin"), TargetPath: target.Join(targetRoot, "cancel.bin"), Move: true}
	if err := source.Exec(ctx, "head -c 33554432 /dev/urandom > "+shellQuote(op.SourcePath), endpoint.ExecOptions{}); err != nil {
		t.Fatal(err)
	}
	before, err := transfer.Snapshot(ctx, source, op.SourcePath, true)
	if err != nil {
		t.Fatal(err)
	}
	life, stop := context.WithCancel(ctx)
	defer stop()
	connected := make(chan struct{}, 1)
	observed, _ := activity.WithObserver(life, func(stage string, _ int64) {
		if stage == "ncat-connected" {
			select {
			case connected <- struct{}{}:
			default:
			}
			stop() // Real ncat has completed its TCP handshake, not just bound.
		}
	})
	err = runNcatTar(observed, op, strategy.SourcePush)
	select {
	case <-connected:
	default:
		t.Fatal("no real ncat connection was observed before cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	after, err := transfer.Snapshot(ctx, source, op.SourcePath, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := transfer.CompareManifests(before, after, false); err != nil {
		t.Fatal("cancelled move changed its source")
	}
	if _, err := target.Stat(ctx, op.TargetPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("cancelled transfer committed an unverified target")
	}
	assertSystemNcatClean(t, ctx, source, target, targetRoot)
	t.Log("cancelled established data connection; source SHA unchanged, no destination commit, no live ncat or marked workspace, browsing SSH still usable")
}

// Exercise the same five-port controller loop for encrypted streams and the
// authenticated ncat carrier used inside SOCKS/SSH/Hans strategies. This case
// verifies direct routing; it is not evidence of the other route tiers.
func TestHostedDataStreamChangesPortAfterConnectRejection(t *testing.T) {
	source, target, document := fixtureEndpoints(t)
	app := New(filepath.Join(t.TempDir(), "unused.vault"))
	app.document = document
	t.Cleanup(func() { _ = app.Lock() })
	for _, carrier := range []string{"", "ncat"} {
		for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
			t.Run(carrier+"/"+string(direction), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
				t.Cleanup(func() {
					_ = source.Remove(context.Background(), sourceRoot, true)
					_ = target.Remove(context.Background(), targetRoot, true)
				})
				op := transfer.Operation{Source: source, Destination: target, SourcePath: source.Join(sourceRoot, "stream.txt"), TargetPath: target.Join(targetRoot, "stream.txt"), Move: true}
				writeRemoteFile(t, ctx, source, op.SourcePath, "new-port stream with authenticated receipt")
				initiator, peer := source, target
				if direction == strategy.TargetPull {
					initiator, peer = target, source
				}
				rejectCount := rejectNcatDataConnections(t, ctx, initiator, peer, false)
				var mu sync.Mutex
				var ports []string
				observed, _ := activity.WithObserver(ctx, func(stage string, _ int64) {
					if strings.HasPrefix(stage, "stream-port/") {
						mu.Lock()
						ports = append(ports, strings.TrimPrefix(stage, "stream-port/"))
						mu.Unlock()
					}
				})
				if err := app.runAgentStreamWithCarrier(observed, op, mustPreflight(t, ctx, op), direction, false, "", nil, nil, "", carrier); err != nil {
					t.Fatalf("real data connection rejected before new-port recovery: %v", err)
				}
				mu.Lock()
				attempted := append([]string(nil), ports...)
				mu.Unlock()
				if len(attempted) < 2 || len(attempted) > 5 || attempted[0] == attempted[1] || rejectCount() == 0 {
					t.Fatalf("connection failure did not switch ports: %v", attempted)
				}
				assertRemoteFile(t, ctx, target, op.TargetPath, "new-port stream with authenticated receipt")
				if _, err := source.Stat(ctx, op.SourcePath); !errors.Is(err, fs.ErrNotExist) {
					t.Fatal("verified stream move retained source")
				}
				for _, host := range []*endpoint.Remote{source, target} {
					var output bytes.Buffer
					command := "find /tmp -maxdepth 1 -user tester -name '.dragfm-*' -print"
					if host == target {
						command += "; find " + shellQuote(targetRoot) + " -name '*dragfm-partial-*' -print"
					}
					if err := host.Exec(ctx, command, endpoint.ExecOptions{Stdout: &output}); err != nil {
						t.Fatal("browser transport did not survive retries")
					}
					if output.Len() != 0 {
						t.Fatal("stream retry left owned helpers or partials")
					}
				}
			})
		}
	}
}

func systemOnlyFixtureEndpoints(t *testing.T) (*endpoint.Remote, *endpoint.Remote) {
	t.Helper()
	for _, name := range []string{"DRAGFM_E2E_SOURCE_SSH", "DRAGFM_E2E_TARGET_SSH"} {
		value := os.Getenv(name)
		if value == "" {
			t.Skip("disposable SSH fixtures are not configured")
		}
		t.Setenv(name, value+"-system")
	}
	source, target, _ := fixtureEndpoints(t)
	return source, target
}

// Only the disposable endpoint-to-endpoint high-port SYN packets are affected.
// REJECT (not DROP) makes the first connect fail, rather than letting a SYN
// retransmission accidentally make that same connect/port succeed.
func rejectNcatDataConnections(t *testing.T, ctx context.Context, initiator, peer *endpoint.Remote, all bool) func() int {
	t.Helper()
	chain := "DFN" + randomTransferToken(5)
	match := "-d " + shellQuote(peer.ConnectionHost()) + " -p tcp --syn --dport 20000:60999 -j " + chain
	var diagnostic bytes.Buffer
	if err := initiator.Exec(ctx, "sudo -n iptables -w 5 -N "+chain, endpoint.ExecOptions{Stderr: &diagnostic}); err != nil {
		t.Fatalf("create disposable firewall chain: %v: %s", err, diagnostic.String())
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := "sudo -n iptables -w 5 -D OUTPUT " + match + "; sudo -n iptables -w 5 -F " + chain + "; sudo -n iptables -w 5 -X " + chain
		var diagnostic bytes.Buffer
		if err := initiator.Exec(cleanup, command, endpoint.ExecOptions{Stderr: &diagnostic}); err != nil {
			t.Errorf("failed to remove task-owned disposable firewall chain: %v: %s", err, diagnostic.String())
		}
	})
	statistic := " -m statistic --mode nth --every 2 --packet 0"
	if all {
		statistic = ""
	}
	command := "sudo -n iptables -w 5 -A " + chain + " -p tcp" + statistic + " -j REJECT --reject-with tcp-reset && sudo -n iptables -w 5 -A OUTPUT " + match
	diagnostic.Reset()
	if err := initiator.Exec(ctx, command, endpoint.ExecOptions{Stderr: &diagnostic}); err != nil {
		t.Fatalf("install disposable firewall rejection: %v: %s", err, diagnostic.String())
	}
	return func() int {
		var output bytes.Buffer
		var diagnostic bytes.Buffer
		if err := initiator.Exec(ctx, "sudo -n iptables -w 5 -L "+chain+" -n -v -x", endpoint.ExecOptions{Stdout: &output, Stderr: &diagnostic}); err != nil {
			t.Fatalf("read disposable firewall counter: %v: %s", err, diagnostic.String())
		}
		for _, line := range strings.Split(output.String(), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 2 && fields[2] == "REJECT" {
				count, err := strconv.Atoi(fields[0])
				if err != nil {
					t.Fatal(err)
				}
				return count
			}
		}
		t.Fatal("missing real firewall counter")
		return 0
	}
}

func assertSystemNcatClean(t *testing.T, ctx context.Context, source, target *endpoint.Remote, targetRoot string) {
	t.Helper()
	for _, host := range []*endpoint.Remote{source, target} {
		var leftovers bytes.Buffer
		command := "find /tmp -maxdepth 1 -user systemtester -name '.dragfm-*' -print"
		if host == target {
			command += "; find " + shellQuote(targetRoot) + " -name '.dragfm-*' -print"
		}
		if err := host.Exec(ctx, command, endpoint.ExecOptions{Stdout: &leftovers}); err != nil {
			t.Fatal("browsing SSH was closed or resource inspection failed")
		}
		if strings.TrimSpace(leftovers.String()) != "" {
			t.Fatal("system fallback left a marked workspace or partial behind")
		}
		var processes bytes.Buffer
		if err := host.Exec(ctx, "ps -u systemtester -o stat= -o comm=", endpoint.ExecOptions{Stdout: &processes}); err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(processes.String(), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[1] == "ncat" && !strings.HasPrefix(fields[0], "Z") {
				t.Fatal("system fallback left a running ncat process")
			}
		}
		var listeners bytes.Buffer
		if err := host.Exec(ctx, "sudo -n ss -H -ltnp", endpoint.ExecOptions{Stdout: &listeners}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(listeners.String(), `"ncat"`) {
			t.Fatal("system fallback left a listening data port")
		}
	}
	// Successful reads/commands above use the original browser connections,
	// not new connections created solely to conceal accidental cancellation.
}
