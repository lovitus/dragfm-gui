//go:build integration && !windows

package webgui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/remoteagent"
)

// Actual queue, OpenSSH sftp-server, sudo, encrypted vault and reconnect RPC.
// Red evidence must remove the system-file journal wiring while keeping the
// existing system fallback, not overlay this onto a baseline missing that API.
func TestHostedSystemPartialJournalLifecycle(t *testing.T) {
	for _, candidate := range []struct {
		mode  string
		posix bool
	}{{mode: "commit"}, {mode: "abort"}, {mode: "connection-loss"}, {mode: "commit", posix: true}, {mode: "abort", posix: true}} {
		name := candidate.mode
		if candidate.posix {
			name += "/posix"
		}
		t.Run(name, func(t *testing.T) {
			mode := candidate.mode
			if candidate.posix {
				fixture := os.Getenv("DRAGFM_E2E_POSIX_SSH")
				if fixture == "" {
					t.Fatal("no-system-SFTP hosted fixture was not provisioned")
				}
				t.Setenv("DRAGFM_E2E_SOURCE_SSH", fixture)
			}
			source, _, document := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			parent := remoteTempDir(t, ctx, source)
			t.Cleanup(func() {
				_ = source.Exec(context.Background(), "sudo -n rm -rf -- "+shellQuote(parent), endpoint.ExecOptions{})
			})
			app := unlockedTestApp(t)
			app.mu.Lock()
			app.document = document.Clone()
			generation := app.generation
			app.mu.Unlock()
			if err := app.save(); err != nil {
				t.Fatal(err)
			}
			type started struct {
				files *remoteagent.SystemFiles
				err   error
			}
			ready, release := make(chan started, 1), make(chan struct{}, 1)
			defer close(release)
			finished := make(chan JobUpdateModel, 1)
			app.mu.Lock()
			app.eventSink = func(name string, value any) {
				if name == "job:update" {
					update := value.(JobUpdateModel)
					if update.ID == "system-journal" && (update.State == "succeeded" || update.State == "failed" || update.State == "cancelled") {
						finished <- update
					}
				}
			}
			app.mu.Unlock()
			if _, err := app.submitFor(generation, jobs.Job{ID: "system-journal", Description: "hosted system file recovery", Run: func(jobCtx context.Context, _ func(jobs.Update)) error {
				files, cleanup, err := app.openSystemFiles(jobCtx, source, "")
				ready <- started{files: files, err: err}
				if err != nil {
					return err
				}
				select {
				case <-release:
					return cleanup()
				case <-jobCtx.Done():
					return errors.Join(jobCtx.Err(), cleanup())
				}
			}}); err != nil {
				t.Fatal(err)
			}
			var active started
			select {
			case active = <-ready:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if active.err != nil {
				t.Fatal(active.err)
			}
			records := readHelperJournal(t, app.vaultPath)
			identity, err := source.Identity(ctx)
			if err != nil || len(records) != 1 || !records[0].Elevated || records[0].HostID != document.Hosts[0].ID || records[0].Fingerprint != identity.Fingerprint || records[0].MachineID != identity.MachineID {
				t.Fatalf("system SFTP missing its actual host/privilege-bound recovery record: %v", err)
			}
			directory := records[0].Path
			target := source.Join(parent, "published.txt")
			if candidate.posix {
				target = source.Join(parent, "quote' and newline\npublished.txt")
			}
			writer, err := active.files.Files.CreateAtomic(ctx, target, 0600)
			if err != nil {
				t.Fatal(err)
			}
			records = readHelperJournal(t, app.vaultPath)
			if len(records) != 1 || len(records[0].Partials) != 1 || records[0].Partials[0].FileID == "" {
				t.Fatal("system SFTP returned a writer before persisting its real inode")
			}
			partial := records[0].Partials[0]
			var actual bytes.Buffer
			if err := source.Exec(ctx, "sudo -n stat -c '%d:%i' -- "+shellQuote(partial.Path), endpoint.ExecOptions{Stdout: &actual}); err != nil || strings.TrimSpace(actual.String()) != partial.FileID {
				t.Fatal("journal pin differs from actual system SFTP inode", err)
			}
			const contents = "real system SFTP uncommitted payload\n"
			if _, err := io.WriteString(writer, contents); err != nil {
				t.Fatal(err)
			}
			if mode == "connection-loss" {
				recoverInterruptedSystemWriter(t, ctx, app, source, active.files, directory, partial.Path, target, contents, release, finished)
				return
			}
			if mode == "commit" {
				err = writer.Commit()
			} else {
				err = writer.Abort()
			}
			if err != nil {
				t.Fatal(err)
			}
			if records := readHelperJournal(t, app.vaultPath); len(records) != 1 || len(records[0].Partials) != 0 {
				t.Fatal("confirmed commit/abort did not retire the partial record")
			}
			release <- struct{}{}
			select {
			case result := <-finished:
				if result.State != "succeeded" {
					t.Fatalf("system cleanup: %s: %s", result.State, result.Message)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if _, err := source.Stat(ctx, directory); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("system lease directory was not removed", err)
			}
			if records := readHelperJournal(t, app.vaultPath); len(records) != 0 {
				t.Fatal("system cleanup left its recovery journal")
			}
			var published bytes.Buffer
			if mode == "commit" {
				if err := source.Exec(ctx, "sudo -n cat -- "+shellQuote(target), endpoint.ExecOptions{Stdout: &published}); err != nil || published.String() != contents {
					t.Fatal("system committed contents changed", err)
				}
			} else if _, err := source.Stat(ctx, target); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("aborted system writer published a target", err)
			}
		})
	}
}

func recoverInterruptedSystemWriter(t *testing.T, ctx context.Context, app *App, source *endpoint.Remote, system *remoteagent.SystemFiles, directory, partial, target, contents string, release chan<- struct{}, finished <-chan JobUpdateModel) {
	t.Helper()
	input, feed := io.Pipe()
	output, stream := io.Pipe()
	defer input.Close()
	defer feed.Close()
	defer output.Close()
	defer stream.Close()
	observerDone := make(chan error, 1)
	go func() {
		err := source.Exec(ctx, "sudo -n python3 -u -c "+shellQuote(systemSFTPExitObserver)+" "+shellQuote(directory), endpoint.ExecOptions{Stdin: input, Stdout: stream})
		_ = stream.CloseWithError(err)
		observerDone <- err
	}()
	stop := context.AfterFunc(ctx, func() { _ = output.CloseWithError(ctx.Err()); _ = feed.CloseWithError(ctx.Err()) })
	defer stop()
	reader := bufio.NewReader(output)
	if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("real system SFTP stop fixture not ready: %q %v", line, err)
	}
	if err := system.Close(); err == nil {
		t.Fatal("stopped system writer was reported as safely cleaned")
	}
	release <- struct{}{}
	select {
	case result := <-finished:
		if result.State != "failed" {
			t.Fatalf("lost system channel ended %s", result.State)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	ageJournaledWorkspace(t, ctx, app, source, directory)
	recovered := make(chan JobUpdateModel, 2)
	app.mu.Lock()
	app.eventSink = func(name string, value any) {
		if name == "challenge" {
			challenge := value.(ChallengeModel)
			app.ResolveChallenge(challenge.ID, true, "", false)
		}
		if name == "job:update" {
			update := value.(JobUpdateModel)
			if strings.HasPrefix(update.Description, "清理过期临时资源 · ") && (update.State == "succeeded" || update.State == "failed" || update.State == "cancelled") {
				recovered <- update
			}
		}
	}
	app.mu.Unlock()
	reconnect := func() {
		if err := app.Lock(); err != nil {
			t.Fatal(err)
		}
		if _, err := app.Unlock("test master password"); err != nil {
			t.Fatal(err)
		}
		if _, err := app.ChangeEndpoint(LeftPane, "ci-source", ""); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-recovered:
			if result.State != "succeeded" {
				t.Fatalf("system recovery: %s: %s", result.State, result.Message)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	reconnect()
	var preserved bytes.Buffer
	if err := source.Exec(ctx, "sudo -n cat -- "+shellQuote(partial), endpoint.ExecOptions{Stdout: &preserved}); err != nil || preserved.String() != contents {
		t.Fatal("live system writer's data was deleted or changed", err)
	}
	if records := readHelperJournal(t, app.vaultPath); len(records) != 1 || records[0].Path != directory {
		t.Fatal("live system writer lost its recovery record")
	}
	if _, err := io.WriteString(feed, "finish\n"); err != nil {
		t.Fatal(err)
	}
	_ = feed.Close()
	if line, err := reader.ReadString('\n'); err != nil || line != "exited\n" {
		t.Fatalf("system writer exit unconfirmed: %q %v", line, err)
	}
	select {
	case err := <-observerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	reconnect()
	for _, value := range []string{directory, partial, target} {
		if _, err := source.Stat(ctx, value); !errors.Is(err, fs.ErrNotExist) {
			t.Fatal("unpublished target or reclaimed system resource still exists", err)
		}
	}
	if records := readHelperJournal(t, app.vaultPath); len(records) != 0 {
		t.Fatal("reclaimed system partial still journaled")
	}
	if _, err := app.List(LeftPane, "ci-source", source.Dir(target)); err != nil {
		t.Fatal("recovery interrupted browsing", err)
	}
}

// Identify ONLY a real sftp-server holding this exact directory inode on fd9.
// No PID file, process-name kill, or mocked server; pidfd is the exit authority.
const systemSFTPExitObserver = `import os, sys, signal, select
directory = sys.argv[1]
identity = os.stat(directory)
children = []
for item in os.scandir('/proc'):
    if not item.name.isdecimal():
        continue
    try:
        if os.path.basename(os.readlink(item.path + '/exe')) != 'sftp-server':
            continue
        held = os.stat(item.path + '/fd/9')
        if (held.st_dev, held.st_ino) == (identity.st_dev, identity.st_ino):
            children.append(int(item.name))
    except (FileNotFoundError, PermissionError):
        pass
assert len(children) == 1, 'expected one real system SFTP lease holder'
fd = os.pidfd_open(children[0])
def exited():
    return bool(select.select([fd], [], [], 8)[0])
def interrupted(signum, frame):
    raise RuntimeError('fixture observer interrupted')
try:
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGHUP, interrupted)
    signal.pidfd_send_signal(fd, signal.SIGSTOP)
    status = open('/proc/' + str(children[0]) + '/status').read()
    assert '\nState:\tT' in status, 'system SFTP stop has not been observed'
    print('ready', flush=True)
    assert sys.stdin.readline() == 'finish\n', 'controller observer ended early'
    signal.pidfd_send_signal(fd, signal.SIGKILL)
    assert exited(), 'system SFTP still alive'
    print('exited', flush=True)
finally:
    try:
        signal.pidfd_send_signal(fd, signal.SIGKILL)
    except ProcessLookupError:
        pass
    assert exited(), 'fixture system SFTP exit unconfirmed'
    os.close(fd)
`
