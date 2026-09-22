package endpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
}

func DialSSH(ctx context.Context, name, fingerprint string, route connector.Route) (*Remote, error) {
	chain, err := connector.Dial(ctx, route)
	if err != nil {
		return nil, err
	}
	connectionHost := ""
	if len(route.Hops) > 0 {
		connectionHost = route.Hops[len(route.Hops)-1].Host
	}
	remote := &Remote{name: name, fingerprint: fingerprint, chain: chain, client: chain.Final(), connectionHost: connectionHost}
	remote.sftp, remote.sftpError = sftp.NewClient(remote.client)
	return remote, nil
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

func (r *Remote) Identity(ctx context.Context) (Identity, error) {
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
			probe := "if [ -e " + shellQuote(target) + " ] || [ -L " + shellQuote(target) + " ]; then printf exists; elif [ -x " + shellQuote(path.Dir(target)) + " ]; then printf missing; else printf denied; fi"
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

func (r *Remote) open(_ context.Context, target string) (io.ReadCloser, error) {
	if r.sftp != nil {
		return r.sftp.Open(target)
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

func (r *Remote) createAtomic(_ context.Context, target string, mode fs.FileMode) (AtomicWriter, error) {
	temporary := path.Join(path.Dir(target), ".dragfm-partial-"+randomSuffix())
	if r.sftp != nil {
		file, err := r.sftp.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if err != nil {
			return nil, err
		}
		if err := file.Chmod(mode.Perm()); err != nil {
			_ = file.Close()
			_ = r.sftp.Remove(temporary)
			return nil, err
		}
		return &sftpAtomicWriter{file: file, client: r.sftp, temporary: temporary, target: target}, nil
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
		return nil, err
	}
	return &sshAtomicWriter{WriteCloser: stdin, session: session, remote: r, temporary: temporary, target: target, mode: mode}, nil
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
		if err := r.sftp.MkdirAll(target); err != nil {
			return err
		}
		return r.sftp.Chmod(target, mode.Perm())
	}
	return r.Exec(ctx, fmt.Sprintf("mkdir -p -- %s && chmod %04o -- %s", shellQuote(target), mode.Perm(), shellQuote(target)), ExecOptions{})
}

func (r *Remote) Symlink(ctx context.Context, linkTarget, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stopIO := r.watchIO(ctx)
	defer stopIO()
	if r.sftp != nil {
		return r.sftp.Symlink(linkTarget, target)
	}
	return r.Exec(ctx, "ln -s -- "+shellQuote(linkTarget)+" "+shellQuote(target), ExecOptions{})
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
		return r.sftp.Remove(target)
	}
	flag := ""
	if recursive {
		flag = "-r"
	}
	return r.Exec(ctx, "rm "+flag+" -- "+shellQuote(target), ExecOptions{})
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
		return renameSFTP(r.sftp, source, target, overwrite)
	}
	flag := ""
	if !overwrite {
		flag = "-n"
	}
	command := "mv " + flag + " -T -- " + shellQuote(source) + " " + shellQuote(target)
	if !overwrite {
		command += "; result=$?; [ \"$result\" -eq 0 ] || exit \"$result\"; if [ -e " + shellQuote(source) + " ] || [ -L " + shellQuote(source) + " ]; then exit 73; fi"
	}
	return r.Exec(ctx, command, ExecOptions{})
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
	go func() { done <- session.Run(command) }()
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
	loginCommand, cleanupPath, err := r.remoteLoginCommand(ctx, shell)
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
	pty := &sshPTY{session: session, input: input, output: output, remote: r, cleanupPath: cleanupPath}
	go func() {
		<-ctx.Done()
		_ = pty.Close()
	}()
	return pty, nil
}

func (r *Remote) remoteLoginCommand(ctx context.Context, shell string) (string, string, error) {
	name := strings.ToLower(path.Base(shell))
	if name == "fish" {
		return "exec " + shellQuote(shell) + " -l -i -C " + shellQuote(fishCWDHook()), "", nil
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
		if err := write("bashrc", `[ -r "$HOME/.bashrc" ] && . "$HOME/.bashrc"
`+bashCWDHook()); err != nil {
			return fail(err)
		}
		command := "exec " + shellQuote(shell) + " -l -c " + shellQuote(`exec "$0" --noprofile --rcfile "$1" -i`) + " " + shellQuote(shell) + " " + shellQuote(rc)
		return command, directory, nil
	case "zsh":
		home, err := r.Home(ctx)
		if err != nil {
			return fail(err)
		}
		for _, file := range []string{".zshenv", ".zprofile", ".zshrc", ".zlogin", ".zlogout"} {
			body := "[[ -r " + shellQuote(path.Join(home, file)) + " ]] && source " + shellQuote(path.Join(home, file)) + "\nexport ZDOTDIR=" + shellQuote(directory) + "\n"
			if file == ".zlogin" {
				body += zshCWDHook()
			}
			if err := write(file, body); err != nil {
				return fail(err)
			}
		}
		return "ZDOTDIR=" + shellQuote(directory) + " exec " + shellQuote(shell) + " -l -i", directory, nil
	default:
		env := path.Join(directory, "env")
		if err := write("env", posixCWDHook()); err != nil {
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
		if r.chain != nil {
			transportErr = r.chain.Close()
		}
		if r.sftp != nil {
			sftpErr = r.sftp.Close()
		}
		r.closeErr = errors.Join(transportErr, sftpErr)
	})
	return r.closeErr
}

func (r *Remote) listPOSIX(ctx context.Context, directory string) ([]Entry, error) {
	return r.findEntries(ctx, directory, false)
}

func (r *Remote) findEntries(ctx context.Context, target string, exact bool) ([]Entry, error) {
	depth := "-mindepth 1 -maxdepth 1"
	if exact {
		depth = "-maxdepth 0"
	}
	command := "LC_ALL=C find -- " + shellQuote(target) + " " + depth + " -printf '%y\\0%f\\0%s\\0%T@\\0%m\\0%l\\0'"
	var stdout bytes.Buffer
	if err := r.Exec(ctx, command, ExecOptions{Stdout: &stdout}); err != nil {
		return nil, err
	}
	fields := bytes.Split(stdout.Bytes(), []byte{0})
	if len(fields) > 0 && len(fields[len(fields)-1]) == 0 {
		fields = fields[:len(fields)-1]
	}
	if len(fields)%6 != 0 {
		return nil, errors.New("invalid POSIX directory response")
	}
	entries := make([]Entry, 0, len(fields)/6)
	for index := 0; index < len(fields); index += 6 {
		name := string(fields[index+1])
		entryPath := target
		if !exact {
			entryPath = path.Join(target, name)
		}
		size, _ := strconv.ParseInt(string(fields[index+2]), 10, 64)
		seconds, _ := strconv.ParseFloat(string(fields[index+3]), 64)
		permissions, _ := strconv.ParseUint(string(fields[index+4]), 8, 32)
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
		entries = append(entries, Entry{Name: name, Path: entryPath, Mode: mode, Size: size, Modified: time.Unix(0, int64(seconds*1e9)), LinkTarget: string(fields[index+5])})
	}
	sortEntries(entries)
	return entries, nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

type sessionReader struct {
	io.Reader
	session *ssh.Session
	once    sync.Once
}

type sshPTY struct {
	session     *ssh.Session
	input       io.WriteCloser
	output      io.Reader
	remote      *Remote
	cleanupPath string
	once        sync.Once
}

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
	var err error
	r.once.Do(func() { err = errors.Join(r.session.Close(), r.session.Wait()) })
	return err
}

type sftpAtomicWriter struct {
	file              *sftp.File
	client            *sftp.Client
	temporary, target string
	done              bool
}

func (w *sftpAtomicWriter) Write(data []byte) (int, error) { return w.file.Write(data) }
func (w *sftpAtomicWriter) Close() error                   { return w.file.Close() }
func (w *sftpAtomicWriter) Commit() error {
	if w.done {
		return errors.New("atomic writer already completed")
	}
	if _, supported := w.client.HasExtension("fsync@openssh.com"); supported {
		if err := w.file.Sync(); err != nil {
			_ = w.Abort()
			return err
		}
	}
	if err := w.file.Close(); err != nil {
		_ = w.Abort()
		return err
	}
	if err := renameSFTP(w.client, w.temporary, w.target, true); err != nil {
		_ = w.client.Remove(w.temporary)
		w.done = true
		return err
	}
	w.done = true
	return nil
}
func (w *sftpAtomicWriter) Abort() error {
	if w.done {
		return nil
	}
	w.done = true
	return errors.Join(w.file.Close(), w.client.Remove(w.temporary))
}

type sshAtomicWriter struct {
	io.WriteCloser
	session           *ssh.Session
	remote            *Remote
	temporary, target string
	mode              fs.FileMode
	done, closed      bool
}

func (w *sshAtomicWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.WriteCloser.Close()
}
func (w *sshAtomicWriter) Commit() error {
	if w.done {
		return errors.New("atomic writer already completed")
	}
	if err := w.Close(); err != nil {
		_ = w.Abort()
		return err
	}
	if err := w.session.Wait(); err != nil {
		_ = w.Abort()
		return err
	}
	command := fmt.Sprintf("chmod %04o -- %s && sync -f -- %s && mv -f -T -- %s %s", w.mode.Perm(), shellQuote(w.temporary), shellQuote(w.temporary), shellQuote(w.temporary), shellQuote(w.target))
	if err := w.remote.Exec(context.Background(), command, ExecOptions{}); err != nil {
		_ = w.Abort()
		return err
	}
	w.done = true
	return nil
}
func (w *sshAtomicWriter) Abort() error {
	if w.done {
		return nil
	}
	w.done = true
	_ = w.WriteCloser.Close()
	_ = w.session.Close()
	return w.remote.Exec(context.Background(), "rm -f -- "+shellQuote(w.temporary), ExecOptions{})
}
