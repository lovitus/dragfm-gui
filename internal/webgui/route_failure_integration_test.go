//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

func TestHostedReviewedRouteFailureRecovery(t *testing.T) {
	source, target, document := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
	defer source.Remove(context.Background(), sourceRoot, true)
	defer target.Remove(context.Background(), targetRoot, true)
	app := New(filepath.Join(t.TempDir(), "unused.vault"))
	defer app.Lock()
	app.document = document
	app.document.SOCKS = []config.SOCKSProxy{{ID: "fixture-proxy", Name: "fixture-proxy", Spec: os.Getenv("DRAGFM_E2E_SOCKS_SPEC")}}
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
	approval := func(context.Context, strategy.Risk, strategy.Attempt) error { return nil }
	if err := strategy.Execute(ctx, attempts, approval, func(e strategy.Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	var directFailures int
	var winner strategy.Tier
	for _, event := range events {
		if event.Attempt.Tier == strategy.Direct && event.Stage == "failed" {
			directFailures++
		}
		if event.Stage == "succeeded" {
			winner = event.Attempt.Tier
		}
	}
	if directFailures != len(directRemotePlan(preflight, true)) || winner != strategy.SOCKSPool {
		t.Fatalf("wrong actual fallback: direct failures=%d winner=%s", directFailures, winner)
	}
	assertRemoteFile(t, ctx, target, operation.TargetPath, "verified full-strategy TCP-block fallback")
	assertRemoteFile(t, ctx, source, operation.SourcePath, "verified full-strategy TCP-block fallback")
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
}

func TestHostedReviewedFailedRelayCacheReprobes(t *testing.T) {
	source, target, document := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
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
	app.mu.RLock()
	cached := app.cachedRelayLocked(sourceHost.ID, target.Name())
	app.mu.RUnlock()
	if cached != valid.ID {
		t.Fatalf("cache not replaced: %s", cached)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(fmt.Sprint(stages), "jump-probe") {
		t.Fatalf("no actual re-probe: %v", stages)
	}
}
