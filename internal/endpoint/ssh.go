package endpoint

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flyssh/flyssh/pkg/connector"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Remote struct {
	closed         atomic.Bool
	closeOnce      sync.Once
	closeErr       error
	name           string
	fingerprint    string
	chain          *connector.Chain
	client         *ssh.Client
	sftp           *sftp.Client
	sftpError      error
	connectionHost string
	dialRoute      connector.Route
	// A task may supply a separate privileged SFTP channel while borrowing the
	// authenticated SSH identity. Closing that view must not close the chain
	// used by browsing/PTYs. The transport owns the SFTP client and its EOF/Wait
	// shutdown, so Remote.Close must not close that client a second time.
	// Commands still run as the original SSH account.
	fileTransport io.Closer
	fileVersion   func(context.Context, string) (uint64, uint64, error)
	physicalPath  func(context.Context, string) (string, error)
	partial       func(context.Context, string, bool) error
	fileOwner     func(context.Context, string, uint32, uint32) error
	fileSync      func(context.Context, []string) error
	commands      *commandFilesystem
}

func (r *Remote) BorrowFileChannel(client *sftp.Client, transport io.Closer, version func(context.Context, string) (uint64, uint64, error), physical func(context.Context, string) (string, error), partial func(context.Context, string, bool) error, owner func(context.Context, string, uint32, uint32) error, syncPaths func(context.Context, []string) error) *Remote {
	return &Remote{name: r.name, fingerprint: r.fingerprint, client: r.client,
		sftp: client, connectionHost: r.connectionHost, fileTransport: transport,
		fileVersion: version, physicalPath: physical, partial: partial, fileOwner: owner, fileSync: syncPaths}
}

func (r *Remote) SetOwner(ctx context.Context, path string, uid, gid uint32) error {
	if r.fileOwner == nil {
		return errors.New("ownership changes require an approved privileged file channel")
	}
	return r.fileOwner(ctx, path, uid, gid)
}

func DialSSH(ctx context.Context, name, _ string, route connector.Route) (*Remote, error) {
	// The legacy fingerprint argument is display/configuration metadata, not
	// proof of the key used by this route. Keep only a pin actually enforced by
	// connector.Dial, or a key that its final-hop confirmation accepted.
	fingerprint := ""
	route.Hops = append([]connector.Hop(nil), route.Hops...)
	verified := make([]string, len(route.Hops))
	for i := range route.Hops {
		hop := &route.Hops[i]
		verified[i] = hop.HostKey.PinnedSHA256
		if verified[i] == "" && hop.HostKey.ConfirmNew != nil {
			confirm := hop.HostKey.ConfirmNew
			hop.HostKey.ConfirmNew = func(address, key string) bool {
				if !confirm(address, key) {
					return false
				}
				verified[i] = key
				return true
			}
		}
	}
	chain, err := connector.Dial(ctx, route)
	if err != nil {
		return nil, err
	}
	for i := range route.Hops {
		if verified[i] != "" {
			route.Hops[i].HostKey.PinnedSHA256 = verified[i]
			route.Hops[i].HostKey.ConfirmNew = nil
		}
	}
	if len(verified) > 0 {
		fingerprint = verified[len(verified)-1]
	}
	connectionHost := ""
	if len(route.Hops) > 0 {
		connectionHost = route.Hops[len(route.Hops)-1].Host
	}
	remote := &Remote{name: name, fingerprint: fingerprint, chain: chain, client: chain.Final(), connectionHost: connectionHost, dialRoute: route}
	// NewClient performs session/subsystem/INIT-VERSION negotiation before it
	// returns a client. Ordinary watchIO cannot protect this interval yet.
	// Bound it and close only this newly owned route on cancellation; Fork
	// must never tear down the original browsing/PTY route.
	negotiationTimeout := route.Timeout
	if negotiationTimeout <= 0 || negotiationTimeout > 15*time.Second {
		negotiationTimeout = 15 * time.Second
	}
	negotiation, cancel := context.WithTimeout(ctx, negotiationTimeout)
	closed := make(chan struct{})
	stop := context.AfterFunc(negotiation, func() {
		_ = chain.Close()
		close(closed)
	})
	remote.sftp, remote.sftpError = sftp.NewClient(remote.client)
	if !stop() {
		<-closed
	}
	negotiationErr := negotiation.Err()
	cancel()
	if negotiationErr != nil {
		_ = remote.Close()
		return nil, fmt.Errorf("SFTP 初始化未完成: %w", errors.Join(negotiationErr, remote.sftpError))
	}
	// A prompt subsystem rejection still permits the existing POSIX fallback.
	return remote, nil
}

// Fork opens the exact already-authenticated route, including any composed
// relay and enforced pins. A task can close its stalled transport without
// tearing down the browser/PTY transport or rereading edited host settings.
func (r *Remote) Fork(ctx context.Context) (*Remote, error) {
	if len(r.dialRoute.Hops) == 0 {
		return nil, errors.New("remote has no authenticated reconnect route")
	}
	return DialSSH(ctx, r.name, r.fingerprint, r.dialRoute)
}

func (r *Remote) SFTPError() error       { return r.sftpError }
func (r *Remote) SSHClient() *ssh.Client { return r.client }
func (r *Remote) ConnectionHost() string { return r.connectionHost }
func (r *Remote) Name() string           { return r.name }

// AvailableBytes uses the SFTP statvfs extension so capability probing does
// not depend on a login shell preserving stdout from df/awk pipelines.
func (r *Remote) AvailableBytes(ctx context.Context, target string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if r.sftp == nil {
		return -1, errors.New("SFTP statvfs is unavailable")
	}
	statistics, err := r.sftp.StatVFS(target)
	if err != nil {
		return -1, err
	}
	available := statistics.Frsize * statistics.Bavail
	if available > uint64(^uint64(0)>>1) {
		return int64(^uint64(0) >> 1), nil
	}
	return int64(available), nil
}

func (r *Remote) FileVersion(ctx context.Context, target string) (uint64, uint64, error) {
	if r.fileVersion != nil {
		return r.fileVersion(ctx, target)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	statCommand := "if stat -c '%d %i' -- " + shellQuote(target) + " >/dev/null 2>&1; then stat -c '%d %i' -- " + shellQuote(target) + "; else stat -f '%d %i' -- " + shellQuote(target) + "; fi"
	parse := func(value string) (uint64, uint64, error) {
		fields := strings.Fields(value)
		if len(fields) != 2 {
			return 0, 0, errors.New("stat returned no file identity")
		}
		device, deviceErr := strconv.ParseUint(fields[0], 10, 64)
		inode, inodeErr := strconv.ParseUint(fields[1], 10, 64)
		if err := errors.Join(deviceErr, inodeErr); err != nil || inode == 0 {
			return 0, 0, errors.Join(err, errors.New("stat returned an invalid inode"))
		}
		return device, inode, nil
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		var output bytes.Buffer
		lastErr = r.Exec(ctx, statCommand, ExecOptions{Stdout: &output, Stderr: &output})
		if lastErr == nil {
			if device, inode, parseErr := parse(output.String()); parseErr == nil {
				return device, inode, nil
			} else {
				lastErr = parseErr
			}
		}
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
	}
	if r.sftp != nil {
		probePath := path.Join("/tmp", ".dragfm-stat-"+randomSuffix())
		defer r.sftp.Remove(probePath)
		command := "umask 077; { " + statCommand + "; } > " + shellQuote(probePath)
		if execErr := r.Exec(ctx, command, ExecOptions{}); execErr == nil {
			probe, openErr := r.sftp.Open(probePath)
			if openErr == nil {
				data, readErr := io.ReadAll(io.LimitReader(probe, 256))
				readErr = errors.Join(readErr, probe.Close())
				if readErr != nil {
					lastErr = readErr
				} else if device, inode, parseErr := parse(string(data)); parseErr == nil {
					return device, inode, nil
				} else {
					lastErr = parseErr
				}
			} else {
				lastErr = openErr
			}
		} else {
			lastErr = execErr
		}
	}
	return 0, 0, errors.Join(errors.New("remote stat returned no file identity"), lastErr)
}

func (r *Remote) identityViaCommand(ctx context.Context) (Identity, error) {
	var stdout bytes.Buffer
	err := r.Exec(ctx, "if test -r /etc/machine-id; then cat /etc/machine-id; elif test -r /var/lib/dbus/machine-id; then cat /var/lib/dbus/machine-id; fi", ExecOptions{Stdout: &stdout})
	return Identity{Kind: SSHKind, Name: r.name, MachineID: strings.TrimSpace(stdout.String()), Fingerprint: r.fingerprint}, err
}

func (r *Remote) Home(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if r.sftp != nil {
		return r.sftp.Getwd()
	}
	var stdout bytes.Buffer
	if err := r.Exec(ctx, "printf '%s' \"$HOME\"", ExecOptions{Stdout: &stdout}); err != nil {
		return "", err
	}
	return stdout.String(), nil
}

func (r *Remote) Abs(ctx context.Context, value string) (string, error) {
	if value == "" || value == "~" {
		return r.Home(ctx)
	}
	if strings.HasPrefix(value, "~/") {
		home, err := r.Home(ctx)
		if err != nil {
			return "", err
		}
		return path.Join(home, strings.TrimPrefix(value, "~/")), nil
	}
	if path.IsAbs(value) {
		return path.Clean(value), nil
	}
	home, err := r.Home(ctx)
	if err != nil {
		return "", err
	}
	return path.Join(home, value), nil
}

func (r *Remote) Join(parts ...string) string { return path.Join(parts...) }
func (r *Remote) Dir(value string) string     { return path.Dir(value) }

func (r *Remote) List(ctx context.Context, directory string) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if r.sftp == nil {
		return r.listPOSIX(ctx, directory)
	}
	items, err := r.sftp.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		entry := Entry{Name: item.Name(), Path: path.Join(directory, item.Name()), Mode: item.Mode(), Size: item.Size(), Modified: item.ModTime()}
		if stat, ok := item.Sys().(*sftp.FileStat); ok {
			entry.UID, entry.GID, entry.OwnerKnown = stat.UID, stat.GID, true
		}
		if item.Mode()&fs.ModeSymlink != 0 {
			entry.LinkTarget, _ = r.sftp.ReadLink(entry.Path)
		}
		entries = append(entries, entry)
	}
	sortEntries(entries)
	return entries, nil
}

func (r *Remote) Stat(ctx context.Context, target string) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if r.sftp == nil {
		entries, err := r.findEntries(ctx, target, true)
		if err != nil {
			var status bytes.Buffer
			// A missing immediate parent is not evidence of denied traversal.
			// Walk up to the first existing ancestor, without resolving links or
			// executing path contents. Only a searchable directory proves ENOENT.
			probe := "p=" + shellQuote(target) + `
if [ -e "$p" ] || [ -L "$p" ]; then printf exists; exit; fi
while :; do
  case "$p" in
    /|.) printf unknown; break ;;
    */*) p=${p%/*}; [ -n "$p" ] || p=/ ;;
    *) p=. ;;
  esac
  if [ -e "$p" ] || [ -L "$p" ]; then
    if [ ! -d "$p" ]; then printf unknown
    elif [ -x "$p" ]; then printf missing
    else printf denied; fi
    break
  fi
done`
			if probeErr := r.Exec(ctx, probe, ExecOptions{Stdout: &status}); probeErr == nil {
				switch status.String() {
				case "missing":
					return Entry{}, fs.ErrNotExist
				case "denied":
					return Entry{}, fs.ErrPermission
				}
			}
			return Entry{}, err
		}
		if len(entries) != 1 {
			return Entry{}, fs.ErrNotExist
		}
		return entries[0], nil
	}
	info, err := r.sftp.Lstat(target)
	if err != nil {
		return Entry{}, err
	}
	entry := Entry{Name: info.Name(), Path: target, Mode: info.Mode(), Size: info.Size(), Modified: info.ModTime()}
	if stat, ok := info.Sys().(*sftp.FileStat); ok {
		entry.UID, entry.GID, entry.OwnerKnown = stat.UID, stat.GID, true
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		entry.LinkTarget, _ = r.sftp.ReadLink(target)
	}
	return entry, nil
}

func (r *Remote) Readlink(ctx context.Context, target string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if r.sftp != nil {
		return r.sftp.ReadLink(target)
	}
	var stdout bytes.Buffer
	err := r.Exec(ctx, "readlink -- "+shellQuote(target), ExecOptions{Stdout: &stdout})
	return strings.TrimSuffix(stdout.String(), "\n"), err
}

func (r *Remote) open(ctx context.Context, target string) (io.ReadCloser, error) {
	if r.sftp != nil {
		return r.sftp.Open(target)
	}
	if r.commands != nil {
		return r.openCommandFile(ctx, target)
	}
	session, err := r.client.NewSession()
	if err != nil {
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	if err := session.Start("exec cat -- " + shellQuote(target)); err != nil {
		_ = session.Close()
		return nil, err
	}
	return &sessionReader{Reader: stdout, session: session}, nil
}

func (r *Remote) createAtomic(ctx context.Context, target string, mode fs.FileMode) (AtomicWriter, error) {
	temporary := path.Join(path.Dir(target), ".dragfm-partial-"+randomSuffix())
	if r.commands != nil {
		return r.createCommandFile(ctx, temporary, target, mode)
	}
	if r.sftp != nil {
		if r.partial != nil {
			if err := r.partial(ctx, temporary, true); err != nil {
				return nil, err
			}
		}
		file, err := r.sftp.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err != nil {
			return nil, err
		}
		if err := file.Chmod(mode.Perm()); err != nil {
			_ = file.Close()
			_ = r.sftp.Remove(temporary)
			return nil, err
		}
		if r.partial != nil {
			if err := r.partial(ctx, temporary, true); err != nil {
				_ = file.Close()
				return nil, err // The owner retains cleanup; no data was written.
			}
		}
		return &sftpAtomicWriter{file: file, client: r.sftp, temporary: temporary, target: target, partial: r.partial}, nil
	}
	session, err := r.client.NewSession()
	if err != nil {
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	command := fmt.Sprintf("umask 077; set -C; exec cat > %s", shellQuote(temporary))
	if err := session.Start(command); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("start writer for retained partial %q: %w", temporary, errors.Join(ErrCommandExitUnconfirmed, err))
	}
	exit := make(chan error, 1)
	go func() { exit <- session.Wait() }()
	return &sshAtomicWriter{WriteCloser: stdin, session: session, remote: r, temporary: temporary, target: target, mode: mode, ctx: ctx, exit: exit}, nil
}

func (r *Remote) MkdirAll(ctx context.Context, target string, mode fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if entry, err := r.Stat(ctx, target); err == nil {
		if !entry.IsDir() {
			return fmt.Errorf("directory %q is not a real directory", target)
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if r.sftp != nil {
		if r.partial != nil {
			if err := r.partial(ctx, target, true); err != nil {
				return err
			}
		}
		if err := r.sftp.MkdirAll(target); err != nil {
			return err
		}
		if err := r.sftp.Chmod(target, mode.Perm()); err != nil {
			return err
		}
		if r.partial != nil {
			return r.partial(ctx, target, true) // Pin the actual directory inode.
		}
		return nil
	}
	if r.partial != nil {
		if err := r.partial(ctx, target, true); err != nil {
			return err
		}
	}
	if err := r.Exec(ctx, fmt.Sprintf("mkdir -p -- %s && chmod %04o -- %s", shellQuote(target), mode.Perm(), shellQuote(target)), ExecOptions{}); err != nil {
		return err
	}
	if r.partial != nil {
		return r.partial(ctx, target, true)
	}
	return nil
}

func (r *Remote) Symlink(ctx context.Context, linkTarget, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if r.sftp != nil {
		if r.partial != nil {
			if err := r.partial(ctx, target, true); err != nil {
				return err
			}
		}
		if err := r.sftp.Symlink(linkTarget, target); err != nil {
			return err
		}
		if r.partial != nil {
			return r.partial(ctx, target, true) // Pin the link, not its target.
		}
		return nil
	}
	if r.partial != nil {
		if err := r.partial(ctx, target, true); err != nil {
			return err
		}
	}
	if err := r.Exec(ctx, "ln -s -- "+shellQuote(linkTarget)+" "+shellQuote(target), ExecOptions{}); err != nil {
		return err
	}
	if r.partial != nil {
		return r.partial(ctx, target, true)
	}
	return nil
}

func (r *Remote) Chmod(ctx context.Context, target string, mode fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if r.sftp != nil {
		return r.sftp.Chmod(target, mode.Perm())
	}
	return r.Exec(ctx, fmt.Sprintf("chmod %04o -- %s", mode.Perm(), shellQuote(target)), ExecOptions{})
}

func (r *Remote) Chtimes(ctx context.Context, target string, atime, mtime time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if r.sftp != nil {
		return r.sftp.Chtimes(target, atime, mtime)
	}
	return r.Exec(ctx, "touch -a -d @"+strconv.FormatInt(atime.Unix(), 10)+" -- "+shellQuote(target)+" && touch -m -d @"+strconv.FormatInt(mtime.Unix(), 10)+" -- "+shellQuote(target), ExecOptions{})
}

func (r *Remote) Remove(ctx context.Context, target string, recursive bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.sftp != nil && !recursive {
		err := r.sftp.Remove(target)
		if err == nil && r.partial != nil {
			return r.partial(ctx, target, false)
		}
		return err
	}
	if r.sftp != nil && r.fileTransport != nil {
		err := r.sftp.RemoveAll(target)
		if err == nil && r.partial != nil {
			return r.partial(ctx, target, false)
		}
		return err
	}
	flag := ""
	if recursive {
		flag = "-r"
	}
	if err := r.Exec(ctx, "rm "+flag+" -- "+shellQuote(target), ExecOptions{}); err != nil {
		return err
	}
	if r.partial != nil {
		return r.partial(ctx, target, false)
	}
	return nil
}

func (r *Remote) Rename(ctx context.Context, source, target string, overwrite bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.sftp != nil {
		if !overwrite {
			if _, err := r.sftp.Lstat(target); err == nil {
				return fs.ErrExist
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		if err := renameSFTP(r.sftp, source, target, overwrite); err != nil {
			return err
		}
		if r.partial != nil {
			return r.partial(ctx, source, false)
		}
		return nil
	}
	flag := ""
	if !overwrite {
		flag = "-n"
	}
	command := "mv " + flag + " -T -- " + shellQuote(source) + " " + shellQuote(target)
	if !overwrite {
		command += "; result=$?; [ \"$result\" -eq 0 ] || exit \"$result\"; if [ -e " + shellQuote(source) + " ] || [ -L " + shellQuote(source) + " ]; then exit 73; fi"
	}
	if err := r.Exec(ctx, command, ExecOptions{}); err != nil {
		return err
	}
	if r.partial != nil {
		return r.partial(ctx, source, false)
	}
	return nil
}

func (r *Remote) CopyNative(ctx context.Context, source, target string, sourceDirectory, merge bool) error {
	if merge && sourceDirectory {
		return r.Exec(ctx, "cp -a -- "+shellQuote(strings.TrimSuffix(path.Clean(source), "/")+"/.")+" "+shellQuote(target), ExecOptions{})
	}
	return r.Exec(ctx, "cp -a -- "+shellQuote(source)+" "+shellQuote(target), ExecOptions{})
}

// Both streams share a lock even when their io.Writer values differ: two
// wrappers may still write to the same underlying non-thread-safe buffer.
type serializedSSHOutput struct {
	mu     *sync.Mutex
	writer io.Writer
}

func (w serializedSSHOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(data)
}

func (r *Remote) Exec(ctx context.Context, command string, options ExecOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.commands != nil {
		return r.commands.run(ctx, command, options)
	}
	stopOpen := r.watchIO(ctx)
	session, err := r.client.NewSession()
	stopOpen()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = options.Stdin
	// x/crypto/ssh copies stdout and stderr concurrently, unlike os/exec's
	// combined-writer handling. Callers may intentionally supply one buffer.
	var outputMu sync.Mutex
	if options.Stdout != nil {
		session.Stdout = serializedSSHOutput{mu: &outputMu, writer: options.Stdout}
	}
	if options.Stderr != nil {
		session.Stderr = serializedSSHOutput{mu: &outputMu, writer: options.Stderr}
	}
	if options.Directory != "" {
		command = "cd -- " + shellQuote(options.Directory) + " && " + command
	}
	done := make(chan error, 1)
	go func() {
		if err := session.Start(command); err != nil {
			// Start's public error does not distinguish a rejected request
			// from transport loss AFTER the server accepted it. Preserve that
			// ambiguity instead of permitting cleanup/retry of a possible writer.
			done <- errors.Join(ErrCommandExitUnconfirmed, err)
			return
		}
		err := session.Wait()
		var exited *ssh.ExitError
		if err != nil && !errors.As(err, &exited) {
			err = errors.Join(ErrCommandExitUnconfirmed, err)
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return r.cancelCommand(ctx, session, done)
	}
}

func (r *Remote) OpenPTY(ctx context.Context, directory, shell string, rows, columns uint) (PTYSession, error) {
	if shell == "" {
		shell = r.detectLoginShell(ctx)
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	nonce := rand.Text()
	loginCommand, cleanupPath, err := r.remoteLoginCommand(ctx, shell, nonce)
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		if cleanupPath != "" {
			_ = r.Remove(context.Background(), cleanupPath, true)
		}
	}
	session, err := r.client.NewSession()
	if err != nil {
		cleanup()
		return nil, err
	}
	input, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		cleanup()
		return nil, err
	}
	output, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		cleanup()
		return nil, err
	}
	if err := session.RequestPty("xterm-256color", int(rows), int(columns), ssh.TerminalModes{ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}); err != nil {
		_ = session.Close()
		cleanup()
		return nil, err
	}
	command := "cd -- " + shellQuote(directory) + " && " + loginCommand
	if err := session.Start(command); err != nil {
		_ = session.Close()
		cleanup()
		return nil, err
	}
	pty := &sshPTY{session: session, input: input, output: output, remote: r, cleanupPath: cleanupPath, nonce: nonce}
	go func() {
		<-ctx.Done()
		_ = pty.Close()
	}()
	return pty, nil
}

func (r *Remote) remoteLoginCommand(ctx context.Context, shell, nonce string) (string, string, error) {
	name := strings.ToLower(path.Base(shell))
	if name == "fish" {
		return "exec " + shellQuote(shell) + " -l -i -C " + shellQuote(fishCWDHook(nonce)), "", nil
	}
	directory := "/tmp/.dragfm-shell-" + randomSuffix()
	if err := r.MkdirAll(ctx, directory, 0700); err != nil {
		return "", "", err
	}
	fail := func(err error) (string, string, error) {
		_ = r.Remove(context.Background(), directory, true)
		return "", "", err
	}
	write := func(name, body string) error {
		target := path.Join(directory, name)
		writer, err := r.CreateAtomic(ctx, target, 0600)
		if err != nil {
			return fmt.Errorf("create remote shell startup %s: %w", target, err)
		}
		if _, err := io.WriteString(writer, body); err != nil {
			_ = writer.Abort()
			return fmt.Errorf("write remote shell startup %s: %w", target, err)
		}
		if err := writer.Commit(); err != nil {
			return fmt.Errorf("commit remote shell startup %s: %w", target, err)
		}
		return nil
	}
	switch name {
	case "bash":
		rc := path.Join(directory, "bashrc")
		if err := write("bashrc", bashStartup(nonce)); err != nil {
			return fail(err)
		}
		command := "exec " + shellQuote(shell) + " --noprofile --rcfile " + shellQuote(rc) + " -i"
		return command, directory, nil
	case "zsh":
		for _, file := range []string{".zshenv", ".zprofile", ".zshrc", ".zlogin"} {
			body := zshStartup(file, directory, nonce)
			if err := write(file, body); err != nil {
				return fail(err)
			}
		}
		return `__DRAGFM_ZDOTDIR=${ZDOTDIR-} __DRAGFM_ZDOTDIR_SET=${ZDOTDIR+1} ZDOTDIR=` + shellQuote(directory) + " exec " + shellQuote(shell) + " -l -i", directory, nil
	default:
		env := path.Join(directory, "env")
		if err := write("env", posixCWDHook(nonce)); err != nil {
			return fail(err)
		}
		return "ENV=" + shellQuote(env) + " exec " + shellQuote(shell) + " -l -i", directory, nil
	}
}

func (r *Remote) detectLoginShell(ctx context.Context) string {
	session, err := r.client.NewSession()
	if err != nil {
		return ""
	}
	defer session.Close()
	done := make(chan struct {
		output []byte
		err    error
	}, 1)
	go func() {
		output, runErr := session.Output(`printf '%s' "${SHELL:-}"`)
		done <- struct {
			output []byte
			err    error
		}{output: output, err: runErr}
	}()
	select {
	case result := <-done:
		if result.err != nil {
			return ""
		}
		return strings.TrimSpace(string(result.output))
	case <-ctx.Done():
		_ = session.Close()
		return ""
	}
}

func (r *Remote) IsClosed() bool { return r.closed.Load() }
func (r *Remote) Close() error {
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		// Close transport first, unblocking pending SFTP requests/channel writes.
		var transportErr, sftpErr error
		if r.commands != nil {
			transportErr = r.commands.close()
		} else if r.fileTransport != nil {
			transportErr = r.fileTransport.Close()
		} else if r.chain != nil {
			transportErr = unexpectedTransportClose(r.chain.Close())
		}
		if r.sftp != nil && r.fileTransport == nil {
			sftpErr = unexpectedTransportClose(r.sftp.Close())
		}
		r.closeErr = errors.Join(transportErr, sftpErr)
	})
	return r.closeErr
}

// Closing our SSH transport first intentionally gives its SFTP reader EOF.
// This is not a remote writer exit acknowledgement. Borrowed file transports
// and command filesystems above keep their own strict process-exit contract;
// never apply this normalization to their cleanup errors.
func unexpectedTransportClose(err error) error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var remaining []error
		for _, child := range joined.Unwrap() {
			remaining = append(remaining, unexpectedTransportClose(child))
		}
		return errors.Join(remaining...)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (r *Remote) listPOSIX(ctx context.Context, directory string) ([]Entry, error) {
	return r.findEntries(ctx, directory, false)
}

func (r *Remote) findEntries(ctx context.Context, target string, exact bool) ([]Entry, error) {
	depth := "-mindepth 1 -maxdepth 1"
	if exact {
		depth = "-maxdepth 0"
	}
	command := "LC_ALL=C find -- " + shellQuote(target) + " " + depth + " -printf '%y\\0%f\\0%s\\0%T@\\0%m\\0%l\\0%U\\0%G\\0'"
	var stdout bytes.Buffer
	if err := r.Exec(ctx, command, ExecOptions{Stdout: &stdout}); err != nil {
		return nil, err
	}
	fields := bytes.Split(stdout.Bytes(), []byte{0})
	if len(fields) > 0 && len(fields[len(fields)-1]) == 0 {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%8 != 0 {
		return nil, errors.New("invalid POSIX directory response")
	}
	entries := make([]Entry, 0, len(fields)/8)
	for index := 0; index < len(fields); index += 8 {
		name := string(fields[index+1])
		entryPath := target
		if !exact {
			entryPath = path.Join(target, name)
		}
		size, sizeErr := strconv.ParseInt(string(fields[index+2]), 10, 64)
		modified, timeErr := parseFindTime(string(fields[index+3]))
		permissions, modeErr := strconv.ParseUint(string(fields[index+4]), 8, 32)
		uid, uidErr := strconv.ParseUint(string(fields[index+6]), 10, 32)
		gid, gidErr := strconv.ParseUint(string(fields[index+7]), 10, 32)
		if sizeErr != nil || timeErr != nil || modeErr != nil || uidErr != nil || gidErr != nil {
			return nil, errors.New("invalid POSIX directory metadata")
		}
		mode := fs.FileMode(permissions)
		switch string(fields[index]) {
		case "d":
			mode |= fs.ModeDir
		case "l":
			mode |= fs.ModeSymlink
		case "p":
			mode |= fs.ModeNamedPipe
		case "s":
			mode |= fs.ModeSocket
		case "c":
			mode |= fs.ModeDevice | fs.ModeCharDevice
		case "b":
			mode |= fs.ModeDevice
		}
		entries = append(entries, Entry{Name: name, Path: entryPath, Mode: mode, Size: size, Modified: modified, LinkTarget: string(fields[index+5]), UID: uint32(uid), GID: uint32(gid), OwnerKnown: true})
	}
	sortEntries(entries)
	return entries, nil
}

// GNU find's %T@ has a variable-length decimal fraction. Converting the
// epoch through float64 loses nanoseconds, changing ordering and snapshots.
func parseFindTime(value string) (time.Time, error) {
	whole, fraction, _ := strings.Cut(value, ".")
	seconds, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	var nanos int64
	for i, digit := range fraction {
		if digit < '0' || digit > '9' {
			return time.Time{}, errors.New("invalid timestamp fraction")
		}
		if i < 9 {
			nanos = nanos*10 + int64(digit-'0')
		}
	}
	for i := len(fraction); i < 9; i++ {
		nanos *= 10
	}
	if strings.HasPrefix(whole, "-") {
		nanos = -nanos
	}
	return time.Unix(seconds, nanos), nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

type sessionReader struct {
	io.Reader
	session  *ssh.Session
	once     sync.Once
	closeErr error
}

type sshPTY struct {
	session     *ssh.Session
	input       io.WriteCloser
	output      io.Reader
	remote      *Remote
	cleanupPath string
	nonce       string
	once        sync.Once
}

func (p *sshPTY) CWDNonce() string      { return p.nonce }
func (p *sshPTY) Input() io.WriteCloser { return p.input }
func (p *sshPTY) Output() io.Reader     { return p.output }
func (p *sshPTY) Resize(rows, columns uint) error {
	return p.session.WindowChange(int(rows), int(columns))
}
func (p *sshPTY) Wait() error { return p.session.Wait() }
func (p *sshPTY) Close() error {
	var err error
	p.once.Do(func() {
		err = errors.Join(p.input.Close(), p.session.Close())
		if p.remote != nil && p.cleanupPath != "" {
			err = errors.Join(err, p.remote.Remove(context.Background(), p.cleanupPath, true))
		}
	})
	return err
}

func (r *sessionReader) Close() error {
	r.once.Do(func() {
		closeErr := r.session.Close()
		waitErr := r.session.Wait()
		// A short cat can finish and close its channel before this call.
		// Only an actual successful exit permits ignoring its already-closed
		// transport result; missing/nonzero exit evidence must remain an error.
		if waitErr == nil {
			closeErr = unexpectedTransportClose(closeErr)
		}
		r.closeErr = errors.Join(closeErr, waitErr)
	})
	return r.closeErr
}

type sftpAtomicWriter struct {
	file              *sftp.File
	client            *sftp.Client
	temporary, target string
	done, closed      bool
	closeErr          error
	partial           func(context.Context, string, bool) error
}

func (w *sftpAtomicWriter) Write(data []byte) (int, error) { return w.file.Write(data) }
func (w *sftpAtomicWriter) Close() error {
	if !w.closed {
		w.closed = true
		w.closeErr = w.file.Close()
	}
	return w.closeErr
}
func (w *sftpAtomicWriter) Commit() error {
	if w.done {
		return errors.New("atomic writer already completed")
	}
	if _, supported := w.client.HasExtension("fsync@openssh.com"); supported {
		if err := w.file.Sync(); err != nil {
			return durabilityError{errors.Join(err, w.Abort())}
		}
	}
	if err := w.Close(); err != nil {
		// Keep ownership registered when the server did not acknowledge CLOSE.
		// The owning lease must prove the writer stopped before cleanup.
		w.done = true
		return fmt.Errorf("SFTP writer close unconfirmed; retained partial %q: %w", w.temporary, errors.Join(ErrCommandExitUnconfirmed, err))
	}
	if err := renameSFTP(w.client, w.temporary, w.target, true); err != nil {
		return errors.Join(err, w.Abort())
	}
	w.done = true
	if w.partial != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return w.partial(ctx, w.temporary, false)
	}
	return nil
}

func (w *sftpAtomicWriter) PrepareStaged() (string, error) {
	if w.done {
		return w.temporary, errors.New("atomic writer already completed")
	}
	if err := w.Close(); err != nil {
		return w.temporary, errors.Join(ErrCommandExitUnconfirmed, err)
	}
	return w.temporary, nil
}
func (w *sftpAtomicWriter) Abort() error {
	if w.done {
		return nil
	}
	w.done = true
	if err := w.Close(); err != nil {
		return fmt.Errorf("SFTP writer close unconfirmed; retained partial %q: %w", w.temporary, errors.Join(ErrCommandExitUnconfirmed, err))
	}
	if err := w.client.Remove(w.temporary); err != nil {
		return err
	}
	if w.partial != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return w.partial(ctx, w.temporary, false)
	}
	return nil
}

type sshAtomicWriter struct {
	io.WriteCloser
	session           *ssh.Session
	remote            *Remote
	temporary, target string
	mode              fs.FileMode
	done, closed      bool
	ctx               context.Context
	exit              <-chan error
	waitOnce          sync.Once
	exitErr, closeErr error
	abortErr          error
}

func (w *sshAtomicWriter) Close() error {
	if w.closed {
		return w.closeErr
	}
	w.closed = true
	w.closeErr = w.WriteCloser.Close()
	return w.closeErr
}

func (w *sshAtomicWriter) wait(ctx context.Context) error {
	w.waitOnce.Do(func() {
		select {
		case err := <-w.exit:
			w.exitErr = err
			var status *ssh.ExitError
			if err != nil && !errors.As(err, &status) {
				w.exitErr = errors.Join(ErrCommandExitUnconfirmed, err)
			}
		case <-ctx.Done():
			// Reuse the bounded TERM/Wait/KILL path. Closing an SSH channel
			// alone never authorizes erasing a path still open by remote cat.
			w.exitErr = w.remote.cancelCommand(ctx, w.session, w.exit)
		}
		_ = w.session.Close()
	})
	return w.exitErr
}
func (w *sshAtomicWriter) Commit() error {
	if w.done {
		return errors.New("atomic writer already completed")
	}
	if err := w.Close(); err != nil {
		return errors.Join(err, w.Abort())
	}
	if err := w.wait(w.ctx); err != nil {
		return errors.Join(err, w.Abort())
	}
	command := fmt.Sprintf("chmod %04o -- %s && sync -f -- %s && mv -f -T -- %s %s", w.mode.Perm(), shellQuote(w.temporary), shellQuote(w.temporary), shellQuote(w.temporary), shellQuote(w.target))
	if err := w.remote.Exec(w.ctx, command, ExecOptions{}); err != nil {
		// A commit command can itself outlive a failed transport. Do not
		// race its fsync/rename with cleanup in a second SSH session.
		if errors.Is(err, ErrCommandExitUnconfirmed) {
			w.done = true
			return fmt.Errorf("commit unconfirmed; retained partial %q: %w", w.temporary, err)
		}
		return errors.Join(err, w.Abort())
	}
	w.done = true
	return nil
}

func (w *sshAtomicWriter) PrepareStaged() (string, error) {
	if w.done {
		return w.temporary, errors.New("atomic writer already completed")
	}
	return w.temporary, errors.Join(w.Close(), w.wait(w.ctx))
}
func (w *sshAtomicWriter) Abort() error {
	if w.done {
		return w.abortErr
	}
	w.done = true
	closeErr := w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	exitErr := w.wait(ctx)
	cancel()
	if errors.Is(exitErr, ErrCommandExitUnconfirmed) {
		w.abortErr = fmt.Errorf("writer exit unconfirmed; retained partial %q: %w", w.temporary, errors.Join(closeErr, exitErr))
		return w.abortErr
	}
	cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	removeErr := w.remote.Exec(cleanup, "rm -f -- "+shellQuote(w.temporary), ExecOptions{})
	w.abortErr = errors.Join(closeErr, exitErr, removeErr)
	if w.abortErr != nil {
		w.abortErr = fmt.Errorf("partial shutdown/cleanup %q: %w", w.temporary, w.abortErr)
	}
	return w.abortErr
}
