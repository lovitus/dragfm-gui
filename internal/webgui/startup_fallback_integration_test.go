//go:build integration && !windows

package webgui

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/transfer"
	"golang.org/x/crypto/ssh"
)

// The real queue/preflight/approved file view and streaming copy are exercised
// together. A disposable login policy rejects the uploaded executable. In the
// negative case a real sleep child first acquires its installation flock, so
// cleanup is genuinely refused; no Endpoint or helper result is substituted.
func TestHostedUncertainHelperStartupCannotBecomeSuccessfulFallback(t *testing.T) {
	for _, held := range []bool{false, true} {
		name := "confirmed-rejection"
		if held {
			name = "live-installation"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("DRAGFM_E2E_SOURCE_SSH", os.Getenv("DRAGFM_E2E_SOURCE_SSH")+"-system")
			source, target, document := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			home, err := source.Home(ctx)
			if err != nil {
				t.Fatal(err)
			}
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			app := unlockedTestApp(t)
			t.Cleanup(func() {
				if err := app.Lock(); err != nil {
					t.Error(err)
					return // Retain evidence when stopping queued work is unconfirmed.
				}
				cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
				defer stop()
				// Pin the exact child through its inherited fd before signalling;
				// wait for pidfd exit, then remove only this test's journal paths.
				if held {
					if err := source.Exec(cleanup, "python3 -c "+shellQuote(stopRejectedHelperLease)+" "+shellQuote(home), endpoint.ExecOptions{}); err != nil {
						t.Error(err)
						return // Never remove an installation under an unconfirmed child.
					}
				}
				for _, record := range readHelperJournal(t, app.vaultPath) {
					if err := source.Remove(cleanup, record.Path, true); err != nil && !errors.Is(err, fs.ErrNotExist) {
						t.Error(err)
					}
				}
				for _, file := range []string{".hold-denied-helper", ".held-helper-pid", ".held-helper-path", ".helper-denied"} {
					_ = source.Remove(cleanup, source.Join(home, file), false)
				}
				_ = source.Exec(cleanup, "sudo -n rm -rf -- "+shellQuote(sourceRoot), endpoint.ExecOptions{})
				_ = target.Remove(cleanup, targetRoot, true)
			})
			parent := source.Join(sourceRoot, "protected")
			if err := source.MkdirAll(ctx, parent, 0700); err != nil {
				t.Fatal(err)
			}
			from, to := source.Join(parent, "payload"), target.Join(targetRoot, "payload")
			const contents = "real approved fallback data\n"
			writeRemoteFile(t, ctx, source, from, contents)
			if err := source.Exec(ctx, "sudo -n chown root:root -- "+shellQuote(parent)+" && sudo -n chmod 0700 -- "+shellQuote(parent), endpoint.ExecOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := source.Stat(ctx, from); !errors.Is(err, fs.ErrPermission) {
				t.Fatal("source must actually require approved privilege", err)
			}
			if held {
				writeRemoteFile(t, ctx, source, source.Join(home, ".hold-denied-helper"), "hold\n")
			}
			finished := make(chan JobUpdateModel, 1)
			app.mu.Lock()
			app.document = document.Clone()
			generation := app.generation
			app.eventSink = func(name string, value any) {
				if name == "challenge" {
					challenge := value.(ChallengeModel)
					app.ResolveChallenge(challenge.ID, challenge.Kind == "password", "", false)
				}
				if name == "job:update" {
					update := value.(JobUpdateModel)
					if update.ID == "startup-fallback" && (update.State == "succeeded" || update.State == "failed" || update.State == "cancelled") {
						finished <- update
					}
				}
			}
			app.mu.Unlock()
			if err := app.save(); err != nil {
				t.Fatal(err)
			}
			operation := transfer.Operation{Source: source, Destination: target, SourcePath: from, TargetPath: to}
			_, err = app.submitFor(generation, jobs.Job{ID: "startup-fallback", Description: "approved file access fallback", Run: func(jobCtx context.Context, _ func(jobs.Update)) (result error) {
				jobCtx, access := newTransferAccess(jobCtx, app, operation)
				defer func() { finishAgentCleanup(&result, access.close) }()
				if _, err := access.preflight(jobCtx); err != nil {
					return err
				}
				return access.relay(jobCtx)
			}})
			if err != nil {
				t.Fatal(err)
			}
			var result JobUpdateModel
			select {
			case result = <-finished:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			assertRemoteFile(t, ctx, source, source.Join(home, ".helper-denied"), "denied\n")
			var original bytes.Buffer
			if err := source.Exec(ctx, "sudo -n cat -- "+shellQuote(from), endpoint.ExecOptions{Stdout: &original}); err != nil || original.String() != contents {
				t.Fatal("source changed", err)
			}
			records := readHelperJournal(t, app.vaultPath)
			if held {
				if result.State != "failed" {
					t.Fatalf("uncertain helper startup was hidden by a successful fallback: %s", result.State)
				}
				if _, err := target.Stat(ctx, to); !errors.Is(err, fs.ErrNotExist) {
					t.Fatal("unsafe fallback published destination data", err)
				}
				if len(records) != 1 {
					t.Fatalf("retained installation journal count=%d, want 1", len(records))
				}
				var exited *ssh.ExitError
				err := source.Exec(ctx, "flock -x -n -E 73 "+shellQuote(records[0].Path)+" true", endpoint.ExecOptions{})
				if !errors.As(err, &exited) || exited.ExitStatus() != 73 {
					t.Fatal("fixture did not retain a real live installation lease", err)
				}
			} else {
				if result.State != "succeeded" {
					t.Fatalf("confirmed helper rejection prevented safe fallback: %s: %s", result.State, result.Message)
				}
				assertRemoteFile(t, ctx, target, to, contents)
				if len(records) != 0 {
					t.Fatal("confirmed cleanup left its journal")
				}
			}
			if _, err := source.List(ctx, sourceRoot); err != nil {
				t.Fatal("startup damaged browser connection", err)
			}
		})
	}
}

const stopRejectedHelperLease = `import os, pathlib, select, signal, sys
home = pathlib.Path(sys.argv[1])
pidfile = home / '.held-helper-pid'
if not pidfile.exists():
    sys.exit(0)
pid = int(pidfile.read_text())
expected = (home / '.held-helper-path').read_text().strip()
fd = os.pidfd_open(pid)
try:
    if os.readlink('/proc/%d/fd/9' % pid) != expected:
        raise RuntimeError('fixture child no longer owns the recorded lease')
    signal.pidfd_send_signal(fd, signal.SIGTERM)
    if not select.select([fd], [], [], 5)[0]:
        raise RuntimeError('fixture child did not exit')
finally:
    os.close(fd)
`
