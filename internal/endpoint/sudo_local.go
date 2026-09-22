package endpoint

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lovitus/dragfm-gui/internal/boundedbuf"
)

// SudoLocal uses the ordinary endpoint where possible and narrowly scoped
// filesystem subprocesses for explicitly approved protected paths. Credentials are supplied over stdin and
// never appear in argv, the environment, or a temporary file.
type SudoLocal struct {
	local    *Local
	password string
	owner    string
}

func NewSudoLocal(password string) *SudoLocal {
	owner := ""
	if current, err := user.Current(); err == nil && current.Uid != "" && current.Gid != "" {
		owner = current.Uid + ":" + current.Gid
	}
	return &SudoLocal{local: NewLocal(), password: password, owner: owner}
}

func (s *SudoLocal) Check(ctx context.Context) error {
	if runtime.GOOS == "windows" {
		return errors.New("Windows 控制机不支持 sudo")
	}
	return s.run(ctx, "true")
}

func (s *SudoLocal) Identity(ctx context.Context) (Identity, error) { return s.local.Identity(ctx) }
func (s *SudoLocal) Home(ctx context.Context) (string, error)       { return s.local.Home(ctx) }
func (s *SudoLocal) Abs(ctx context.Context, value string) (string, error) {
	return s.local.Abs(ctx, value)
}
func (s *SudoLocal) Join(parts ...string) string { return s.local.Join(parts...) }
func (s *SudoLocal) Dir(value string) string     { return s.local.Dir(value) }
func (s *SudoLocal) List(ctx context.Context, directory string) ([]Entry, error) {
	entries, err := s.local.List(ctx, directory)
	if errors.Is(err, fs.ErrPermission) {
		err = s.childResult(ctx, "list", directory, &entries)
	}
	return entries, err
}
func (s *SudoLocal) Stat(ctx context.Context, path string) (Entry, error) {
	entry, err := s.local.Stat(ctx, path)
	if errors.Is(err, fs.ErrPermission) {
		err = s.childResult(ctx, "stat", path, &entry)
	}
	return entry, err
}
func (s *SudoLocal) Readlink(ctx context.Context, path string) (string, error) {
	target, err := s.local.Readlink(ctx, path)
	if errors.Is(err, fs.ErrPermission) {
		err = s.childResult(ctx, "readlink", path, &target)
	}
	return target, err
}

func (s *SudoLocal) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	reader, err := s.local.Open(ctx, path)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return reader, err
	}
	command, stdin, stderr, marker, err := s.childCommand(ctx, "read", path)
	if err != nil {
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	if err := s.writePrelude(stdin, marker); err != nil {
		stdin.Close()
		command.Process.Kill()
		command.Wait()
		return nil, err
	}
	_ = stdin.Close()
	return &sudoReadCloser{ReadCloser: stdout, command: command, stderr: stderr}, nil
}

func (s *SudoLocal) CreateAtomic(ctx context.Context, target string, mode fs.FileMode) (AtomicWriter, error) {
	if err := rejectFilesystemRoot(target); err != nil {
		return nil, err
	}
	partial := filepath.Join(filepath.Dir(target), ".dragfm-partial-"+randomSuffix())
	command, stdin, stderr, marker, err := s.childCommand(ctx, "write", partial)
	if err != nil {
		return nil, err
	}
	command.Stdout = io.Discard
	if err := command.Start(); err != nil {
		stdin.Close()
		return nil, err
	}
	if err := s.writePrelude(stdin, marker); err != nil {
		stdin.Close()
		command.Process.Kill()
		command.Wait()
		return nil, err
	}
	return &sudoAtomicWriter{endpoint: s, ctx: ctx, command: command, stdin: stdin, stderr: stderr, partial: partial, target: target, mode: mode.Perm()}, nil
}

func (s *SudoLocal) MkdirAll(ctx context.Context, target string, mode fs.FileMode) error {
	if info, err := s.Stat(ctx, target); err == nil {
		if !info.IsDir() {
			return errors.New("destination is not a real directory")
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := s.run(ctx, "mkdir", "-p", "-m", fmt.Sprintf("%04o", mode.Perm()), target); err != nil {
		return err
	}
	return s.takeOwnership(ctx, target)
}

func (s *SudoLocal) Symlink(ctx context.Context, target, path string) error {
	if strings.HasPrefix(target, "-") {
		return errors.New("管理员模式暂不支持以连字符开头的相对符号链接目标")
	}
	return s.run(ctx, "ln", "-s", target, path)
}

func (s *SudoLocal) Chmod(ctx context.Context, path string, mode fs.FileMode) error {
	if err := s.local.Chmod(ctx, path, mode); err == nil {
		return nil
	}
	return s.run(ctx, "chmod", fmt.Sprintf("%04o", mode.Perm()), path)
}

func (s *SudoLocal) Chtimes(ctx context.Context, path string, atime, mtime time.Time) error {
	if err := s.local.Chtimes(ctx, path, atime, mtime); err == nil {
		return nil
	}
	return s.childResult(ctx, "times", path, nil, strconv.FormatInt(atime.UnixNano(), 10), strconv.FormatInt(mtime.UnixNano(), 10))
}

func (s *SudoLocal) Remove(ctx context.Context, path string, recursive bool) error {
	if err := rejectFilesystemRoot(path); err != nil {
		return err
	}
	if err := s.local.Remove(ctx, path, recursive); err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if recursive {
		return s.run(ctx, "rm", "-rf", path)
	}
	return s.run(ctx, "rm", "-f", path)
}

func (s *SudoLocal) Rename(ctx context.Context, source, target string, overwrite bool) error {
	if err := rejectFilesystemRoot(source); err != nil {
		return err
	}
	if err := rejectFilesystemRoot(target); err != nil {
		return err
	}
	if err := s.local.Rename(ctx, source, target, overwrite); err == nil || !errors.Is(err, fs.ErrPermission) {
		return err
	}
	return s.childResult(ctx, "rename", source, nil, target, strconv.FormatBool(overwrite))
}

func (s *SudoLocal) Exec(ctx context.Context, command string, options ExecOptions) error {
	return s.local.Exec(ctx, command, options)
}
func (s *SudoLocal) Close() error { return nil }

func (s *SudoLocal) takeOwnership(ctx context.Context, path string) error {
	if s.owner == "" {
		return nil
	}
	return s.run(ctx, "chown", s.owner, path)
}

func (s *SudoLocal) run(ctx context.Context, program string, args ...string) error {
	command, stderr := s.newCommand(ctx, program, args...)
	command.Stdout = io.Discard
	if s.password != "" {
		// NOPASSWD/cached sudo may finish without reading a supplied password.
		// Let os/exec own the stdin copy and process wait: its pipe-copy handling
		// tolerates an unused pipe only while retaining the real exit status.
		// A synchronous password Write followed by an early return on EPIPE
		// incorrectly reported successful chmod/chown operations as failures.
		command.Stdin = strings.NewReader(s.password + "\n")
	}
	err := command.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("sudo %s 失败: %s", program, message)
	}
	return nil
}

func (s *SudoLocal) newCommand(ctx context.Context, program string, args ...string) (*exec.Cmd, *boundedbuf.Buffer) {
	sudoArgs := []string{"-n", "--", program}
	if s.password != "" {
		sudoArgs = []string{"-S", "-p", "", "--", program}
	}
	sudoArgs = append(sudoArgs, args...)
	command := exec.CommandContext(ctx, "sudo", sudoArgs...)
	command.WaitDelay = 2 * time.Second
	stderr := &boundedbuf.Buffer{}
	command.Stderr = stderr
	return command, stderr
}

func (s *SudoLocal) command(ctx context.Context, program string, args ...string) (*exec.Cmd, io.WriteCloser, *boundedbuf.Buffer, error) {
	command, stderr := s.newCommand(ctx, program, args...)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	// Framed filesystem reads/writes still use their explicit stream. This
	// is deliberately separate from the credential-only non-streaming run.
	return command, stdin, stderr, nil
}

type sudoReadCloser struct {
	io.ReadCloser
	command *exec.Cmd
	stderr  *boundedbuf.Buffer
}

func (r *sudoReadCloser) Close() error {
	readErr := r.ReadCloser.Close()
	waitErr := r.command.Wait()
	if waitErr != nil {
		return errors.Join(readErr, fmt.Errorf("sudo cat 失败: %s", strings.TrimSpace(r.stderr.String())))
	}
	return readErr
}

type sudoAtomicWriter struct {
	endpoint *SudoLocal
	ctx      context.Context
	command  *exec.Cmd
	stdin    io.WriteCloser
	stderr   *boundedbuf.Buffer
	partial  string
	target   string
	mode     fs.FileMode
	done     bool
}

func (w *sudoAtomicWriter) Write(data []byte) (int, error) {
	if w.done {
		return 0, fs.ErrClosed
	}
	return w.stdin.Write(data)
}

func (w *sudoAtomicWriter) Close() error {
	if w.done {
		return nil
	}
	return w.stdin.Close()
}

func (w *sudoAtomicWriter) Commit() error {
	if w.done {
		return errors.New("atomic writer already completed")
	}
	w.done = true
	closeErr := w.stdin.Close()
	waitErr := w.command.Wait()
	if closeErr != nil || waitErr != nil {
		_ = w.cleanup()
		message := strings.TrimSpace(w.stderr.String())
		if message != "" {
			waitErr = fmt.Errorf("sudo tee 失败: %s", message)
		}
		return errors.Join(closeErr, waitErr)
	}
	if err := w.endpoint.run(w.ctx, "chmod", fmt.Sprintf("%04o", w.mode.Perm()), w.partial); err != nil {
		_ = w.cleanup()
		return err
	}
	if err := w.endpoint.takeOwnership(w.ctx, w.partial); err != nil {
		_ = w.cleanup()
		return err
	}
	if err := w.endpoint.Rename(w.ctx, w.partial, w.target, true); err != nil {
		_ = w.cleanup()
		return err
	}
	return nil
}

func (w *sudoAtomicWriter) Abort() error {
	if w.done {
		return nil
	}
	w.done = true
	closeErr := w.stdin.Close()
	waitErr := w.command.Wait()
	removeErr := w.cleanup()
	return errors.Join(closeErr, waitErr, removeErr)
}

func rejectFilesystemRoot(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	root := string(filepath.Separator)
	if volume != "" {
		root = volume + string(filepath.Separator)
	}
	if clean == root {
		return errors.New("拒绝以管理员权限修改文件系统根目录")
	}
	return nil
}

func (w *sudoAtomicWriter) cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return w.endpoint.Remove(ctx, w.partial, false)
}
func (s *SudoLocal) childCommand(ctx context.Context, op, path string, args ...string) (*exec.Cmd, io.WriteCloser, *boundedbuf.Buffer, string, error) {
	if !filepath.IsAbs(path) {
		return nil, nil, nil, "", errors.New("filesystem path must be absolute")
	}
	if strings.ContainsAny(s.password, "\r\n") || len(s.password) > 4096 {
		return nil, nil, nil, "", errors.New("invalid sudo password framing")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, nil, "", err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nil, nil, "", err
	}
	marker := fmt.Sprintf("%x", nonce[:]) // A frame delimiter, not a credential.
	argv := append([]string{FilesystemChildFlag, op, marker, path}, args...)
	command, stdin, stderr, err := s.command(ctx, executable, argv...)
	return command, stdin, stderr, marker, err
}
func (s *SudoLocal) writePrelude(writer io.Writer, marker string) error {
	if s.password != "" {
		if _, err := io.WriteString(writer, s.password+"\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "dragfm-fs-v1:"+marker+"\n")
	return err
}
func (s *SudoLocal) childResult(ctx context.Context, op, path string, result any, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command, stdin, stderr, marker, err := s.childCommand(ctx, op, path, args...)
	if err != nil {
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		stdin.Close()
		return err
	}
	if err = command.Start(); err != nil {
		stdin.Close()
		return err
	}
	if err = s.writePrelude(stdin, marker); err != nil {
		stdin.Close()
		command.Process.Kill()
		command.Wait()
		return err
	}
	stdin.Close()
	// Directory listings are bounded independently of adversarial diagnostic output.
	data, readErr := io.ReadAll(io.LimitReader(stdout, (16<<20)+1))
	if len(data) > 16<<20 {
		command.Process.Kill()
		readErr = errors.New("filesystem response exceeds 16 MiB")
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			switch exit.ExitCode() {
			case 73:
				return fs.ErrExist
			case 66:
				return fs.ErrNotExist
			case 77:
				return fs.ErrPermission
			}
		}
		return fmt.Errorf("sudo filesystem %s: %w: %s", op, waitErr, stderr.String())
	}
	if readErr != nil {
		return readErr
	}
	if result != nil {
		return json.Unmarshal(data, result)
	}
	return nil
}
