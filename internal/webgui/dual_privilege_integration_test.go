//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/vault"
)

// Exercises the real queue/admission/preview path with two separate non-root
// SSH logins, two root-owned trees and genuine sudo. The old preview/preflight
// rejects the hidden source before a user can approve it. No mock endpoint or
// direct invocation of a transfer method can make this end-to-end test pass.
func TestHostedDualProtectedQueuedTransfer(t *testing.T) {
	if os.Getenv("DRAGFM_E2E_SUDO") == "" {
		t.Skip("non-root sudo fixture not enabled")
	}
	for _, candidate := range []struct{ move, systemOnly, password, wrong, ncat, reverse, cancel, posix bool }{
		{move: false}, {move: true}, {systemOnly: true}, {move: true, systemOnly: true},
		{move: true, password: true}, {move: true, systemOnly: true, password: true}, {move: true, systemOnly: true, password: true, wrong: true},
		{systemOnly: true, password: true, ncat: true}, {move: true, systemOnly: true, password: true, ncat: true, reverse: true},
		{move: true, systemOnly: true, password: true, ncat: true, cancel: true},
		{systemOnly: true, posix: true}, {move: true, systemOnly: true, posix: true},
		{move: true, systemOnly: true, password: true, posix: true},
		{move: true, systemOnly: true, password: true, wrong: true, posix: true},
	} {
		t.Run(fmt.Sprintf("move=%t/system-only=%t/password=%t/wrong=%t/ncat=%t/reverse=%t/cancel=%t/posix=%t", candidate.move, candidate.systemOnly, candidate.password, candidate.wrong, candidate.ncat, candidate.reverse, candidate.cancel, candidate.posix), func(t *testing.T) {
			move := candidate.move
			if candidate.posix {
				for key, fixture := range map[string]string{"DRAGFM_E2E_SOURCE_SSH": "DRAGFM_E2E_POSIX_SSH", "DRAGFM_E2E_TARGET_SSH": "DRAGFM_E2E_POSIX_TARGET_SSH"} {
					value := os.Getenv(fixture)
					if value == "" {
						t.Fatal("no-system-SFTP hosted fixture was not provisioned")
					}
					t.Setenv(key, value)
				}
			}
			controlSource, controlTarget, _ := fixtureEndpoints(t)
			if candidate.posix {
				for _, host := range []*endpoint.Remote{controlSource, controlTarget} {
					if host.SFTPError() == nil {
						t.Fatal("POSIX fixture unexpectedly accepts the SFTP subsystem")
					}
					probe, stop := context.WithTimeout(context.Background(), 10*time.Second)
					err := host.Exec(probe, "for p in /usr/lib/openssh/sftp-server /usr/libexec/openssh/sftp-server /usr/lib/ssh/sftp-server /usr/libexec/sftp-server; do test ! -x \"$p\" || exit 1; done", endpoint.ExecOptions{})
					stop()
					if err != nil {
						t.Fatalf("fixture does not prove the OS SFTP executable is absent: %v", err)
					}
				}
			}
			suffix := ""
			if candidate.systemOnly {
				suffix = "-system"
			}
			offered := "unused-fixture-only-password"
			if candidate.password {
				suffix = "-password"
				offered = os.Getenv("DRAGFM_E2E_SUDO_PASSWORD")
				if offered == "" {
					t.Fatal("password-required hosted fixture was not provisioned")
				}
				if candidate.wrong {
					offered += "-incorrect"
				}
			}
			if suffix != "" {
				for _, key := range []string{"DRAGFM_E2E_SOURCE_SSH", "DRAGFM_E2E_TARGET_SSH"} {
					if value := os.Getenv(key); value != "" {
						t.Setenv(key, value+suffix)
					}
				}
			}
			source, target, document := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			var rejectedPushes func() int
			if candidate.reverse {
				// Keep the actual source->target data firewall closed. Only a
				// target-initiated pull can succeed; control SSH is unaffected.
				rejectedPushes = rejectNcatDataConnections(t, ctx, controlSource, controlTarget, true)
			}
			for _, host := range []*endpoint.Remote{source, target} {
				if candidate.password && host.Exec(ctx, "sudo -n true", endpoint.ExecOptions{}) == nil {
					t.Fatal("password-required fixture unexpectedly accepted sudo -n")
				}
				home, err := host.Home(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if candidate.systemOnly {
					_ = host.Remove(ctx, host.Join(home, ".helper-denied"), false)
				}
				if candidate.password && candidate.systemOnly {
					flag := host.Join(home, ".deny-helper")
					writeRemoteFile(t, ctx, host, flag, "disposable execution policy\n")
					t.Cleanup(func() { _ = host.Remove(context.Background(), flag, false) })
				}
			}
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			t.Cleanup(func() {
				_ = controlSource.Exec(context.Background(), "sudo -n rm -rf -- "+shellQuote(sourceRoot), endpoint.ExecOptions{})
				_ = controlTarget.Exec(context.Background(), "sudo -n rm -rf -- "+shellQuote(targetRoot), endpoint.ExecOptions{})
			})
			sourceParent := source.Join(sourceRoot, "protected")
			sourcePath := source.Join(sourceParent, "payload")
			targetParent := target.Join(targetRoot, "protected")
			targetPath := target.Join(targetParent, "payload")
			for _, pair := range []struct {
				ep   *endpoint.Remote
				path string
			}{{source, sourcePath}, {target, targetPath}} {
				if candidate.ncat && pair.ep == target {
					pair.path = targetParent // independent ncat stages a NEW root
				}
				if err := pair.ep.MkdirAll(ctx, pair.path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			contents := "real dual privilege queue contents\n"
			if candidate.posix {
				// A binary body larger than SSH's window exercises streaming,
				// including frame-like lines; neither sudo input nor protocol
				// readiness bytes may become part of the destination content.
				contents = strings.Repeat("\x00\xff\r\ndragfm-file-not-a-control-frame\n"+contents, 65536)
			}
			writeRemoteFile(t, ctx, source, source.Join(sourcePath, "new.txt"), contents)
			blob := source.Join(sourcePath, "cancel.bin")
			var originalBlobHash bytes.Buffer
			if candidate.cancel {
				if err := source.Exec(ctx, "head -c 67108864 /dev/urandom > "+shellQuote(blob)+" && sha256sum < "+shellQuote(blob), endpoint.ExecOptions{Stdout: &originalBlobHash}); err != nil {
					t.Fatal(err)
				}
			}
			if !candidate.ncat {
				writeRemoteFile(t, ctx, target, target.Join(targetPath, "old.txt"), "keep existing merge entry\n")
			}
			if err := source.Symlink(ctx, "new.txt", source.Join(sourcePath, "relative-link")); err != nil {
				t.Fatal(err)
			}
			app := unlockedTestApp(t)
			app.mu.Lock()
			app.document.Keys, app.document.Hosts = document.Keys, document.Hosts
			app.mu.Unlock()
			if err := app.save(); err != nil {
				t.Fatal(err)
			}
			if _, err := app.ChangeEndpoint(LeftPane, "ci-source", ""); err != nil {
				t.Fatal(err)
			}
			if _, err := app.ChangeEndpoint(RightPane, "ci-target", ""); err != nil {
				t.Fatal(err)
			}
			if _, err := app.List(LeftPane, "ci-source", sourceParent); err != nil {
				t.Fatal(err)
			}
			// Permission changes after listing are real: stale visible entries may
			// still be dragged, but their metadata must be rechecked by the job.
			for _, pair := range []struct {
				ep   *endpoint.Remote
				path string
			}{{controlSource, sourceParent}, {controlTarget, targetParent}} {
				if err := pair.ep.Exec(ctx, "sudo -n chown -R root:root -- "+shellQuote(pair.path)+" && sudo -n chmod 0700 -- "+shellQuote(pair.path), endpoint.ExecOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			if err := controlSource.Exec(ctx, "sudo -n chown -h 12345:12346 -- "+shellQuote(source.Join(sourcePath, "new.txt"))+" "+shellQuote(source.Join(sourcePath, "relative-link")), endpoint.ExecOptions{}); err != nil {
				t.Fatal(err)
			}
			preview, err := app.PrepareDrop(LeftPane, sourcePath, RightPane, targetParent)
			if err != nil {
				t.Fatalf("protected source was rejected before task approval: %v", err)
			}
			if !preview.Conflict || preview.TargetPath != targetPath {
				t.Fatal("inaccessible target must require explicit overwrite confirmation")
			}
			terminal := make(chan JobUpdateModel, 8)
			var eventsMu sync.Mutex
			passwordPrompts, skipped := 0, 0
			usedRelay := false
			var cancelOnce sync.Once
			var cancelledAt time.Time
			app.mu.Lock()
			app.eventSink = func(name string, value any) {
				if name == "challenge" {
					challenge := value.(ChallengeModel)
					if challenge.Kind == "password" {
						eventsMu.Lock()
						passwordPrompts++
						eventsMu.Unlock()
						// NOPASSWD must neither contaminate the helper's stdin nor
						// count as validating this offered-but-unused password.
						app.ResolveChallenge(challenge.ID, true, offered, true)
					} else if challenge.Kind == "confirm" {
						if candidate.ncat {
							app.ResolveChallenge(challenge.ID, true, "", false)
							return
						}
						eventsMu.Lock()
						skipped++
						eventsMu.Unlock()
						if skippable, ok := any(app).(interface{ SkipChallenge(string) }); ok {
							skippable.SkipChallenge(challenge.ID)
						} else {
							// Baseline-compatible test overlay: the old UI only had
							// Cancel; no compile failure is used as the red evidence.
							app.ResolveChallenge(challenge.ID, false, "", false)
						}
					} else {
						app.ResolveChallenge(challenge.ID, false, "", false)
					}
				}
				if name == "job:update" {
					update := value.(JobUpdateModel)
					if candidate.cancel && update.Stage == "ncat-connected" {
						// Machine phase, not translated prose or a sleep: cancel
						// through the same RPC the GUI uses after the real handshake.
						cancelOnce.Do(func() {
							eventsMu.Lock()
							cancelledAt = time.Now()
							eventsMu.Unlock()
							app.CancelJob(update.ID)
						})
					}
					if strings.Contains(update.Method, "controller-memory-relay") {
						eventsMu.Lock()
						usedRelay = true
						eventsMu.Unlock()
					}
					if update.State == "succeeded" || update.State == "failed" || update.State == "cancelled" {
						terminal <- update
					}
				}
			}
			app.mu.Unlock()
			id, err := app.QueueTransfer(TransferRequest{DropPreview: preview, Move: move, Overwrite: true})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case result := <-terminal:
				expected := "succeeded"
				if candidate.wrong {
					expected = "failed"
				} else if candidate.cancel {
					expected = "cancelled"
				}
				if result.ID != id || result.State != expected {
					t.Fatalf("queued transfer: %s: %s", result.State, result.Message)
				}
				if candidate.cancel {
					eventsMu.Lock()
					promptCancellation := !cancelledAt.IsZero() && time.Since(cancelledAt) < 8*time.Second
					eventsMu.Unlock()
					if !promptCancellation {
						t.Fatal("privileged queue cancellation did not finish promptly after the real data connection")
					}
				}
				if candidate.ncat {
					direction := "source-push"
					if candidate.reverse {
						direction = "target-pull"
					}
					if !strings.Contains(result.Method, "ncat-tar") || !strings.Contains(result.Method, direction) {
						t.Fatalf("expected independent privileged %s ncat, got %s", direction, result.Method)
					}
					if rejectedPushes != nil && rejectedPushes() == 0 {
						t.Fatal("reverse path was not forced by the real data firewall")
					}
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if candidate.wrong {
				var preserved bytes.Buffer
				if err := controlSource.Exec(ctx, "sudo -n cat -- "+shellQuote(source.Join(sourcePath, "new.txt")), endpoint.ExecOptions{Stdout: &preserved}); err != nil {
					t.Fatal(err)
				}
				if sha256.Sum256(preserved.Bytes()) != sha256.Sum256([]byte(contents)) {
					t.Fatal("failed sudo changed source content")
				}
				if err := controlTarget.Exec(ctx, "sudo -n test ! -e "+shellQuote(target.Join(targetPath, "new.txt")), endpoint.ExecOptions{}); err != nil {
					t.Fatal("failed sudo wrote destination content")
				}
				_, saved, err := vault.Open(app.vaultPath, []byte("test master password"))
				if err != nil {
					t.Fatal(err)
				}
				for _, host := range saved.Hosts {
					if host.SudoPassword != "" {
						t.Fatal("rejected sudo credential was persisted")
					}
				}
				encoded, err := json.Marshal(app.queue.Snapshot())
				if err != nil || strings.Contains(string(encoded), offered) {
					t.Fatal("failed sudo credential leaked into authoritative job state")
				}
				return
			}
			eventsMu.Lock()
			promptsOK := passwordPrompts == 2 && skipped >= 2 && usedRelay
			if candidate.ncat {
				promptsOK = passwordPrompts == 2 && !usedRelay
			}
			eventsMu.Unlock()
			if !promptsOK {
				t.Fatal("did not independently approve both endpoints and finish with the requested transport")
			}
			if candidate.systemOnly {
				for _, host := range []*endpoint.Remote{source, target} {
					home, err := host.Home(ctx)
					if err != nil {
						t.Fatal(err)
					}
					assertRemoteFile(t, ctx, host, host.Join(home, ".helper-denied"), "denied\n")
				}
			}
			var data bytes.Buffer
			checkEndpoint, checkRoot := controlTarget, targetPath
			if candidate.cancel {
				checkEndpoint, checkRoot = controlSource, sourcePath
				if err := controlTarget.Exec(ctx, "sudo -n test ! -e "+shellQuote(targetPath), endpoint.ExecOptions{}); err != nil {
					t.Fatal("cancelled transfer committed an unverified target")
				}
				var preserved bytes.Buffer
				if err := controlSource.Exec(ctx, "sudo -n /bin/sh -c "+shellQuote("sha256sum < "+shellQuote(blob)), endpoint.ExecOptions{Stdout: &preserved}); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(originalBlobHash.Bytes(), preserved.Bytes()) {
					t.Fatal("cancelled high-privilege move changed source SHA-256")
				}
			}
			if err := checkEndpoint.Exec(ctx, "sudo -n cat -- "+shellQuote(checkEndpoint.Join(checkRoot, "new.txt")), endpoint.ExecOptions{Stdout: &data}); err != nil {
				t.Fatal(err)
			}
			if sha256.Sum256(data.Bytes()) != sha256.Sum256([]byte(contents)) {
				t.Fatal("destination SHA-256 mismatch")
			}
			for _, name := range []string{"old.txt", "relative-link"} {
				if candidate.ncat && name == "old.txt" {
					continue
				}
				if err := checkEndpoint.Exec(ctx, "sudo -n test -e "+shellQuote(checkEndpoint.Join(checkRoot, name)), endpoint.ExecOptions{}); err != nil {
					t.Fatal("directory merge lost existing content or relative link")
				}
			}
			for _, name := range []string{"new.txt", "relative-link"} {
				var owner bytes.Buffer
				if err := checkEndpoint.Exec(ctx, "sudo -n stat -c '%u:%g' -- "+shellQuote(checkEndpoint.Join(checkRoot, name)), endpoint.ExecOptions{Stdout: &owner}); err != nil {
					t.Fatal(err)
				}
				if strings.TrimSpace(owner.String()) != "12345:12346" {
					t.Fatal("elevated transfer did not preserve file/link ownership")
				}
			}
			if candidate.ncat {
				for _, host := range []*endpoint.Remote{controlSource, controlTarget} {
					var leftovers bytes.Buffer
					command := "sudo -n find /tmp -maxdepth 1 -user root -name '.dragfm-*' -print"
					if host == controlTarget {
						command += "; sudo -n find " + shellQuote(targetRoot) + " -name '.dragfm-*' -print"
					}
					if err := host.Exec(ctx, command, endpoint.ExecOptions{Stdout: &leftovers}); err != nil || leftovers.Len() != 0 {
						t.Fatal("privileged transfer left an owned archive/workspace")
					}
					var listeners bytes.Buffer
					if err := host.Exec(ctx, "sudo -n ss -H -ltnp", endpoint.ExecOptions{Stdout: &listeners}); err != nil || strings.Contains(listeners.String(), `"ncat"`) {
						t.Fatal("privileged transfer left a listening ncat")
					}
					var processes bytes.Buffer
					if err := host.Exec(ctx, "ps -eo uid=,stat=,comm=", endpoint.ExecOptions{Stdout: &processes}); err != nil {
						t.Fatal(err)
					}
					for _, line := range strings.Split(processes.String(), "\n") {
						fields := strings.Fields(line)
						if len(fields) == 3 && fields[0] == "0" && fields[2] == "ncat" && !strings.HasPrefix(fields[1], "Z") {
							t.Fatal("privileged transfer left a live root ncat process")
						}
					}
				}
			}
			exists := controlSource.Exec(ctx, "sudo -n test -d "+shellQuote(sourcePath), endpoint.ExecOptions{}) == nil
			if exists == (move && !candidate.cancel) {
				t.Fatal("copy/move source retention is wrong")
			}
			for _, paneID := range []PaneID{LeftPane, RightPane} {
				pane, err := app.pane(paneID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := pane.endpoint.Home(ctx); err != nil {
					t.Fatal("task cleanup closed the browsing SSH connection")
				}
			}
			_, saved, err := vault.Open(app.vaultPath, []byte("test master password"))
			if err != nil {
				t.Fatal(err)
			}
			for _, host := range saved.Hosts {
				if candidate.password && host.SudoPassword != offered {
					t.Fatal("successfully authenticated sudo password was not saved to its approved host")
				}
				if !candidate.password && host.SudoPassword != "" {
					t.Fatal("NOPASSWD saved an unverified password")
				}
			}
			if candidate.posix {
				// Read the serialized vault through a baseline-compatible API.
				// A normal transfer must leave no partial recovery records.
				encoded, err := json.Marshal(saved)
				var stored struct {
					Workspaces []json.RawMessage `json:"workspaces"`
				}
				if err != nil || json.Unmarshal(encoded, &stored) != nil || len(stored.Workspaces) != 0 {
					t.Fatal("normal POSIX transfer did not retire its workspace journal")
				}
				var leftovers bytes.Buffer
				if err := controlTarget.Exec(ctx, "sudo -n find "+shellQuote(targetRoot)+" -name '.dragfm-partial-*' -print", endpoint.ExecOptions{Stdout: &leftovers}); err != nil || leftovers.Len() != 0 {
					t.Fatal("normal POSIX transfer retained a partial in the destination tree")
				}
			}
			for _, job := range app.queue.Snapshot() {
				encoded, err := json.Marshal(job)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(encoded), offered) {
					t.Fatal("offered sudo credential leaked into task output")
				}
			}
			t.Log("real queue: protected source preview, separate source/target approval, skipped network risks, bounded relay, merge, SHA move gate and live browsing connections")
		})
	}
}
