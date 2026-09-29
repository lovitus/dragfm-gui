//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

// A saved controller key is accepted only from the real controller address.
// Each remote has a different locally generated default key, which the peer
// accepts. Thus a copied vault key or controller relay cannot masquerade as
// actual initiating-user passwordless SCP/rsync. All accounts/network mutations
// below belong solely to the disposable GitHub-hosted OpenSSH fixtures.
func TestHostedInitiatingUserPasswordlessQueuedTransfer(t *testing.T) {
	for _, mode := range []struct{ scp, pull, system, root bool }{
		{}, {scp: true}, {pull: true}, {scp: true, pull: true},
		{system: true}, {system: true, scp: true}, {system: true, pull: true}, {system: true, scp: true, pull: true},
		{root: true}, {root: true, scp: true, pull: true}, {root: true, system: true, scp: true}, {root: true, system: true, pull: true},
	} {
		t.Run(fmt.Sprintf("scp=%t/pull=%t/system=%t/root=%t", mode.scp, mode.pull, mode.system, mode.root), func(t *testing.T) {
			if mode.system {
				for _, name := range []string{"DRAGFM_E2E_SOURCE_SSH", "DRAGFM_E2E_TARGET_SSH"} {
					value := os.Getenv(name)
					if value == "" {
						t.Skip("disposable SSH fixtures are not configured")
					}
					t.Setenv(name, value+"-system")
				}
			}
			source, target, document := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if mode.root {
				ordinary, _ := openSSHRoute(t, os.Getenv("DRAGFM_E2E_SOURCE_SSH"))
				ordinary.Hops[len(ordinary.Hops)-1].User = "root"
				ordinary.Hops[len(ordinary.Hops)-1].Credentials.UseAgent = false
				if chain, err := connector.Dial(ctx, ordinary); err == nil {
					_ = chain.Close()
					t.Fatal("root fixture accepts the controller's ordinary account key")
				}
			}
			remotes := []*endpoint.Remote{source, target}
			type identity struct{ home, public, original, origin, rootOriginal string }
			identities := make([]identity, len(remotes))
			for i, remote := range remotes {
				home, err := remote.Home(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if mode.system {
					if err := remote.Remove(ctx, remote.Join(home, ".helper-denied"), false); err != nil && !errors.Is(err, fs.ErrNotExist) {
						t.Fatal(err)
					}
				}
				keyHome := home
				if mode.root {
					keyHome = "/root" // Isolated Ubuntu fixture account, never a user host.
				}
				keyPath := remote.Join(keyHome, ".ssh/id_ed25519")
				for _, name := range []string{keyPath, keyPath + ".pub"} {
					if mode.root {
						check := "test ! -e " + shellQuote(name) + " && test ! -L " + shellQuote(name)
						if err := remote.Exec(ctx, "sudo -n sh -c "+shellQuote(check), endpoint.ExecOptions{}); err != nil {
							t.Fatal("root fixture default identity path is not absent")
						}
					} else if _, err := remote.Stat(ctx, name); !errors.Is(err, fs.ErrNotExist) {
						t.Fatal("fixture default identity path is not absent")
					}
				}
				var output bytes.Buffer
				command := "ssh-keygen -q -t ed25519 -N '' -f " + shellQuote(keyPath) + " && cat -- " + shellQuote(keyPath+".pub")
				if mode.root {
					command = "sudo -n sh -c " + shellQuote(command)
				}
				if err := remote.Exec(ctx, command, endpoint.ExecOptions{Stdout: &output}); err != nil {
					t.Fatal(err)
				}
				public := strings.TrimSpace(output.String())
				if !strings.HasPrefix(public, "ssh-ed25519 ") || strings.ContainsAny(public, "\r\n") {
					t.Fatal("fixture did not generate one public identity")
				}
				t.Cleanup(func() {
					cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
					defer stop()
					if mode.root {
						_ = remote.Exec(cleanup, "sudo -n rm -f -- "+shellQuote(keyPath)+" "+shellQuote(keyPath+".pub"), endpoint.ExecOptions{})
					} else {
						_ = remote.Remove(cleanup, keyPath, false)
						_ = remote.Remove(cleanup, keyPath+".pub", false)
					}
				})
				output.Reset()
				if err := remote.Exec(ctx, `printf '%s' "${SSH_CONNECTION%% *}"`, endpoint.ExecOptions{Stdout: &output}); err != nil {
					t.Fatal(err)
				}
				origin := strings.TrimSpace(output.String())
				if net.ParseIP(origin) == nil || origin == source.ConnectionHost() || origin == target.ConnectionHost() {
					t.Fatal("fixture did not identify a distinct controller origin")
				}
				authorized := remote.Join(home, ".ssh/authorized_keys")
				reader, err := remote.Open(ctx, authorized)
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(io.LimitReader(reader, 65537))
				_ = reader.Close()
				if err != nil || len(data) > 65536 {
					t.Fatal("cannot retain fixture authorization for scoped restoration")
				}
				original := string(data)
				t.Cleanup(func() {
					cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
					defer stop()
					writeRemoteFile(t, cleanup, remote, authorized, original)
				})
				identities[i] = identity{home: home, public: public, original: original, origin: origin}
				if mode.root {
					output.Reset()
					if err := remote.Exec(ctx, "sudo -n head -c 65537 /root/.ssh/authorized_keys", endpoint.ExecOptions{Stdout: &output}); err != nil || output.Len() > 65536 {
						t.Fatal("cannot retain isolated root authorization for restoration")
					}
					rootOriginal := output.String()
					identities[i].rootOriginal = rootOriginal
					t.Cleanup(func() {
						cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
						defer stop()
						if err := remote.Exec(cleanup, "sudo -n tee /root/.ssh/authorized_keys >/dev/null", endpoint.ExecOptions{Stdin: strings.NewReader(rootOriginal)}); err != nil {
							t.Error("cannot restore isolated root authorized_keys")
						}
					})
				}
			}
			for i, remote := range remotes {
				identity := identities[i]
				var rules []string
				for _, line := range strings.Split(strings.TrimSpace(identity.original), "\n") {
					// The image provisions a single raw public-key line, no options.
					if !strings.HasPrefix(line, "ssh-ed25519 ") {
						t.Fatal("unexpected fixture authorized_keys format")
					}
					rules = append(rules, "from=\""+identity.origin+"\" "+line)
				}
				if mode.root {
					// A root default key is accepted ONLY as root on the peer.
					// It cannot rescue the old root->ordinary-user implementation.
					rootKeys := identity.rootOriginal + "\n" + identities[1-i].public + "\n"
					if err := remote.Exec(ctx, "sudo -n tee /root/.ssh/authorized_keys >/dev/null", endpoint.ExecOptions{Stdin: strings.NewReader(rootKeys)}); err != nil {
						t.Fatal(err)
					}
				} else {
					rules = append(rules, identities[1-i].public)
				}
				writeRemoteFile(t, ctx, remote, remote.Join(identity.home, ".ssh/authorized_keys"), strings.Join(rules, "\n")+"\n")
				// One new real connection establishes that controller login still
				// works after source-address restrictions, not just a cached socket.
				owned, err := remote.Fork(ctx)
				if err != nil {
					t.Fatal("scoped fixture policy broke controller login")
				}
				_ = owned.Close()
			}
			if mode.scp {
				for _, remote := range remotes {
					info, err := remote.Stat(ctx, "/usr/bin/rsync")
					if err != nil || info.Mode.Perm()&0111 == 0 {
						t.Fatal("disposable system rsync is unavailable before forced fallback")
					}
					if err := remote.Exec(ctx, "sudo -n chmod 0644 /usr/bin/rsync", endpoint.ExecOptions{}); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
						defer stop()
						_ = remote.Exec(cleanup, fmt.Sprintf("sudo -n chmod %04o /usr/bin/rsync", info.Mode.Perm()), endpoint.ExecOptions{})
					})
				}
			}
			if mode.pull {
				if net.ParseIP(target.ConnectionHost()) == nil {
					t.Fatal("fixture target is not an isolated IP")
				}
				rule := "OUTPUT -d " + shellQuote(target.ConnectionHost()) + " -p tcp --dport 22 -j REJECT --reject-with tcp-reset"
				if err := source.Exec(ctx, "sudo -n iptables -I "+rule, endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
					defer stop()
					_ = source.Exec(cleanup, "sudo -n iptables -D "+rule, endpoint.ExecOptions{})
				})
			}
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			for i, root := range []string{sourceRoot, targetRoot} {
				remote := remotes[i]
				t.Cleanup(func() {
					cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
					defer stop()
					_ = remote.Remove(cleanup, root, true)
				})
			}
			// A real sibling matches this name if a carrier accidentally lets
			// rsync expand it as a glob. Shell quoting alone is insufficient
			// with -s; check exact selection through the public queue as well
			// as spaces, quotes, Unicode and a literal backslash.
			name := "payload ' [a中] * ? \\ $literal"
			decoy := source.Join(sourceRoot, "payload ' a expanded x  $literal")
			const decoyContents = "unselected sibling must remain untouched\n"
			writeRemoteFile(t, ctx, source, decoy, decoyContents)
			from, to := source.Join(sourceRoot, name), target.Join(targetRoot, name)
			fromFile, toFile := from, to
			if mode.pull {
				if err := source.MkdirAll(ctx, source.Join(from, ".empty"), 0750); err != nil {
					t.Fatal(err)
				}
				leaf := "leaf ' [空格] $literal"
				fromFile, toFile = source.Join(from, leaf), target.Join(to, leaf)
				if !mode.scp {
					if err := source.Symlink(ctx, leaf, source.Join(from, "link")); err != nil {
						t.Fatal(err)
					}
				}
			}
			const contents = "remote-owned key used for the real transfer\n"
			writeRemoteFile(t, ctx, source, fromFile, contents)
			app := unlockedTestApp(t)
			terminal := make(chan JobUpdateModel, 2)
			var approvalMu sync.Mutex
			approved := make(map[string]bool)
			var failureMu sync.Mutex
			var attemptFailures []string
			var lastUpdate JobUpdateModel
			app.mu.Lock()
			app.document = document.Clone()
			app.eventSink = func(name string, value any) {
				if name == "challenge" {
					challenge := value.(ChallengeModel)
					if mode.root && challenge.Kind == "password" && (challenge.Title == "源端提权 · "+source.Name() || challenge.Title == "目标端提权 · "+target.Name()) {
						approvalMu.Lock()
						approved[challenge.Title] = true
						approvalMu.Unlock()
						app.ResolveChallenge(challenge.ID, true, "", false)
					} else if challenge.Kind == "confirm" {
						if skip, ok := any(app).(interface{ SkipChallenge(string) }); ok {
							skip.SkipChallenge(challenge.ID)
						} else {
							app.ResolveChallenge(challenge.ID, false, "", false)
						}
					} else {
						app.ResolveChallenge(challenge.ID, false, "", false)
					}
				}
				if name == "job:update" {
					update := value.(JobUpdateModel)
					failureMu.Lock()
					lastUpdate = update
					if strings.Contains(update.Message, " · failed") {
						attemptFailures = append(attemptFailures, update.Message) // Already redacted by the actual queue.
					}
					failureMu.Unlock()
					if update.State == "succeeded" || update.State == "failed" || update.State == "cancelled" {
						terminal <- update
					}
				}
			}
			app.mu.Unlock()
			if err := app.save(); err != nil {
				t.Fatal(err)
			}
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
			id, err := app.QueueTransfer(TransferRequest{DropPreview: preview, Move: mode.pull})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-terminal:
				method, direction := "rsync", "source-push"
				if mode.scp {
					method = "scp"
				}
				if mode.pull {
					direction = "target-pull"
				}
				wantMethod := "direct · " + direction + " · " + method
				if mode.root {
					wantMethod += " · 源端高权 · 目标端高权"
				}
				if result.ID != id || result.State != "succeeded" || result.Method != wantMethod {
					// Only disposable fixture paths: expose the actual received tree
					// when native directory placement/selection differs, rather than
					// misdiagnosing every missing child as an empty-dir filter.
					var received bytes.Buffer
					_ = target.Exec(ctx, "sudo -n find "+shellQuote(targetRoot)+" -maxdepth 3 -printf '%y %m %P\\n'", endpoint.ExecOptions{Stdout: &received})
					t.Logf("received fixture tree: %q", received.String())
					failureMu.Lock()
					failures := append([]string(nil), attemptFailures...)
					failureMu.Unlock()
					for _, failure := range failures {
						t.Logf("queued attempt: %s", failure)
					}
					t.Fatalf("initiator passwordless method failed: state=%s method=%s message=%s", result.State, result.Method, result.Message)
				}
			case <-ctx.Done():
				// This fixture deadline has not cancelled the independent queue.
				// Record one bounded observation before deferred Lock/cleanup.
				deadline, _ := ctx.Deadline()
				t.Logf("timeout boundary: deadline=%s observed=%s", deadline.Format(time.RFC3339Nano), time.Now().Format(time.RFC3339Nano))
				// Never dump raw stacks: only symbol names/line numbers, without
				// argument values, addresses, filesystem paths or credentials.
				records := make([]runtime.StackRecord, 1024)
				count, complete := runtime.GoroutineProfile(records)
				t.Logf("timeout goroutine capture: count=%d complete=%t", count, complete)
				if complete {
					for _, record := range records[:count] {
						frames := runtime.CallersFrames(record.Stack())
						var names []string
						relevant := false
						for {
							frame, more := frames.Next()
							relevant = relevant || strings.Contains(frame.Function, "dragfm-gui/internal/webgui.") || strings.Contains(frame.Function, "dragfm-gui/internal/jobs.")
							names = append(names, fmt.Sprintf("%s:%d", frame.Function, frame.Line))
							if !more {
								break
							}
						}
						if relevant {
							t.Logf("timeout function chain: %s", strings.Join(names, " -> "))
						}
					}
				}
				failureMu.Lock()
				last, failures := lastUpdate, append([]string(nil), attemptFailures...)
				failureMu.Unlock()
				t.Logf("timeout last delivered event: revision=%d state=%s method=%s stage=%s message=%s", last.Revision, last.State, last.Method, last.Stage, last.Message)
				for _, failure := range failures {
					t.Logf("timeout queued attempt: %s", failure)
				}
				for _, state := range app.queue.Snapshot() {
					if state.ID == id {
						// submitFor already redacts every queued update. No remote
						// commands, cancellation or event retries are issued here.
						t.Logf("timeout authoritative snapshot: revision=%d state=%s method=%s stage=%s bytes=%d/%d started=%s finished=%s message=%s", state.Revision, state.State, state.Method, state.Stage, state.BytesDone, state.BytesTotal, state.StartedAt.Format(time.RFC3339Nano), state.FinishedAt.Format(time.RFC3339Nano), state.Message)
					}
				}
				t.Fatal(ctx.Err())
			}
			if mode.root {
				approvalMu.Lock()
				bothApproved := approved["源端提权 · "+source.Name()] && approved["目标端提权 · "+target.Name()]
				approvalMu.Unlock()
				if !bothApproved {
					t.Fatal("root-to-root transfer did not obtain two independent endpoint approvals")
				}
			}
			assertRemoteFile(t, ctx, target, toFile, contents)
			if mode.pull {
				info, err := target.Stat(ctx, target.Join(to, ".empty"))
				if err != nil || !info.Mode.IsDir() || info.Mode.Perm() != 0750 {
					t.Fatalf("native directory transfer did not preserve an empty directory/mode: mode=%v err=%v", info.Mode, err)
				}
				if !mode.scp {
					link, err := target.Readlink(ctx, target.Join(to, "link"))
					if err != nil || link != "leaf ' [空格] $literal" {
						t.Fatal("native rsync did not preserve the symlink target literally")
					}
				}
			}
			if mode.pull {
				if _, err := source.Stat(ctx, from); !errors.Is(err, fs.ErrNotExist) {
					t.Fatal("verified passwordless move retained the source")
				}
			} else {
				assertRemoteFile(t, ctx, source, fromFile, contents)
			}
			assertRemoteFile(t, ctx, source, decoy, decoyContents)
			if _, err := app.List(LeftPane, source.Name(), sourceRoot); err != nil {
				t.Fatal("file browsing failed after passwordless transfer")
			}
			if _, err := app.List(RightPane, target.Name(), targetRoot); err != nil {
				t.Fatal("destination browsing failed after passwordless transfer")
			}
			entries, err := target.List(ctx, targetRoot)
			if err != nil || len(entries) != 1 || entries[0].Name != name {
				t.Fatal("normal transfer retained staging files or failed to publish the payload")
			}
			if mode.system {
				for i, remote := range remotes {
					if i == 1 && !mode.pull {
						continue // Source helper refusal prevents attempting a receiver helper.
					}
					assertRemoteFile(t, ctx, remote, remote.Join(identities[i].home, ".helper-denied"), "denied\n")
				}
				app.mu.RLock()
				data, err := json.Marshal(app.document)
				app.mu.RUnlock()
				if err != nil {
					t.Fatal(err)
				}
				var stored struct {
					Workspaces []any `json:"workspaces"`
				}
				if err := json.Unmarshal(data, &stored); err != nil || len(stored.Workspaces) != 0 {
					t.Fatal("successful system transfer retained workspace recovery records")
				}
			}
		})
	}
}
