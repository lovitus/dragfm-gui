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
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/strategy"
	"github.com/lovitus/dragfm-gui/internal/transfer"
)

// A real target-side SCP/rsync writer is stopped after creating its staging
// directory. Kill ONLY that transfer's SSH session process, leaving the writer
// alive. The product must report uncertainty without erasing the live partial
// or moving the source. No substitute Endpoint/transfer implementation is used.
func TestHostedPeerExitLossRetainsLivePartial(t *testing.T) {
	for _, test := range []struct {
		carrier string
		method  strategy.Method
	}{{"helper", strategy.SCP}, {"helper", strategy.Rsync}, {"system", strategy.SCP}, {"system", strategy.Rsync}} {
		t.Run(test.carrier+"/"+string(test.method), func(t *testing.T) {
			if test.carrier == "system" {
				for _, name := range []string{"DRAGFM_E2E_SOURCE_SSH", "DRAGFM_E2E_TARGET_SSH"} {
					value := os.Getenv(name)
					if value == "" {
						t.Skip("disposable SSH fixtures are not configured")
					}
					t.Setenv(name, value+"-system")
				}
			}
			source, target, document := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
			defer cancel()
			sourceRoot, targetRoot := remoteTempDir(t, ctx, source), remoteTempDir(t, ctx, target)
			app := New(filepath.Join(t.TempDir(), "unused.vault"))
			defer app.Lock()
			app.document = document
			operation := transfer.Operation{Source: source, Destination: target, SourcePath: source.Join(sourceRoot, "source"), TargetPath: target.Join(targetRoot, "result"), Move: true}
			if err := source.MkdirAll(ctx, operation.SourcePath, 0700); err != nil {
				t.Fatal(err)
			}
			if err := source.Exec(ctx, "dd if=/dev/urandom of="+shellQuote(source.Join(operation.SourcePath, "data"))+" bs=1M count=32 status=none", endpoint.ExecOptions{}); err != nil {
				t.Fatal(err)
			}
			before, err := transfer.Snapshot(ctx, source, operation.SourcePath, true)
			if err != nil {
				t.Fatal(err)
			}
			report := mustPreflight(t, ctx, operation)
			input, feed := io.Pipe()
			output, stream := io.Pipe()
			defer input.Close()
			defer feed.Close()
			defer output.Close()
			defer stream.Close()
			observerDone := make(chan error, 1)
			var diagnostic bytes.Buffer
			go func() {
				command := "sudo -n python3 -u -c " + shellQuote(peerWriterObserver) + " " + shellQuote(targetRoot) + " result " + string(test.method) + " " + test.carrier
				err := target.Exec(ctx, command, endpoint.ExecOptions{Stdin: input, Stdout: stream, Stderr: &diagnostic})
				_ = stream.CloseWithError(err)
				observerDone <- err
			}()
			stop := context.AfterFunc(ctx, func() { _ = output.CloseWithError(ctx.Err()); _ = feed.CloseWithError(ctx.Err()) })
			defer stop()
			reader := bufio.NewReader(output)
			if line, err := reader.ReadString('\n'); err != nil || line != "watching\n" {
				t.Fatalf("inotify observer did not arm: %q %v", line, err)
			}
			completed := make(chan error, 1)
			go func() {
				if test.carrier == "system" {
					completed <- app.runSystemNative(ctx, operation, strategy.SourcePush, false, nil, nil, test.method)
					return
				}
				completed <- app.runAgentMethod(ctx, operation, report, strategy.SourcePush, false, "", nil, nil, test.method)
			}()
			partial, err := reader.ReadString('\n')
			if err != nil {
				// Exec owns diagnostic until it returns. Join before reading it,
				// including EOF failures, so the original fixture error survives
				// without a concurrent bytes.Buffer read/write.
				select {
				case observerErr := <-observerDone:
					// The observer has already performed its own cleanup. This
					// snapshot precedes only Go cancel, not observer SIGKILL.
					select {
					case failure := <-completed:
						t.Logf("native result after observer cleanup, before Go cancel: %v", failure)
					default:
						t.Log("native result after observer cleanup, before Go cancel: still in flight")
					}
					t.Fatalf("fixture did not observe and stop a real peer writer: %v; observer: %v; %s", err, observerErr, diagnostic.String())
				case <-ctx.Done():
				}
				t.Fatal("fixture did not observe and stop a real peer writer: ", err)
			}
			partial = strings.TrimSuffix(partial, "\n")
			if test.carrier == "system" {
				workspace := path.Base(target.Dir(partial))
				if target.Dir(target.Dir(partial)) != targetRoot || path.Base(partial) != "payload" || len(workspace) != len(".dragfm-")+32 || !strings.HasPrefix(workspace, ".dragfm-") || strings.Trim(workspace[len(".dragfm-"):], "0123456789abcdef") != "" {
					t.Fatal("observer returned an out-of-scope system staging path")
				}
			} else if !strings.HasPrefix(partial, operation.TargetPath+".dragfm-partial-") || strings.ContainsRune(strings.TrimPrefix(partial, operation.TargetPath), '/') {
				t.Fatal("observer returned an out-of-scope helper staging path")
			}
			select {
			case failure := <-completed:
				if failure == nil || transfer.Retryable(failure) {
					t.Errorf("missing exit status must stop retry/move: %v", failure)
				}
			case <-ctx.Done():
				t.Fatal("transfer did not return after the real peer SSH session died")
			}
			if _, err := target.Stat(ctx, partial); err != nil {
				t.Errorf("live receiver staging path was erased: %v", err)
			}
			if _, err := target.Stat(ctx, operation.TargetPath); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("unconfirmed transfer published its target: %v", err)
			}
			after, err := transfer.Snapshot(ctx, source, operation.SourcePath, true)
			if err != nil || transfer.CompareManifests(before, after, false) != nil {
				t.Error("failed move changed or deleted its source")
			}
			for _, remote := range []*endpoint.Remote{source, target} {
				if _, err := remote.List(ctx, "/tmp"); err != nil {
					t.Errorf("transfer failure closed the independent browsing connection: %v", err)
				}
			}
			if _, err := io.WriteString(feed, "finish\n"); err != nil {
				t.Fatal(err)
			}
			_ = feed.Close()
			select {
			case err := <-observerDone:
				if err != nil {
					t.Fatalf("fixture writer cleanup failed: %v: %s", err, diagnostic.String())
				}
				t.Logf("peer lifecycle observation (fixture only): %s", diagnostic.String())
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// Delete fixture files only after pidfds confirmed every stopped
			// writer is dead. Failure paths leave them to container teardown.
			for _, item := range []struct {
				ep   *endpoint.Remote
				root string
			}{{source, sourceRoot}, {target, targetRoot}} {
				if err := item.ep.Remove(ctx, item.root, true); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

const peerWriterObserver = `import os, sys, signal, select, struct, ctypes, json, time, subprocess
directory, basename, tool, carrier = sys.argv[1:]
started, records = time.monotonic(), 0
def event(kind, **values):
    global records
    if records < 200:
        print(json.dumps(dict(event=kind, seconds=round(time.monotonic()-started,6), **values)), file=sys.stderr, flush=True)
    records += 1
def identity(pid):
    try:
        raw = open('/proc/%d/stat' % pid).read()
        values = raw[raw.rfind(')')+2:].split()
        # No argv, paths, credentials or arbitrary process names in evidence.
        name = raw[raw.find('(')+1:raw.rfind(')')]
        if name not in ('rsync','scp','bash','python3','sshd','sshd-session','flock','sudo'): name = 'other'
        return dict(pid=pid, name=name, state=values[0], ppid=int(values[1]), pgid=int(values[2]), sid=int(values[3]), start=int(values[19]), exit_status=int(values[49]))
    except (FileNotFoundError, ProcessLookupError):
        return dict(pid=pid, gone=True)
event('versions', packages=subprocess.check_output(['dpkg-query','-W','-f=${Package}=${Version}\n','bash','rsync'], text=True, timeout=3).splitlines())
libc = ctypes.CDLL(None, use_errno=True)
watch = libc.inotify_init1(os.O_CLOEXEC)
assert watch >= 0
root_watch = libc.inotify_add_watch(watch, directory.encode(), 0x100)
assert root_watch >= 0
workspaces = {}
print('watching', flush=True)
partial = None
while partial is None:
    assert select.select([watch], [], [], 50)[0], 'partial creation event timed out'
    events, offset = os.read(watch, 65536), 0
    while offset < len(events):
        wd, mask, cookie, length = struct.unpack_from('iIII', events, offset)
        name = events[offset+16:offset+16+length].split(b'\0',1)[0].decode()
        offset += 16 + length
        if carrier == 'helper' and wd == root_watch and name.startswith(basename + '.dragfm-partial-'):
            partial = directory + '/' + name
        elif carrier == 'system' and wd == root_watch and mask & 0x40000000 and name.startswith('.dragfm-'):
            nonce = name[len('.dragfm-'):]
            assert len(nonce) == 32 and all(c in '0123456789abcdef' for c in nonce)
            workspace = directory + '/' + name
            child_watch = libc.inotify_add_watch(watch, workspace.encode(), 0x100)
            assert child_watch >= 0, 'could not watch actual native staging workspace'
            workspaces[child_watch] = workspace
        elif carrier == 'system' and wd in workspaces and name == 'payload':
            # The workspace itself is created before the tool starts. Only its
            # payload event is evidence that the real receiving tool is live.
            partial = workspaces[wd] + '/payload'
os.close(watch)
writers = []
for entry in os.scandir('/proc'):
    if not entry.name.isdecimal(): continue
    try:
        exe = os.readlink(entry.path + '/exe')
        argv = open(entry.path + '/cmdline','rb').read().split(b'\0')
        if os.path.basename(exe) == tool and partial.encode() in argv:
            writers.append(int(entry.name))
    except (FileNotFoundError, PermissionError): pass
assert writers, 'no real tool process for observed partial'
event('selected', order=writers)
ancestors = {}
for pid in writers:
    argv = open('/proc/%d/cmdline' % pid,'rb').read().split(b'\0')
    event('candidate', **identity(pid), server=b'--server' in argv, sender=b'--sender' in argv, exact_target=partial.encode() in argv)
    parent = pid
    for _ in range(12):
        info = identity(parent)
        if info.get('gone') or parent <= 1: break
        ancestors[parent] = info
        parent = info['ppid']
        if info['name'] in ('sshd','sshd-session'): break
event('initial_ancestry', processes=list(ancestors.values()))
fds = [os.pidfd_open(pid) for pid in writers]
traced, reaped = set(), set()
def stop_observer(sig, frame):
    raise RuntimeError('observer interrupted')
signal.signal(signal.SIGTERM, stop_observer)
signal.signal(signal.SIGHUP, stop_observer)
signal.signal(signal.SIGALRM, stop_observer)
libc.ptrace.argtypes = [ctypes.c_uint, ctypes.c_int, ctypes.c_void_p, ctypes.c_void_p]
libc.ptrace.restype = ctypes.c_long
def ptrace(pid, request, data=0):
    if libc.ptrace(request, pid, None, ctypes.c_void_p(data)) == -1:
        code = ctypes.get_errno()
        raise OSError(code, os.strerror(code))
try:
    # SIGSTOP delivery is asynchronous. As in the existing helper-loss
    # fixture, observe a kernel group-stop before killing the SSH owner.
    # No status polling or substitute transfer process is used.
    signal.alarm(8)
    for pid, fd in zip(writers, fds):
        ptrace(pid, 0x4206)  # PTRACE_SEIZE, no EXITKILL
        traced.add(pid)
        event('observer_sigstop', pid=pid)
        signal.pidfd_send_signal(fd, signal.SIGSTOP)
        while True:
            _, status = os.waitpid(pid, 0x40000000)  # __WALL, blocking event
            # Preserve the real kernel result, rather than guessing whether
            # this rsync process finished normally or failed before injection.
            exit_code = os.waitstatus_to_exitcode(status) if os.WIFEXITED(status) or os.WIFSIGNALED(status) else None
            event('wait', pid=pid, status=status, ptrace_event=status >> 16, exit_code=exit_code,
                  stopped=os.WIFSTOPPED(status), processes=[identity(p) for p in ancestors])
            if exit_code is not None: reaped.add(pid)
            assert os.WIFSTOPPED(status), 'writer exited before group-stop: wait_status=%s exit_code=%s' % (status, exit_code)
            stopped_signal = os.WSTOPSIG(status)
            if status >> 16 == 128 and stopped_signal == signal.SIGSTOP:
                ptrace(pid, 0x4208)  # PTRACE_LISTEN: remain group-stopped
                event('listen', pid=pid)
                break
            if status >> 16 == 0:
                # Linux siginfo ABI: three ints, then an aligned union. Only
                # decode sender fields for SI_USER/SI_TKILL or SIGCHLD; kernel
                # sources do not identify a userspace sender. No trace change.
                siginfo = ctypes.create_string_buffer(128)
                result = libc.ptrace(0x4202, pid, None, ctypes.cast(siginfo, ctypes.c_void_p))
                if result == -1:
                    event('siginfo_unavailable', pid=pid, errno=ctypes.get_errno())
                else:
                    signo, error, code = struct.unpack_from('=iii', siginfo.raw)
                    details = dict(pid=pid, signal=signo, errno=error, code=code)
                    offset = 16 if ctypes.sizeof(ctypes.c_void_p) == 8 else 12
                    if code in (0, -6) or signo == signal.SIGCHLD:
                        sender, uid = struct.unpack_from('=iI', siginfo.raw, offset)
                        details.update(sender=sender, uid=uid, sender_identity=identity(sender))
                    if signo == signal.SIGCHLD:
                        details['child_status'] = struct.unpack_from('=i', siginfo.raw, offset+8)[0]
                    event('siginfo', **details)
            # Preserve real signal delivery, including SIGCHLD from another
            # rsync worker. The following wait blocks for the resulting stop.
            ptrace(pid, 7, stopped_signal)  # PTRACE_CONT
            event('continue', pid=pid, signal=stopped_signal)
    signal.alarm(0)
    parent = writers[0]
    while parent > 1:
        status = open('/proc/'+str(parent)+'/status').read().splitlines()
        info = dict(line.split(':',1) for line in status if ':' in line)
        if info['Name'].strip() in ('sshd', 'sshd-session'): break
        parent = int(info['PPid'])
    assert parent > 1, 'no owning SSH session process'
    sshfd = os.pidfd_open(parent)
    event('planned_ssh_sigkill', pid=parent)
    signal.pidfd_send_signal(sshfd, signal.SIGKILL)
    assert select.select([sshfd], [], [], 8)[0], 'SSH session did not exit'
    os.close(sshfd)
    print(partial, flush=True)
    assert sys.stdin.readline() == 'finish\n', 'observer released early'
finally:
    signal.alarm(0)
    event('observer_teardown', reaped=sorted(reaped))
    for fd in fds:
        try: signal.pidfd_send_signal(fd, signal.SIGKILL)
        except ProcessLookupError: pass
    for pid, fd in zip(writers, fds):
        assert select.select([fd], [], [], 8)[0], 'writer exit unconfirmed'
        if pid in traced and pid not in reaped:
            _, status = os.waitpid(pid, 0x40000000)
            assert os.WIFEXITED(status) or os.WIFSIGNALED(status), 'traced writer was not reaped'
        os.close(fd)
`
