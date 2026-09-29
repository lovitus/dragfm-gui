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
	"github.com/lovitus/dragfm-gui/internal/remoteagent"
	"github.com/pkg/sftp"
)

// Exercise actual uploaded helpers, independent SFTP and OFFICIAL Hans.
// pidfds pin process identities; select waits for actual exit, without polling
// PID/name lists. Stopping Hans makes the orphan window deterministic.
// Uses baseline public APIs: old-red is missing cleanup protection, not an
// absent lease/Fork/OpenFiles symbol or a fake replacement Hans executable.
func TestHostedHelperControlLossPreservesLiveDependents(t *testing.T) {
	for _, candidate := range []struct {
		name, kind string
		crash      bool
	}{
		{name: "control-disconnect", kind: "hans"},
		{name: "killed-helper-stopped-hans", kind: "hans", crash: true},
		{name: "killed-helper-stopped-sftp", kind: "sftp", crash: true},
	} {
		t.Run(candidate.name, func(t *testing.T) {
			crash := candidate.crash
			source, _, _ := fixtureEndpoints(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			route, _ := openSSHRoute(t, os.Getenv("DRAGFM_E2E_SOURCE_SSH"))
			owned, err := endpoint.DialSSH(ctx, "ci-helper-lifecycle", route.Hops[0].HostKey.PinnedSHA256, route)
			if err != nil {
				t.Fatal(err)
			}
			defer owned.Close()
			var architecture bytes.Buffer
			if err := source.Exec(ctx, "uname -m", endpoint.ExecOptions{Stdout: &architecture}); err != nil {
				t.Fatal(err)
			}
			helper, err := remoteagent.StartElevated(ctx, owned, strings.TrimSpace(architecture.String()), "")
			if err != nil {
				t.Fatal(err)
			}
			defer helper.Close()
			binary := helper.Directory + "/dragfm-agent"
			if candidate.kind == "hans" {
				binary, err = remoteagent.InstallHans(ctx, helper, strings.TrimSpace(architecture.String()))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := helper.StartHansServer("lifecycle", binary, "10.253.78.0", helper.Directory+"/identity", helper.Directory+"/leases", "hosted-disposable-lifecycle-secret"); err != nil {
					t.Fatal(err)
				}
			} else {
				channel, err := owned.SSHClient().NewSession()
				if err != nil {
					t.Fatal(err)
				}
				defer channel.Close()
				stopChannel := context.AfterFunc(ctx, func() { _ = channel.Close() })
				defer stopChannel()
				input, err := channel.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				output, err := channel.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := channel.Start("exec sudo -n -- " + shellQuote(binary) + " --sftp"); err != nil {
					t.Fatal(err)
				}
				client, err := sftp.NewClientPipe(output, input)
				if err != nil {
					t.Fatal("uploaded helper did not serve independent standard SFTP", err)
				}
				defer func() {
					// SSH Session.Close sends channel-close; it does not close
					// the local read buffer until the peer replies. This fault
					// fixture may have lost that reply. Close its dedicated
					// transport before joining the SFTP reader. The browser is
					// a different connection and must remain usable below.
					_ = channel.Close()
					_ = owned.Close()
					_ = client.Close()
				}()
				file, err := client.Open(binary)
				if err != nil {
					t.Fatal(err)
				}
				var magic [4]byte
				_, err = io.ReadFull(file, magic[:])
				_ = file.Close()
				if err != nil || string(magic[:]) != "\x7fELF" {
					t.Fatal("independent SFTP could not read the actual helper binary", err)
				}
			}
			input, feed := io.Pipe()
			output, stream := io.Pipe()
			defer input.Close()
			defer feed.Close()
			defer output.Close()
			defer stream.Close()
			done := make(chan error, 1)
			mode := "disconnect"
			if crash {
				mode = "crash"
			}
			// Only the exact installation and pidfds discovered below can be
			// signalled/removed. The test fixture is disposable, not a user host.
			command := "sudo -n python3 -u -c " + shellQuote(helperExitObserver) + " " + shellQuote(helper.Directory) + " " + mode + " " + candidate.kind
			go func() {
				var diagnostic bytes.Buffer
				err := source.Exec(ctx, command, endpoint.ExecOptions{Stdin: input, Stdout: stream, Stderr: &diagnostic})
				if err != nil && diagnostic.Len() != 0 {
					err = errors.Join(err, errors.New(diagnostic.String()))
				}
				_ = stream.CloseWithError(err)
				done <- err
			}()
			stop := context.AfterFunc(ctx, func() { _ = output.CloseWithError(ctx.Err()); _ = feed.CloseWithError(ctx.Err()) })
			defer stop()
			reader := bufio.NewReader(output)
			if line, err := reader.ReadString('\n'); err != nil || line != "ready\n" {
				t.Fatalf("real %s process readiness: %q %v", candidate.kind, line, err)
			}
			var closeErr error
			if crash {
				closeErr = helper.Close()
				// Even though the helper is reaped, its stopped dependent owns a
				// directory descriptor. Never upgrade that same open
				// description or fall back to an unconditional remote Remove.
				probe := "exec 9< " + shellQuote(helper.Directory) + "; flock -x -n -E 73 9; test \"$?\" = 73"
				if err := source.Exec(ctx, "exec bash --noprofile --norc -c "+shellQuote(probe), endpoint.ExecOptions{}); err != nil {
					t.Errorf("live %s did not retain its directory lease: %v", candidate.kind, err)
				}
				if _, err := source.Stat(ctx, binary); err != nil {
					t.Errorf("helper close removed a live %s installation: %v", candidate.kind, err)
				}
				if closeErr == nil {
					t.Error("lost control connection claimed confirmed cleanup")
				}
			} else {
				_ = owned.Close() // Actual control transport loss, not CancelJob.
			}
			if _, err := io.WriteString(feed, "finish\n"); err != nil {
				t.Fatal(err)
			}
			_ = feed.Close()
			if line, err := reader.ReadString('\n'); err != nil || line != "exited\n" {
				t.Fatalf("helper/%s exits were not observed: %q %v", candidate.kind, line, err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !crash {
				if _, err := source.Stat(ctx, helper.Directory); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("EOF shutdown did not remove the quiescent installation: %v", err)
				}
			}
			if _, err := source.List(ctx, "/tmp"); err != nil {
				t.Fatalf("helper shutdown disturbed browsing: %v", err)
			}
		})
	}
}

const helperExitObserver = `import os, sys, signal, select, shutil, ctypes
directory, mode, kind = sys.argv[1:4]
preserve = len(sys.argv) > 4 and sys.argv[4] == 'preserve'
helper = int(open(directory + '/.dragfm-agent-pid').read())
children = []
for item in os.scandir('/proc'):
    if not item.name.isdecimal():
        continue
    try:
        executable = os.readlink(item.path + '/exe')
        args = open(item.path + '/cmdline', 'rb').read().split(b'\0')
        if ((kind == 'hans' and executable == directory + '/hans') or
            (kind == 'sftp' and executable == directory + '/dragfm-agent' and b'--sftp' in args)):
            children.append(int(item.name))
    except (FileNotFoundError, PermissionError):
        pass
assert len(children) == 1, 'expected one exact helper dependent'
helperfd, childfd = os.pidfd_open(helper), os.pidfd_open(children[0])
traced = False
def exited(fd):
    return bool(select.select([fd], [], [], 8)[0])
def interrupted(signum, frame):
    raise RuntimeError('fixture observer interrupted')
try:
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGHUP, interrupted)
    if mode == 'crash':
        # SIGSTOP delivery is asynchronous. Observe the kernel group-stop
        # via waitpid instead of racing a one-shot /proc/status read. Seize
        # needs SYS_PTRACE only in this disposable fault-injection container.
        libc = ctypes.CDLL(None, use_errno=True)
        libc.ptrace.argtypes = [ctypes.c_uint, ctypes.c_int, ctypes.c_void_p, ctypes.c_void_p]
        libc.ptrace.restype = ctypes.c_long
        def ptrace(request, data=0):
            if libc.ptrace(request, children[0], None, ctypes.c_void_p(data)) == -1:
                code = ctypes.get_errno()
                raise OSError(code, os.strerror(code))
        ptrace(0x4206)  # PTRACE_SEIZE, no options or exit-kill behavior
        traced = True
        signal.signal(signal.SIGALRM, interrupted)
        signal.alarm(8)
        signal.pidfd_send_signal(childfd, signal.SIGSTOP)
        _, status = os.waitpid(children[0], 0x40000000)  # __WALL
        assert os.WIFSTOPPED(status) and os.WSTOPSIG(status) == signal.SIGSTOP, 'missing stop delivery'
        ptrace(7, signal.SIGSTOP)  # PTRACE_CONT delivers the group-stop signal
        _, status = os.waitpid(children[0], 0x40000000)
        assert os.WIFSTOPPED(status) and status >> 16 == 128, 'missing PTRACE_EVENT_STOP'
        ptrace(0x4208)  # PTRACE_LISTEN: remain group-stopped, not resumed
        signal.alarm(0)
        signal.pidfd_send_signal(helperfd, signal.SIGKILL)
        assert exited(helperfd), 'helper was not reaped'
    print('ready', flush=True)
    assert sys.stdin.readline() == 'finish\n', 'controller observer ended early'
    if mode == 'crash':
        signal.pidfd_send_signal(childfd, signal.SIGKILL)
    assert exited(childfd), 'dependent survived control loss'
    assert exited(helperfd), 'helper survived control loss'
    print('exited', flush=True)
finally:
    signal.alarm(0)
    for fd in (childfd, helperfd):
        try:
            signal.pidfd_send_signal(fd, signal.SIGKILL)
        except ProcessLookupError:
            pass
        assert exited(fd), 'fixture process exit unconfirmed'
        if fd == childfd and traced:
            os.waitpid(children[0], 0x40000000)
        os.close(fd)
    if mode == 'crash' and not preserve and os.path.exists(directory):
        shutil.rmtree(directory, ignore_errors=False)
`
