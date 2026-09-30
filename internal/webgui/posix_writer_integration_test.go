//go:build integration && !windows

package webgui

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

// All product APIs exist on the pre-fix baseline. Old-red is deleting a live
// receiver's file/tree or losing its cleanup failure, never missing symbols.
func TestHostedPOSIXWriterExitProtocol(t *testing.T) {
	alias := os.Getenv("DRAGFM_E2E_POSIX_SSH")
	if alias == "" {
		t.Fatal("hosted no-SFTP OpenSSH fixture is required")
	}
	for _, mode := range []string{"commit", "abort", "stopped-file", "stopped-directory"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("DRAGFM_E2E_TARGET_SSH", alias)
			_, control, _ := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			route, _ := openSSHRoute(t, alias)
			writerRemote, err := endpoint.DialSSH(ctx, "ci-posix-writer", route.Hops[0].HostKey.PinnedSHA256, route)
			if err != nil {
				t.Fatal(err)
			}
			defer writerRemote.Close()
			if writerRemote.SFTPError() == nil {
				t.Fatal("fixture did not actually reject SFTP")
			}
			parent := remoteTempDir(t, ctx, control)
			target := control.Join(parent, "result")
			payload := bytes.Repeat([]byte("real-posix-stream\n"), 32768)
			if mode == "commit" || mode == "abort" {
				writer, err := writerRemote.CreateAtomic(ctx, target, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := writer.Write(payload); err != nil {
					t.Fatal(err)
				}
				if mode == "commit" {
					err = writer.Commit()
				} else {
					err = writer.Abort()
				}
				if err != nil {
					t.Fatal(err)
				}
				entries, err := control.List(ctx, parent)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "commit" {
					assertRemoteFile(t, ctx, control, target, string(payload))
					if len(entries) != 1 || entries[0].Name != "result" {
						t.Fatal("normal POSIX commit left a partial")
					}
				} else if len(entries) != 0 {
					t.Fatal("normal POSIX abort did not clean its partial")
				}
				if err := control.Remove(ctx, parent, true); err != nil {
					t.Fatal(err)
				}
				return
			}
			input, feed := io.Pipe()
			output, stream := io.Pipe()
			defer input.Close()
			defer feed.Close()
			defer output.Close()
			defer stream.Close()
			observerDone := make(chan error, 1)
			var diagnostic bytes.Buffer
			go func() {
				command := "sudo -n python3 -u -c " + shellQuote(posixWriterObserver) + " " + shellQuote(parent) + " " + shellQuote(mode) + " " + shellQuote(filepath.Base(target))
				err := control.Exec(ctx, command, endpoint.ExecOptions{Stdin: input, Stdout: stream, Stderr: &diagnostic})
				_ = stream.CloseWithError(err)
				observerDone <- err
			}()
			stop := context.AfterFunc(ctx, func() { _ = output.CloseWithError(ctx.Err()); _ = feed.CloseWithError(ctx.Err()) })
			defer stop()
			reader := bufio.NewReader(output)
			if line, err := reader.ReadString('\n'); err != nil || line != "watching\n" {
				t.Fatalf("POSIX write observer did not arm: %q %v", line, err)
			}
			var writer endpoint.AtomicWriter
			var sourceFile string
			completed := make(chan error, 1)
			copyCtx, stopCopy := context.WithCancel(ctx)
			defer stopCopy()
			gate := make(chan struct{})
			var gateOnce sync.Once
			release := func() { gateOnce.Do(func() { close(gate) }) }
			defer release()
			if mode == "stopped-file" {
				writer, err = writerRemote.CreateAtomic(ctx, target, 0600)
				if err != nil {
					t.Fatal(err)
				}
				// Small enough to fit a channel window even if the observer stops
				// cat on its first write; success still requires a real write event.
				if _, err := writer.Write(payload[:4096]); err != nil {
					t.Fatal(err)
				}
			} else {
				sourceDir := t.TempDir()
				sourceFile = filepath.Join(sourceDir, "data")
				if err := os.WriteFile(sourceFile, payload, 0600); err != nil {
					t.Fatal(err)
				}
				go func() {
					_, err := transfer.Run(copyCtx, transfer.Operation{Source: endpoint.NewLocal(), Destination: writerRemote, SourcePath: sourceDir, TargetPath: target, Move: true, Progress: func(progress transfer.Progress) {
						if progress.BytesDone > 0 {
							select {
							case <-gate:
							case <-ctx.Done():
							}
						}
					}})
					completed <- err
				}()
			}
			partial, err := reader.ReadString('\n')
			if err != nil {
				select {
				case observerErr := <-observerDone:
					t.Fatalf("observer did not stop the actual cat writer: %v; observer: %v; %s", err, observerErr, diagnostic.String())
				case <-ctx.Done():
					t.Fatal("observer did not stop the actual cat writer: ", err)
				}
			}
			partial = strings.TrimSuffix(partial, "\n")
			if !strings.HasPrefix(partial, parent+"/") || !strings.HasPrefix(filepath.Base(partial), ".dragfm-partial-") {
				t.Fatal("observer returned an unrelated file")
			}
			began := time.Now()
			var failure error
			if mode == "stopped-file" {
				failure = writer.Abort()
			} else {
				stopCopy()
				release()
				select {
				case failure = <-completed:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if failure == nil || transfer.Retryable(failure) || time.Since(began) > 8*time.Second {
				t.Errorf("unconfirmed POSIX receiver did not stop safely/boundedly: %v", failure)
			}
			if _, err := control.Stat(ctx, partial); err != nil {
				t.Errorf("live cat's partial (or enclosing staged root) was deleted: %v", err)
			}
			if _, err := control.Stat(ctx, target); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("unconfirmed writer published a destination: %v", err)
			}
			if sourceFile != "" {
				actual, err := os.ReadFile(sourceFile)
				if err != nil || sha256.Sum256(actual) != sha256.Sum256(payload) {
					t.Error("failed cross-machine move changed/deleted its source")
				}
			}
			if _, err := io.WriteString(feed, "finish\n"); err != nil {
				t.Fatal(err)
			}
			_ = feed.Close()
			select {
			case err := <-observerDone:
				if err != nil {
					t.Fatal("POSIX fixture did not confirm writer exit", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// Only fixture teardown removes it, after observed exit. Production
			// does not fabricate a lease/ownership marker for an unknown writer.
			if err := control.Remove(ctx, parent, true); err != nil {
				t.Fatal(err)
			}
		})
	}
}

const posixWriterObserver = `import os, sys, signal, select, struct, ctypes, stat
directory = sys.argv[1]
directory_move = sys.argv[2] == 'stopped-directory'
basename = sys.argv[3]
libc = ctypes.CDLL(None, use_errno=True)
watch = libc.inotify_init1(os.O_CLOEXEC)
assert watch >= 0
directories = {}
def add(path):
    wd = libc.inotify_add_watch(watch, path.encode(), 0x100 | 0x2)
    assert wd >= 0
    directories[wd] = path
add(directory)
print('watching', flush=True)
partial = None
while partial is None:
    assert select.select([watch], [], [], 40)[0], 'write event timed out'
    events, offset = os.read(watch, 65536), 0
    while offset < len(events):
        wd, mask, cookie, length = struct.unpack_from('iIII', events, offset)
        name = events[offset+16:offset+16+length].split(b'\0',1)[0].decode()
        offset += 16 + length
        path = directories[wd] + '/' + name
        if '.dragfm-partial-' not in name: continue
        # A directory move first makes short-lived overlap probes in its
        # parent. They are not the receiver whose cancellation this test owns.
        # Observe only the actual staged payload tree, retaining the original
        # live-cat and exit/retention assertions below.
        if directory_move and directories[wd] == directory:
            if not mask & 0x40000000 or not name.startswith(basename + '.dragfm-partial-'):
                continue
        if mask & 0x40000000:
            add(path)
            candidates = [entry.path for entry in os.scandir(path)]
        else:
            candidates = [path]
        for candidate in candidates:
            info = os.lstat(candidate)
            if os.path.basename(candidate).startswith('.dragfm-partial-') and stat.S_ISREG(info.st_mode) and info.st_size > 0:
                partial = candidate
os.close(watch)
writers = []
for item in os.scandir('/proc'):
    if not item.name.isdecimal(): continue
    try:
        if os.path.basename(os.readlink(item.path + '/exe')) == 'cat' and os.readlink(item.path + '/fd/1') == partial:
            writers.append(int(item.name))
    except (FileNotFoundError, PermissionError): pass
assert len(writers) == 1, 'no unique real cat writer for observed file'
fd = os.pidfd_open(writers[0])
def interrupted(signum, frame):
    raise RuntimeError('observer interrupted')
try:
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGHUP, interrupted)
    signal.pidfd_send_signal(fd, signal.SIGSTOP)
    assert '\nState:\tT' in open('/proc/'+str(writers[0])+'/status').read(), 'cat stop not observed'
    print(partial, flush=True)
    assert sys.stdin.readline() == 'finish\n', 'observer released early'
finally:
    try: signal.pidfd_send_signal(fd, signal.SIGKILL)
    except ProcessLookupError: pass
    assert select.select([fd], [], [], 8)[0], 'cat exit unconfirmed'
    os.close(fd)
`
