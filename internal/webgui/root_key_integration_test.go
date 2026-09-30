//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/configtext"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/vault"
	"golang.org/x/crypto/ssh"
)

// Real OpenSSH root authentication uses a key different from the login key.
// Empty/wrong sudo input cannot rescue this flow: both ordinary accounts require
// a password. The public config/drop/queue path must carry the encrypted vault
// key through Pending, an intervening key edit, privilege approval and transfer.
// This test uses baseline APIs, not a new root-route symbol as its red gate.
func TestHostedExplicitRootVaultKeyQueuedTransfer(t *testing.T) {
	rootKeyPath := os.Getenv("DRAGFM_E2E_ROOT_KEY")
	if rootKeyPath == "" {
		t.Skip("disposable root-key fixture not configured")
	}
	rootPEM, err := os.ReadFile(rootKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	private, err := ssh.ParseRawPrivateKey(rootPEM)
	if err != nil {
		t.Fatal(err)
	}
	phrase := rand.Text()
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "disposable root identity", []byte(phrase))
	if err != nil {
		t.Fatal(err)
	}
	encrypted := string(pem.EncodeToMemory(block))
	_, replacement, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherBlock, err := ssh.MarshalPrivateKey(replacement, "not authorized on fixture")
	if err != nil {
		t.Fatal(err)
	}
	replacementPEM := string(pem.EncodeToMemory(otherBlock))
	t.Setenv("SSH_AUTH_SOCK", "")
	for _, systemOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("system-only=%t", systemOnly), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			controlSource, controlTarget, _ := fixtureEndpoints(t)
			for _, name := range []string{"DRAGFM_E2E_SOURCE_SSH", "DRAGFM_E2E_TARGET_SSH"} {
				t.Setenv(name, os.Getenv(name)+"-password")
			}
			source, target, document := fixtureEndpoints(t)
			for _, remote := range []*endpoint.Remote{source, target} {
				if remote.Exec(ctx, "sudo -n true", endpoint.ExecOptions{}) == nil {
					t.Fatal("fixture permits passwordless sudo")
				}
			}
			// Demonstrate actual account isolation before exercising the GUI.
			ordinary, _ := openSSHRoute(t, os.Getenv("DRAGFM_E2E_SOURCE_SSH"))
			ordinary.Hops[len(ordinary.Hops)-1].User = "root"
			if chain, err := connector.Dial(ctx, ordinary); err == nil {
				_ = chain.Close()
				t.Fatal("root fixture incorrectly accepts the ordinary account's key")
			}
			if systemOnly {
				for _, control := range []*endpoint.Remote{controlSource, controlTarget} {
					if err := control.Exec(ctx, "sudo -n touch /root/.deny-helper", endpoint.ExecOptions{}); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
						defer stop()
						_ = control.Exec(cleanup, "sudo -n rm -f -- /root/.deny-helper /root/.helper-denied", endpoint.ExecOptions{})
					})
				}
			}
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			for _, pair := range []struct {
				remote *endpoint.Remote
				path   string
			}{{controlSource, sourceRoot}, {controlTarget, targetRoot}} {
				t.Cleanup(func() {
					cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
					defer stop()
					_ = pair.remote.Exec(cleanup, "sudo -n rm -rf -- "+shellQuote(pair.path), endpoint.ExecOptions{})
				})
			}
			from, to := source.Join(sourceRoot, "payload"), target.Join(targetRoot, "payload")
			const content = "explicit root key traversed the real queue\n"
			writeRemoteFile(t, ctx, source, from, content)
			writeRemoteFile(t, ctx, source, source.Join(sourceRoot, "next"), "must remain on source")
			for i := range document.Hosts {
				host := &document.Hosts[i]
				host.RouteSpec = fmt.Sprintf(`%s@%s:%d --keys "%s"`, host.User, host.Address, host.Port, host.KeyIDs[0])
				host.HopFingerprints = []string{host.HostFingerprint}
			}
			document.Keys = append(document.Keys, config.PrivateKey{ID: "root-key", Name: "root-key", PEM: encrypted, Passphrase: phrase})
			markdown := configtext.Markdown(document)
			for _, host := range document.Hosts {
				// Leave root用户 absent to cover the documented default root.
				markdown = strings.Replace(markdown, host.RouteSpec+"\n", host.RouteSpec+"\n###root私钥\nroot-key\n", 1)
			}
			app := unlockedTestApp(t)
			app.mu.Lock()
			app.document = document.Clone()
			app.mu.Unlock()
			if _, err := app.SaveConfigTexts(markdown); err != nil {
				t.Fatal(err)
			}
			if _, err := app.ChangeEndpoint(LeftPane, source.Name(), "本机"); err != nil {
				t.Fatal(err)
			}
			if _, err := app.ChangeEndpoint(RightPane, target.Name(), "本机"); err != nil {
				t.Fatal(err)
			}
			for _, pair := range []struct {
				remote *endpoint.Remote
				path   string
			}{{controlSource, sourceRoot}, {controlTarget, targetRoot}} {
				if err := pair.remote.Exec(ctx, "sudo -n chown root:root -- "+shellQuote(pair.path)+" && sudo -n chmod 0700 -- "+shellQuote(pair.path), endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			gate, started := make(chan struct{}), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(gate) }) }
			defer release()
			if _, err := app.queue.Submit(jobs.Job{ID: "root-key-barrier", Run: func(ctx context.Context, _ func(jobs.Update)) error {
				close(started)
				select {
				case <-gate:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			terminal := make(chan JobUpdateModel, 4)
			offered := rand.Text() // deliberately not the fixture's sudo password
			app.mu.Lock()
			app.eventSink = func(name string, value any) {
				if name == "challenge" {
					challenge := value.(ChallengeModel)
					if challenge.Kind == "password" {
						app.ResolveChallenge(challenge.ID, true, offered, true)
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
					if update.ID != "root-key-barrier" && (update.State == "succeeded" || update.State == "failed" || update.State == "cancelled") {
						terminal <- update
					}
				}
			}
			app.mu.Unlock()
			preview, err := app.PrepareDrop(LeftPane, from, RightPane, targetRoot)
			if err != nil {
				t.Fatal(err)
			}
			id, err := app.QueueTransfer(TransferRequest{DropPreview: preview, Move: systemOnly, Overwrite: true})
			if err != nil {
				t.Fatal(err)
			}
			pending := false
			for _, job := range app.queue.Snapshot() {
				if job.ID == id && string(job.State) == "pending" {
					pending = true
				}
			}
			if !pending {
				t.Fatal("transfer did not actually wait in Pending before key edit")
			}
			// Replace the selected key and remove its phrase through the real save
			// transaction; only the already-admitted transfer may retain the old key.
			edited := strings.Replace(markdown, encrypted, replacementPEM, 1)
			edited = strings.Replace(edited, "###口令\n"+phrase+"\n###私钥\n", "", 1)
			if _, err := app.SaveConfigTexts(edited); err != nil {
				t.Fatal(err)
			}
			release()
			select {
			case result := <-terminal:
				if result.ID != id || result.State != "succeeded" {
					t.Fatalf("root key transfer: %s: %s", result.State, result.Message)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var data bytes.Buffer
			if err := controlTarget.Exec(ctx, "sudo -n cat -- "+shellQuote(to), endpoint.ExecOptions{Stdout: &data}); err != nil || data.String() != content {
				t.Fatal("root-key transfer did not publish exact content")
			}
			check := "sudo -n test -f "
			if systemOnly {
				check = "sudo -n test ! -e "
			}
			if err := controlSource.Exec(ctx, check+shellQuote(from), endpoint.ExecOptions{}); err != nil {
				t.Fatal("copy/move source gate violated")
			}
			if systemOnly {
				for _, control := range []*endpoint.Remote{controlSource, controlTarget} {
					if err := control.Exec(ctx, "sudo -n test -f /root/.helper-denied", endpoint.ExecOptions{}); err != nil {
						t.Fatal("system path did not encounter real helper execution denial")
					}
				}
			}
			// A fresh task must now fail with the unauthorised replacement key;
			// neither an old snapshot nor unused sudo input may make it succeed.
			if _, err := app.ChangeEndpoint(LeftPane, source.Name(), "本机"); err != nil {
				t.Fatal(err)
			}
			if _, err := app.ChangeEndpoint(RightPane, target.Name(), "本机"); err != nil {
				t.Fatal(err)
			}
			next, err := app.PrepareDrop(LeftPane, source.Join(sourceRoot, "next"), RightPane, targetRoot)
			if err != nil {
				t.Fatal(err)
			}
			nextID, err := app.QueueTransfer(TransferRequest{DropPreview: next, Move: true, Overwrite: true})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-terminal:
				if result.ID != nextID || result.State != "failed" {
					t.Fatal("new transfer borrowed the old root credential or succeeded with wrong sudo")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err := controlSource.Exec(ctx, "sudo -n test -f "+shellQuote(source.Join(sourceRoot, "next")), endpoint.ExecOptions{}); err != nil {
				t.Fatal("failed root auth deleted source")
			}
			if err := controlTarget.Exec(ctx, "sudo -n test ! -e "+shellQuote(target.Join(targetRoot, "next")), endpoint.ExecOptions{}); err != nil {
				t.Fatal("failed root auth published destination")
			}
			for _, remote := range []*endpoint.Remote{source, target} {
				if _, err := remote.Home(ctx); err != nil {
					t.Fatal("task closed the original browsing connection")
				}
			}
			// Terminal events precede the debounced disk save. Flush through the
			// existing save transaction; do not poll or accept an empty history.
			if err := app.save(); err != nil {
				t.Fatal(err)
			}
			_, persisted, err := vault.Open(app.vaultPath, []byte("test master password"))
			if err != nil {
				t.Fatal(err)
			}
			for _, host := range persisted.Hosts {
				if host.SudoPassword != "" {
					t.Fatal("unused or wrong sudo password was persisted")
				}
			}
			states := make(map[string]bool)
			for _, entry := range persisted.History {
				if entry.ID == id && entry.Success || entry.ID == nextID && !entry.Success {
					states[entry.ID] = true
				}
			}
			if !states[id] || !states[nextID] {
				t.Fatal("both completed root-key tasks must exist in persisted history")
			}
			wire, err := json.Marshal(struct {
				Jobs    any
				History any
			}{app.queue.Snapshot(), persisted.History})
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{phrase, offered, strings.Split(encrypted, "\n")[1], strings.Split(replacementPEM, "\n")[1]} {
				if strings.Contains(string(wire), secret) {
					t.Fatal("root key material leaked into task state/history")
				}
			}
		})
	}
}
