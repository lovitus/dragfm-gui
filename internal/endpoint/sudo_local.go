package endpoint

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SudoLocal delegates reads to the ordinary local endpoint and performs only
// filesystem mutations through sudo. Credentials are supplied over stdin and
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
	return s.local.List(ctx, directory)
}
func (s *SudoLocal) Stat(ctx context.Context, path string) (Entry, error) {
	return s.local.Stat(ctx, path)
}
func (s *SudoLocal) Readlink(ctx context.Context, path string) (string, error) {
	return s.local.Readlink(ctx, path)
}

func (s *SudoLocal) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	reader, err := s.local.Open(ctx, path)
	if err == nil || !errors.Is(err, fs.ErrPermission) {
		return reader, err
	}
	command, stdin, stderr, err := s.command(ctx, "cat", path)
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
	if s.password != "" {
		if _, err := io.WriteString(stdin, s.password+"\n"); err != nil {
			_ = stdin.Close()
			_ = command.Wait()
			return nil, err
		}
	}
	_ = stdin.Close()
	return &sudoReadCloser{ReadCloser: stdout, command: command, stderr: stderr}, nil
}

func (s *SudoLocal) CreateAtomic(ctx context.Context, target string, mode fs.FileMode) (AtomicWriter, error) {
	if err := rejectFilesystemRoot(target); err != nil {
		return nil, err
	}
	partial := filepath.Join(filepath.Dir(target), ".dragfm-partial-"+randomSuffix())
	// Keep the credential stream separate from file content. If sudo already has
	// a cached ticket it may not consume stdin; sharing stdin with tee would then
	// prepend the password to the destination file.
	sudoArgs := []string{"-n", "--", "sh", "-c", `cat <&3 > "$1"`, "dragfm-sudo-writer", partial}
	if s.password != "" {
		sudoArgs = []string{"-S", "-p", "", "--", "sh", "-c", `cat <&3 > "$1"`, "dragfm-sudo-writer", partial}
	}
	command := exec.CommandContext(ctx, "sudo", sudoArgs...)
	credentialInput, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	dataReader, dataWriter, err := os.Pipe()
	if err != nil {
		_ = credentialInput.Close()
		return nil, err
	}
	command.ExtraFiles = []*os.File{dataReader}
	command.Stdout = io.Discard
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		_ = credentialInput.Close()
		_ = dataReader.Close()
		_ = dataWriter.Close()
		return nil, err
	}
	_ = dataReader.Close()
	if s.password != "" {
		if _, err := io.WriteString(credentialInput, s.password+"\n"); err != nil {
			_ = credentialInput.Close()
			_ = dataWriter.Close()
			_ = command.Wait()
			return nil, err
		}
	}
	_ = credentialInput.Close()
	return &sudoAtomicWriter{endpoint: s, ctx: ctx, command: command, stdin: dataWriter, stderr: stderr, partial: partial, target: target, mode: mode.Perm()}, nil
}

func (s *SudoLocal) MkdirAll(ctx context.Context, target string, mode fs.FileMode) error {
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return nil
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
	return s.run(ctx, "touch", "-t", mtime.Format("200601021504.05"), path)
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
	if err := s.local.Rename(ctx, source, target, overwrite); err == nil {
		return nil
	}
	if !overwrite {
		if _, err := os.Lstat(target); err == nil {
			return fs.ErrExist
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return s.run(ctx, "mv", "-f", source, target)
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
	command, stdin, stderr, err := s.command(ctx, program, args...)
	if err != nil {
		return err
	}
	command.Stdout = io.Discard
	if err := command.Start(); err != nil {
		return err
	}
	if s.password != "" {
		if _, err := io.WriteString(stdin, s.password+"\n"); err != nil {
			_ = stdin.Close()
			_ = command.Wait()
			return err
		}
	}
	_ = stdin.Close()
	if err := command.Wait(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("sudo %s 失败: %s", program, message)
	}
	return nil
}

func (s *SudoLocal) command(ctx context.Context, program string, args ...string) (*exec.Cmd, io.WriteCloser, *bytes.Buffer, error) {
	sudoArgs := []string{"-n", "--", program}
	if s.password != "" {
		sudoArgs = []string{"-S", "-p", "", "--", program}
	}
	sudoArgs = append(sudoArgs, args...)
	command := exec.CommandContext(ctx, "sudo", sudoArgs...)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	return command, stdin, stderr, nil
}

type sudoReadCloser struct {
	io.ReadCloser
	command *exec.Cmd
	stderr  *bytes.Buffer
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
	stderr   *bytes.Buffer
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
		_ = w.endpoint.run(context.Background(), "rm", "-f", w.partial)
		message := strings.TrimSpace(w.stderr.String())
		if message != "" {
			waitErr = fmt.Errorf("sudo tee 失败: %s", message)
		}
		return errors.Join(closeErr, waitErr)
	}
	if err := w.endpoint.run(w.ctx, "chmod", fmt.Sprintf("%04o", w.mode.Perm()), w.partial); err != nil {
		_ = w.endpoint.run(context.Background(), "rm", "-f", w.partial)
		return err
	}
	if err := w.endpoint.takeOwnership(w.ctx, w.partial); err != nil {
		_ = w.endpoint.run(context.Background(), "rm", "-f", w.partial)
		return err
	}
	if err := w.endpoint.run(w.ctx, "mv", "-f", w.partial, w.target); err != nil {
		_ = w.endpoint.run(context.Background(), "rm", "-f", w.partial)
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
	removeErr := w.endpoint.run(context.Background(), "rm", "-f", w.partial)
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
