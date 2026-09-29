//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/routespec"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

func TestHostedReviewedRouteFailureRecovery(t *testing.T) {
	// Cases 2/3 keep the ncat-only fallback contract; 4/5 allow installed
	// native tools while both peers still reject uploaded executables.
	for _, unavailable := range []int{0, 1, 2, 3, 4, 5} {
		t.Run(fmt.Sprintf("fixture-case=%d", unavailable), func(t *testing.T) {
			sourceWithoutAgent := unavailable > 0
			if sourceWithoutAgent {
				value := os.Getenv("DRAGFM_E2E_SOURCE_SSH")
				if value == "" {
					t.Skip("disposable SSH fixtures are not configured")
				}
				t.Setenv("DRAGFM_E2E_SOURCE_SSH", value+"-system")
			}
			if unavailable >= 2 {
				value := os.Getenv("DRAGFM_E2E_TARGET_SSH")
				if value == "" {
					t.Skip("disposable SSH fixtures are not configured")
				}
				t.Setenv("DRAGFM_E2E_TARGET_SSH", value+"-system")
			}
			source, target, document := fixtureEndpoints(t)
			fixtureStarted := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if unavailable == 2 || unavailable == 3 {
				for _, remote := range []*endpoint.Remote{source, target} {
					for _, tool := range []string{"/usr/bin/rsync", "/usr/bin/scp"} {
						info, err := remote.Stat(ctx, tool)
						if err != nil || info.Mode.Perm()&0111 == 0 {
							t.Fatal("fixture native tool unavailable before forced ncat fallback")
						}
						if err := remote.Exec(ctx, "sudo -n chmod 0644 "+shellQuote(tool), endpoint.ExecOptions{}); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() {
							cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
							defer stop()
							_ = remote.Exec(cleanup, fmt.Sprintf("sudo -n chmod %04o -- %s", info.Mode.Perm(), shellQuote(tool)), endpoint.ExecOptions{})
						})
					}
				}
			}
			if sourceWithoutAgent {
				home, err := source.Home(ctx)
				if err != nil {
					t.Fatal(err)
				}
				_ = source.Remove(ctx, source.Join(home, ".helper-denied"), false)
			}
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			defer source.Remove(context.Background(), sourceRoot, true)
			defer target.Remove(context.Background(), targetRoot, true)
			app := New(filepath.Join(t.TempDir(), "unused.vault"))
			defer app.Lock()
			app.document = document
			app.document.SOCKS = []config.SOCKSProxy{{ID: "fixture-proxy", Name: "fixture-proxy", Spec: os.Getenv("DRAGFM_E2E_SOCKS_SPEC")}}
			if unavailable == 3 || unavailable == 5 {
				// Force the system carrier to pull: only the source's access to
				// the actual configured proxy is blocked, not control SSH.
				proxy, err := routespec.ParseSOCKS(os.Getenv("DRAGFM_E2E_SOCKS_SPEC"))
				if err != nil {
					t.Fatal(err)
				}
				host, port, err := net.SplitHostPort(proxy.Address)
				if err != nil || net.ParseIP(host) == nil {
					t.Fatal("fixture proxy must identify an isolated container IP")
				}
				rule := "OUTPUT -d " + shellQuote(host) + " -p tcp --dport " + shellQuote(port) + " -j REJECT --reject-with tcp-reset"
				if err := source.Exec(ctx, "sudo -n iptables -I "+rule, endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
				defer source.Exec(context.Background(), "sudo -n iptables -D "+rule, endpoint.ExecOptions{})
			}
			// Only these two disposable containers are affected. Rejecting peer TCP
			// leaves the controller and relay connections intact, forcing real fallback
			// instead of choosing a method that merely returns a mocked failure.
			for _, pair := range []struct{ owner, peer *endpoint.Remote }{{source, target}, {target, source}} {
				owner, peer := pair.owner, pair.peer
				chain := "DF" + randomTransferToken(5)
				create := "sudo -n iptables -N " + chain + "; sudo -n iptables -A " + chain + " -p tcp -j REJECT --reject-with tcp-reset; sudo -n iptables -A OUTPUT -d " + shellQuote(peer.ConnectionHost()) + " -j " + chain
				if err := owner.Exec(ctx, create, endpoint.ExecOptions{}); err != nil {
					t.Fatalf("isolated network rule: %v", err)
				}
				defer owner.Exec(context.Background(), "sudo -n iptables -D OUTPUT -d "+shellQuote(peer.ConnectionHost())+" -j "+chain+"; sudo -n iptables -F "+chain+"; sudo -n iptables -X "+chain, endpoint.ExecOptions{})
			}
			operation := transfer.Operation{Source: source, Destination: target, SourcePath: source.Join(sourceRoot, "fallback.txt"), TargetPath: target.Join(targetRoot, "fallback.txt")}
			writeRemoteFile(t, ctx, source, operation.SourcePath, "verified full-strategy TCP-block fallback")
			preflight := mustPreflight(t, ctx, operation)
			attempts, _ := app.transferAttempts(operation, preflight)
			var events []strategy.Event
			strategyStarted := time.Now()
			defer func() {
				if t.Failed() {
					deadline, _ := ctx.Deadline()
					t.Logf("fixture timing: prepared=%s strategy=%s deadline=%s observed=%s", strategyStarted.Sub(fixtureStarted), time.Since(strategyStarted), deadline.Format(time.RFC3339Nano), time.Now().Format(time.RFC3339Nano))
					redactError := knownSecretRedactor(app.document, nil, "")
					for _, event := range events {
						// Elapsed is the existing whole attempt measurement, including
						// its deferred cleanup, not just a remote tool runtime.
						t.Logf("fixture strategy: tier=%s method=%s direction=%s elevated=%t stage=%s started=%s elapsed=%s error=%s", event.Attempt.Tier, event.Attempt.Method, event.Attempt.Direction, event.Attempt.Elevated, event.Stage, event.Started.Format(time.RFC3339Nano), event.Elapsed, redactError(fmt.Sprint(event.Error)))
					}
				}
			}()
			approval := func(context.Context, strategy.Risk, strategy.Attempt) error { return nil }
			if err := strategy.Execute(ctx, attempts, approval, func(e strategy.Event) { events = append(events, e) }); err != nil {
				t.Fatal(err)
			}
			var directFailures int
			var winner strategy.Tier
			var concreteWinner bool
			for _, event := range events {
				if event.Attempt.Tier == strategy.Direct && event.Stage == "failed" {
					directFailures++
				}
				if event.Stage == "succeeded" {
					winner = event.Attempt.Tier
					if event.Attempt.Tier == strategy.SOCKSPool && event.Attempt.RouteName == "fixture-proxy" && event.Attempt.Direction != "" && event.Attempt.Method != "" {
						expectedMethod, expectedDirection := strategy.Rsync, strategy.SourcePush
						if unavailable == 2 || unavailable == 3 {
							expectedMethod = strategy.NcatTar
						}
						if unavailable == 3 || unavailable == 5 {
							expectedDirection = strategy.TargetPull
						}
						concreteWinner = event.Attempt.Method == expectedMethod && event.Attempt.Direction == expectedDirection
					}
				}
			}
			if directFailures != len(directRemotePlan(preflight, true)) || winner != strategy.SOCKSPool {
				t.Fatalf("wrong actual fallback: direct failures=%d winner=%s", directFailures, winner)
			}
			if !concreteWinner {
				t.Fatal("real SOCKS transfer succeeded but its actual candidate, direction and method were absent from the job observer")
			}
			assertRemoteFile(t, ctx, target, operation.TargetPath, "verified full-strategy TCP-block fallback")
			assertRemoteFile(t, ctx, source, operation.SourcePath, "verified full-strategy TCP-block fallback")
			if sourceWithoutAgent {
				home, err := source.Home(ctx)
				if err != nil {
					t.Fatal(err)
				}
				assertRemoteFile(t, ctx, source, source.Join(home, ".helper-denied"), "denied\n")
			}
			for _, host := range []*endpoint.Remote{source, target} {
				var leftovers bytes.Buffer
				if err := host.Exec(ctx, "find /tmp -maxdepth 1 -name '.dragfm-*' -print", endpoint.ExecOptions{Stdout: &leftovers}); err != nil {
					t.Fatal(err)
				}
				if strings.TrimSpace(leftovers.String()) != "" {
					t.Fatalf("helper resources remain: %s", leftovers.String())
				}
			}
			t.Logf("all %d direct direction/privilege/method attempts failed under real TCP blocking; authenticated SOCKS recovered; helpers cleaned", directFailures)
		})
	}
}

func TestHostedReviewedFailedRelayCacheReprobes(t *testing.T) {
	for _, unavailable := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("endpoints-without-agent=%d", unavailable), func(t *testing.T) {
			sourceWithoutAgent := unavailable > 0
			if sourceWithoutAgent {
				value := os.Getenv("DRAGFM_E2E_SOURCE_SSH")
				if value == "" {
					t.Skip("disposable SSH fixtures are not configured")
				}
				t.Setenv("DRAGFM_E2E_SOURCE_SSH", value+"-system")
			}
			if unavailable == 2 {
				value := os.Getenv("DRAGFM_E2E_TARGET_SSH")
				if value == "" {
					t.Skip("disposable SSH fixtures are not configured")
				}
				t.Setenv("DRAGFM_E2E_TARGET_SSH", value+"-system")
			}
			source, target, document := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if sourceWithoutAgent {
				home, err := source.Home(ctx)
				if err != nil {
					t.Fatal(err)
				}
				_ = source.Remove(ctx, source.Join(home, ".helper-denied"), false)
			}
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			defer source.Remove(context.Background(), sourceRoot, true)
			defer target.Remove(context.Background(), targetRoot, true)
			app := New(filepath.Join(t.TempDir(), "unused.vault"))
			defer app.Lock()
			app.document = document
			relayRoute, relayKeys := openSSHRoute(t, os.Getenv("DRAGFM_E2E_RELAY_SSH"))
			app.document.Keys = append(app.document.Keys, relayKeys...)
			valid := hostFromRoute("good-relay", "good-relay", relayRoute, relayKeys)
			invalid := valid
			invalid.ID, invalid.Name = "failed-relay", "failed-relay"
			// A disabled cached host is no longer an eligible route. Its stale cache
			// entry must be discarded before actual probes of eligible saved sessions.
			invalid.Disabled = true
			app.document.Hosts = append(app.document.Hosts, invalid, valid)
			app.sessionSSH[valid.ID], app.sessionSSH[invalid.ID] = true, true
			sourceHost, ok := document.HostByName(source.Name())
			if !ok {
				t.Fatal("source configuration missing")
			}
			app.rememberRelay(sourceHost.ID, target.Name(), invalid.ID)
			op := transfer.Operation{Source: source, Destination: target, SourcePath: source.Join(sourceRoot, "cache-recovery"), TargetPath: target.Join(targetRoot, "cache-recovery")}
			writeRemoteFile(t, ctx, source, op.SourcePath, "new eligible relay")
			var mu sync.Mutex
			var stages []string
			observed, _ := activity.WithObserver(ctx, func(stage string, _ int64) { mu.Lock(); stages = append(stages, stage); mu.Unlock() })
			if err := app.runJumpPool(observed, op, mustPreflight(t, ctx, op), nil); err != nil {
				t.Fatal(err)
			}
			assertRemoteFile(t, ctx, target, op.TargetPath, "new eligible relay")
			if sourceWithoutAgent {
				home, err := source.Home(ctx)
				if err != nil {
					t.Fatal(err)
				}
				assertRemoteFile(t, ctx, source, source.Join(home, ".helper-denied"), "denied\n")
			}
			app.mu.RLock()
			cached := app.cachedRelayLocked(sourceHost.ID, target.Name())
			app.mu.RUnlock()
			if cached != valid.ID {
				t.Fatalf("cache not replaced: %s", cached)
			}
			mu.Lock()
			probed := strings.Contains(fmt.Sprint(stages), "jump-probe")
			stages = nil
			mu.Unlock()
			if !probed {
				t.Fatal("no actual re-probe after invalid cached route")
			}
			if unavailable == 2 {
				// The next actual transfer must reuse the successful pair cache,
				// without another TCP probe pass over eligible SSH sessions.
				op.TargetPath = target.Join(targetRoot, "cached-again")
				if err := app.runJumpPool(observed, op, mustPreflight(t, ctx, op), nil); err != nil {
					t.Fatal(err)
				}
				assertRemoteFile(t, ctx, target, op.TargetPath, "new eligible relay")
				mu.Lock()
				last := fmt.Sprint(stages)
				mu.Unlock()
				if !strings.Contains(last, "jump-cache-reuse") || strings.Contains(last, "jump-probe") {
					t.Fatal("working system relay cache caused an unnecessary probe pass")
				}
			}
		})
	}
}
