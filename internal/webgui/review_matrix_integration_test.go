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

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/activity"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/routespec"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

// Each cell selects a real transport directly: a successful earlier rsync
// attempt must not hide untested SCP/encrypted/ncat paths behind one green test.
func TestHostedReviewedTransportMatrix(t *testing.T) {
	source, target, document := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
	t.Cleanup(func() {
		_ = source.Exec(context.Background(), "sudo -n rm -rf -- "+shellQuote(sourceRoot), endpoint.ExecOptions{})
		_ = target.Exec(context.Background(), "sudo -n rm -rf -- "+shellQuote(targetRoot), endpoint.ExecOptions{})
	})
	app := New(filepath.Join(t.TempDir(), "unused.vault"))
	defer app.Lock()
	app.document = document
	proxy, err := routespec.ParseSOCKS(os.Getenv("DRAGFM_E2E_SOCKS_SPEC"))
	if err != nil {
		t.Fatal(err)
	}
	relayRoute, relayKeys := openSSHRoute(t, os.Getenv("DRAGFM_E2E_RELAY_SSH"))
	app.document.Keys = append(app.document.Keys, relayKeys...)
	app.document.Hosts = append(app.document.Hosts, hostFromRoute("matrix-relay", "matrix-relay", relayRoute, relayKeys))
	type routeCase struct {
		name   string
		socks  *connector.SOCKS5
		prefix []connector.Hop
	}
	routes := []routeCase{{name: "direct"}, {name: "authenticated-socks", socks: &proxy}, {name: "ssh-jump", prefix: relayRoute.Hops}}
	methods := []strategy.Method{strategy.Rsync, strategy.SCP, strategy.EncryptedStream, strategy.NcatTar}
	for _, route := range routes {
		for _, elevated := range []bool{false, true} {
			for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
				for _, method := range methods {
					name := fmt.Sprintf("%s/%s/elevated=%t/%s", route.name, direction, elevated, method)
					t.Run(name, func(t *testing.T) {
						leaf := strings.ReplaceAll(name, "/", "-") + ".txt"
						sourcePath, targetPath := source.Join(sourceRoot, leaf), target.Join(targetRoot, leaf)
						contents := strings.Repeat("exact transport: "+name+"\n", 100)
						writeRemoteFile(t, ctx, source, sourcePath, contents)
						if elevated && direction == strategy.SourcePush {
							if err := source.Exec(ctx, "sudo -n chown root:root -- "+shellQuote(sourcePath)+" && sudo -n chmod 0600 -- "+shellQuote(sourcePath), endpoint.ExecOptions{}); err != nil {
								t.Fatal(err)
							}
						}
						if elevated && direction == strategy.TargetPull {
							protected := target.Join(targetRoot, leaf+".protected")
							if err := target.Exec(ctx, "sudo -n mkdir -m 0700 -- "+shellQuote(protected), endpoint.ExecOptions{}); err != nil {
								t.Fatal(err)
							}
							targetPath = target.Join(protected, leaf)
						}
						operation := transfer.Operation{Source: source, Destination: target, SourcePath: sourcePath, TargetPath: targetPath}
						report := mustPreflight(t, ctx, operation)
						if method == strategy.EncryptedStream || method == strategy.NcatTar {
							err = app.runAgentStreamWithCarrier(ctx, operation, report, direction, elevated, "", route.socks, route.prefix, "", carrierForMethod(method))
						} else {
							err = app.runAgentMethod(ctx, operation, report, direction, elevated, "", route.socks, route.prefix, method)
						}
						if err != nil {
							t.Fatal(err)
						}
						var result bytes.Buffer
						if err := target.Exec(ctx, "sudo -n cat -- "+shellQuote(targetPath), endpoint.ExecOptions{Stdout: &result}); err != nil {
							t.Fatal(err)
						}
						if result.String() != contents {
							t.Fatal("transport changed file contents")
						}
						if _, err := source.Stat(ctx, sourcePath); err != nil {
							t.Fatalf("copy removed source: %v", err)
						}
					})
				}
			}
		}
	}
	// Reusing a known-good jump is observable and must not first probe alternatives.
	app.mu.Lock()
	app.sessionSSH["matrix-relay"] = true
	app.mu.Unlock()
	app.rememberRelay("source", target.Name(), "matrix-relay")
	sourcePath, targetPath := source.Join(sourceRoot, "cached.txt"), target.Join(targetRoot, "cached.txt")
	writeRemoteFile(t, ctx, source, sourcePath, "cached-before-probing")
	operation := transfer.Operation{Source: source, Destination: target, SourcePath: sourcePath, TargetPath: targetPath}
	var mu sync.Mutex
	var stages []string
	observed, _ := activity.WithObserver(ctx, func(stage string, _ int64) { mu.Lock(); stages = append(stages, stage); mu.Unlock() })
	if err := app.runJumpPool(observed, operation, mustPreflight(t, ctx, operation), nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	cached := false
	for _, stage := range stages {
		if stage == "jump-probe" {
			t.Fatal("cached relay caused an unnecessary probe")
		}
		if stage == "jump-cache-reuse" {
			cached = true
		}
	}
	if !cached {
		t.Fatal("cache reuse not observed")
	}
}

func TestHostedReviewedHansRolesAndMethods(t *testing.T) {
	if os.Getenv("DRAGFM_E2E_HANS") == "" {
		t.Skip("official Hans fixture disabled")
	}
	source, target, document := fixtureEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
	defer source.Remove(context.Background(), sourceRoot, true)
	defer target.Remove(context.Background(), targetRoot, true)
	app := New(filepath.Join(t.TempDir(), "unused.vault"))
	defer app.Lock()
	app.document = document
	for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
		for _, method := range []strategy.Method{strategy.Rsync, strategy.SCP, strategy.EncryptedStream, strategy.NcatTar} {
			t.Run(string(direction)+"/"+string(method), func(t *testing.T) {
				name := string(direction) + "-" + string(method)
				sourcePath, targetPath := source.Join(sourceRoot, name), target.Join(targetRoot, name)
				writeRemoteFile(t, ctx, source, sourcePath, "official Hans "+name)
				operation := transfer.Operation{Source: source, Destination: target, SourcePath: sourcePath, TargetPath: targetPath}
				report := mustPreflight(t, ctx, operation)
				server, client := target, source
				serverArch, clientArch := report.TargetCapabilities.Architecture, report.SourceCapabilities.Architecture
				if direction == strategy.TargetPull {
					server, client = source, target
					serverArch, clientArch = clientArch, serverArch
				}
				if err := app.runHansRoleMethods(ctx, operation, report, server, client, serverArch, clientArch, direction, "", "", []strategy.Method{method}); err != nil {
					t.Fatal(err)
				}
				assertRemoteFile(t, ctx, target, targetPath, "official Hans "+name)
			})
		}
	}
}
