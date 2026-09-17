//go:build integration && !windows

package webgui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"golang.org/x/crypto/ssh"
)

func TestRemoteTransferMethodsOnPrivateRunners(t *testing.T) {
	sourceTarget := os.Getenv("DRAGFM_E2E_SOURCE_SSH")
	targetTarget := os.Getenv("DRAGFM_E2E_TARGET_SSH")
	if sourceTarget == "" || targetTarget == "" {
		t.Skip("private SSH integration targets are not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sourceRoute, sourceKeys := openSSHRoute(t, sourceTarget)
	targetRoute, targetKeys := openSSHRoute(t, targetTarget)
	source, err := endpoint.DialSSH(ctx, "integration-source", sourceRoute.Hops[0].HostKey.PinnedSHA256, sourceRoute)
	if err != nil {
		t.Fatalf("dial source: %v", err)
	}
	defer source.Close()
	target, err := endpoint.DialSSH(ctx, "integration-target", targetRoute.Hops[0].HostKey.PinnedSHA256, targetRoute)
	if err != nil {
		t.Fatalf("dial target: %v", err)
	}
	defer target.Close()

	sourceRoot := remoteTempDir(t, ctx, source)
	targetRoot := remoteTempDir(t, ctx, target)
	defer source.Remove(context.Background(), sourceRoot, true)
	defer target.Remove(context.Background(), targetRoot, true)

	app := New(filepath.Join(t.TempDir(), "unused.vault"))
	document := config.NewDocument()
	document.Keys = append(document.Keys, sourceKeys...)
	document.Keys = append(document.Keys, targetKeys...)
	document.Hosts = []config.Host{
		hostFromRoute("source-host", "integration-source", sourceRoute, sourceKeys),
		hostFromRoute("target-host", "integration-target", targetRoute, targetKeys),
	}
	app.document = document

	// Same-machine paths must stay native and absolute even when the endpoint is
	// SSH-backed. Cover cp directory merge and mv before any cross-host route.
	mergeSource := source.Join(sourceRoot, "same-merge-source")
	mergeTarget := source.Join(sourceRoot, "same-merge-target")
	if err := source.MkdirAll(ctx, mergeSource, 0750); err != nil {
		t.Fatal(err)
	}
	if err := source.MkdirAll(ctx, mergeTarget, 0750); err != nil {
		t.Fatal(err)
	}
	writeRemoteFile(t, ctx, source, source.Join(mergeSource, "new.txt"), "native-copy-new")
	writeRemoteFile(t, ctx, source, source.Join(mergeTarget, "old.txt"), "native-copy-old")
	if result, runErr := transfer.Run(ctx, transfer.Operation{Source: source, Destination: source, SourcePath: mergeSource, TargetPath: mergeTarget, Overwrite: true}); runErr != nil || result.Verification != "same-machine-cp" {
		t.Fatalf("same-machine directory copy: result=%#v err=%v", result, runErr)
	}
	assertRemoteFile(t, ctx, source, source.Join(mergeTarget, "new.txt"), "native-copy-new")
	assertRemoteFile(t, ctx, source, source.Join(mergeTarget, "old.txt"), "native-copy-old")
	nativeMoveSource := source.Join(sourceRoot, "same-move-source.txt")
	nativeMoveTarget := source.Join(sourceRoot, "same-move-target.txt")
	writeRemoteFile(t, ctx, source, nativeMoveSource, "native-move")
	if result, runErr := transfer.Run(ctx, transfer.Operation{Source: source, Destination: source, SourcePath: nativeMoveSource, TargetPath: nativeMoveTarget, Move: true}); runErr != nil || !result.Moved || result.Verification != "same-machine-rename" {
		t.Fatalf("same-machine move: result=%#v err=%v", result, runErr)
	}
	assertRemoteFile(t, ctx, source, nativeMoveTarget, "native-move")

	writeRemoteFile(t, ctx, source, source.Join(sourceRoot, "push.txt"), "source-push-scp")
	push := transfer.Operation{Source: source, Destination: target, SourcePath: source.Join(sourceRoot, "push.txt"), TargetPath: target.Join(targetRoot, "push.txt")}
	pushPreflight := mustPreflight(t, ctx, push)
	t.Logf("capabilities source=%q target=%q source-tools=%v target-tools=%v", pushPreflight.SourceCapabilities.Architecture, pushPreflight.TargetCapabilities.Architecture, pushPreflight.SourceCapabilities.Tools, pushPreflight.TargetCapabilities.Tools)
	if pushPreflight.SameMachine {
		t.Fatal("private runner integration endpoints unexpectedly have the same machine identity")
	}
	if err := app.runAgentMethod(ctx, push, pushPreflight, strategy.SourcePush, false, "", nil, nil, strategy.SCP); err != nil {
		t.Fatalf("source push SCP: %v", err)
	}
	assertRemoteFile(t, ctx, target, push.TargetPath, "source-push-scp")

	writeRemoteFile(t, ctx, source, source.Join(sourceRoot, "pull.txt"), "target-pull-scp")
	pull := transfer.Operation{Source: source, Destination: target, SourcePath: source.Join(sourceRoot, "pull.txt"), TargetPath: target.Join(targetRoot, "pull.txt")}
	pullPreflight := mustPreflight(t, ctx, pull)
	if err := app.runAgentMethod(ctx, pull, pullPreflight, strategy.TargetPull, false, "", nil, nil, strategy.SCP); err != nil {
		t.Fatalf("target pull SCP: %v", err)
	}
	assertRemoteFile(t, ctx, target, pull.TargetPath, "target-pull-scp")

	treeSource := source.Join(sourceRoot, "tree")
	if err := source.MkdirAll(ctx, source.Join(treeSource, "nested"), 0750); err != nil {
		t.Fatal(err)
	}
	writeRemoteFile(t, ctx, source, source.Join(treeSource, "nested/data"), "encrypted-tree")
	if err := source.Symlink(ctx, "nested/data", source.Join(treeSource, "link")); err != nil {
		t.Fatal(err)
	}
	tree := transfer.Operation{Source: source, Destination: target, SourcePath: treeSource, TargetPath: target.Join(targetRoot, "tree")}
	treePreflight := mustPreflight(t, ctx, tree)
	if err := app.runAgentStream(ctx, tree, treePreflight, strategy.SourcePush, false, "", nil, nil, ""); err != nil {
		t.Fatalf("encrypted directory stream: %v", err)
	}
	assertRemoteFile(t, ctx, target, target.Join(tree.TargetPath, "nested/data"), "encrypted-tree")
	link, err := target.Stat(ctx, target.Join(tree.TargetPath, "link"))
	if err != nil || link.Mode&fs.ModeSymlink == 0 || link.LinkTarget != "nested/data" {
		t.Fatalf("encrypted stream symlink=%#v err=%v", link, err)
	}

	moveSource := source.Join(sourceRoot, "move.txt")
	writeRemoteFile(t, ctx, source, moveSource, "verified-move")
	move := transfer.Operation{Source: source, Destination: target, SourcePath: moveSource, TargetPath: target.Join(targetRoot, "move.txt"), Move: true}
	movePreflight := mustPreflight(t, ctx, move)
	if err := app.runAgentStream(ctx, move, movePreflight, strategy.TargetPull, false, "", nil, nil, ""); err != nil {
		t.Fatalf("verified target-pull move: %v", err)
	}
	if _, err := source.Stat(ctx, move.SourcePath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("verified move retained source unexpectedly: %v", err)
	}
	assertRemoteFile(t, ctx, target, move.TargetPath, "verified-move")

	relaySource := source.Join(sourceRoot, "relay.txt")
	writeRemoteFile(t, ctx, source, relaySource, "controller-memory-relay")
	relay := transfer.Operation{Source: source, Destination: target, SourcePath: relaySource, TargetPath: target.Join(targetRoot, "relay.txt")}
	if _, err := transfer.Run(ctx, relay); err != nil {
		t.Fatalf("controller memory relay: %v", err)
	}
	assertRemoteFile(t, ctx, target, relay.TargetPath, "controller-memory-relay")

	if proxySpec := os.Getenv("DRAGFM_E2E_SOCKS_SPEC"); proxySpec != "" {
		app.document.SOCKS = []config.SOCKSProxy{{ID: "integration-socks", Name: "integration-socks", Spec: proxySpec}}
		socksSource := source.Join(sourceRoot, "socks.txt")
		writeRemoteFile(t, ctx, source, socksSource, "authenticated-socks-pool")
		socksOperation := transfer.Operation{Source: source, Destination: target, SourcePath: socksSource, TargetPath: target.Join(targetRoot, "socks.txt")}
		socksPreflight := mustPreflight(t, ctx, socksOperation)
		if err := app.runSOCKSPool(ctx, socksOperation, socksPreflight, map[strategy.Direction]string{}); err != nil {
			t.Fatalf("authenticated SOCKS pool: %v", err)
		}
		assertRemoteFile(t, ctx, target, socksOperation.TargetPath, "authenticated-socks-pool")
		if app.document.SOCKS[0].LastSuccess.IsZero() || app.document.SOCKS[0].LastRTT < 0 {
			t.Fatalf("successful SOCKS route was not remembered: %#v", app.document.SOCKS[0])
		}
	}

	if relayTarget := os.Getenv("DRAGFM_E2E_RELAY_SSH"); relayTarget != "" {
		relayRoute, relayKeys := openSSHRoute(t, relayTarget)
		relayHost := hostFromRoute("relay-host", "integration-relay", relayRoute, relayKeys)
		app.document.Keys = append(app.document.Keys, relayKeys...)
		app.document.Hosts = append(app.document.Hosts, relayHost)
		app.sessionSSH[relayHost.ID] = true
		jumpSource := source.Join(sourceRoot, "jump.txt")
		writeRemoteFile(t, ctx, source, jumpSource, "ssh-relay-pool")
		jumpOperation := transfer.Operation{Source: source, Destination: target, SourcePath: jumpSource, TargetPath: target.Join(targetRoot, "jump.txt")}
		jumpPreflight := mustPreflight(t, ctx, jumpOperation)
		if err := app.runJumpPool(ctx, jumpOperation, jumpPreflight, map[strategy.Direction]string{}); err != nil {
			t.Fatalf("SSH relay pool: %v", err)
		}
		assertRemoteFile(t, ctx, target, jumpOperation.TargetPath, "ssh-relay-pool")
		if cached := app.cachedRelayLocked("source-host", "integration-target"); cached != relayHost.ID {
			t.Fatalf("successful SSH relay cache=%q want=%q", cached, relayHost.ID)
		}
	}

	if os.Getenv("DRAGFM_E2E_NCAT") != "" {
		for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
			name := "ncat-" + string(direction) + ".txt"
			ncatSource := source.Join(sourceRoot, name)
			contents := "tar-ncat-" + string(direction)
			writeRemoteFile(t, ctx, source, ncatSource, contents)
			ncatOperation := transfer.Operation{Source: source, Destination: target, SourcePath: ncatSource, TargetPath: target.Join(targetRoot, name)}
			if err := runNcatTar(ctx, ncatOperation, direction); err != nil {
				t.Fatalf("tar+ncat %s: %v", direction, err)
			}
			assertRemoteFile(t, ctx, target, ncatOperation.TargetPath, contents)
		}
	}

	if os.Getenv("DRAGFM_E2E_HANS") != "" {
		hansSource := source.Join(sourceRoot, "hans.txt")
		writeRemoteFile(t, ctx, source, hansSource, "official-hans-v5")
		hansOperation := transfer.Operation{Source: source, Destination: target, SourcePath: hansSource, TargetPath: target.Join(targetRoot, "hans.txt")}
		hansPreflight := mustPreflight(t, ctx, hansOperation)
		if err := app.runHans(ctx, hansOperation, hansPreflight, map[strategy.Direction]string{}); err != nil {
			t.Fatalf("official Hans v5 orchestration: %v", err)
		}
		assertRemoteFile(t, ctx, target, hansOperation.TargetPath, "official-hans-v5")
	}

	if os.Getenv("DRAGFM_E2E_ELEVATED") != "" {
		app.mu.Lock()
		for index := range app.document.Hosts {
			switch app.document.Hosts[index].ID {
			case "source-host":
				app.document.Hosts[index].RootUser = sourceRoute.Hops[len(sourceRoute.Hops)-1].User
			case "target-host":
				app.document.Hosts[index].RootUser = targetRoute.Hops[len(targetRoute.Hops)-1].User
			}
		}
		app.mu.Unlock()
		for _, direction := range []strategy.Direction{strategy.SourcePush, strategy.TargetPull} {
			name := "elevated-" + string(direction) + ".txt"
			contents := "configured-root-route-" + string(direction)
			sourcePath := source.Join(sourceRoot, name)
			writeRemoteFile(t, ctx, source, sourcePath, contents)
			operation := transfer.Operation{Source: source, Destination: target, SourcePath: sourcePath, TargetPath: target.Join(targetRoot, name)}
			preflight := mustPreflight(t, ctx, operation)
			if err := app.runAgentStream(ctx, operation, preflight, direction, true, "", nil, nil, ""); err != nil {
				t.Fatalf("configured root route %s: %v", direction, err)
			}
			assertRemoteFile(t, ctx, target, operation.TargetPath, contents)
		}
	}
}

func openSSHRoute(t *testing.T, target string) (connector.Route, []config.PrivateKey) {
	t.Helper()
	output, err := exec.Command("ssh", "-G", target).Output()
	if err != nil {
		t.Fatalf("resolve OpenSSH target: %v", err)
	}
	values := make(map[string][]string)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			values[fields[0]] = append(values[fields[0]], strings.Join(fields[1:], " "))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(firstValue(values["port"]))
	hop := connector.Hop{Host: firstValue(values["hostname"]), Port: port, User: firstValue(values["user"]), Credentials: connector.Credentials{UseAgent: true}}
	var keys []config.PrivateKey
	for index, name := range values["identityfile"] {
		name = strings.TrimSpace(strings.Replace(name, "~", userHome(t), 1))
		data, readErr := os.ReadFile(name)
		if readErr != nil {
			continue
		}
		if _, parseErr := ssh.ParsePrivateKey(data); parseErr != nil {
			continue
		}
		id := fmt.Sprintf("integration-key-%s-%d", safeIdentifier(hop.Host), index)
		keys = append(keys, config.PrivateKey{ID: id, Name: id, PEM: string(data)})
		hop.Credentials.PrivateKeys = append(hop.Credentials.PrivateKeys, connector.PrivateKey{PEM: data})
	}
	if len(hop.Credentials.PrivateKeys) == 0 && os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Fatal("OpenSSH target has no Go-readable private key or SSH agent")
	}
	var fingerprint string
	hop.HostKey.ConfirmNew = func(_ string, value string) bool { fingerprint = value; return true }
	route := connector.Route{Hops: []connector.Hop{hop}, Timeout: 12 * time.Second}
	probe, err := connector.Dial(context.Background(), route)
	if err != nil {
		t.Fatalf("probe SSH route: %v", err)
	}
	_ = probe.Close()
	if fingerprint == "" {
		t.Fatal("SSH route returned no host fingerprint")
	}
	route.Hops[0].HostKey = connector.HostKeyPolicy{PinnedSHA256: fingerprint}
	return route, keys
}

func hostFromRoute(id, name string, route connector.Route, keys []config.PrivateKey) config.Host {
	hop := route.Hops[0]
	keyIDs := make([]string, 0, len(keys))
	for _, key := range keys {
		keyIDs = append(keyIDs, key.ID)
	}
	return config.Host{ID: id, Name: name, Address: hop.Host, Port: hop.Port, User: hop.User, KeyIDs: keyIDs, HostFingerprint: hop.HostKey.PinnedSHA256}
}

func remoteTempDir(t *testing.T, ctx context.Context, remote *endpoint.Remote) string {
	t.Helper()
	var output bytes.Buffer
	if err := remote.Exec(ctx, "mktemp -d /var/tmp/dragfm-integration.XXXXXX", endpoint.ExecOptions{Stdout: &output}); err != nil {
		t.Fatal(err)
	}
	value := strings.TrimSpace(output.String())
	if !strings.HasPrefix(value, "/var/tmp/dragfm-integration.") {
		t.Fatalf("unsafe remote integration directory %q", value)
	}
	return value
}

func writeRemoteFile(t *testing.T, ctx context.Context, remote endpoint.Endpoint, target, contents string) {
	t.Helper()
	writer, err := remote.CreateAtomic(ctx, target, 0640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(contents)); err != nil {
		_ = writer.Abort()
		t.Fatal(err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
}

func assertRemoteFile(t *testing.T, ctx context.Context, remote endpoint.Endpoint, target, expected string) {
	t.Helper()
	reader, err := remote.Open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	_, copyErr := output.ReadFrom(reader)
	if err := errors.Join(copyErr, reader.Close()); err != nil {
		t.Fatal(err)
	}
	if output.String() != expected {
		t.Fatalf("remote file contents=%q want=%q", output.String(), expected)
	}
}

func mustPreflight(t *testing.T, ctx context.Context, operation transfer.Operation) transfer.PreflightReport {
	t.Helper()
	report, err := transfer.Preflight(ctx, operation)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func firstValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func userHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func safeIdentifier(value string) string {
	return strings.NewReplacer(".", "-", ":", "-", "[", "", "]", "").Replace(value)
}
