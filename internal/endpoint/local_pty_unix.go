//go:build !windows

package endpoint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

func (l *Local) OpenPTY(ctx context.Context, directory, shell string, rows, columns uint) (PTYSession, error) {
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	command, temporary, err := localLoginCommand(shell)
	if err != nil {
		return nil, err
	}
	command.Dir = directory
	if len(command.Env) == 0 {
		command.Env = os.Environ()
	}
	command.Env = append(command.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
	file, err := pty.StartWithSize(command, &pty.Winsize{Rows: uint16(rows), Cols: uint16(columns)})
	if err != nil {
		if temporary != "" {
			_ = os.RemoveAll(temporary)
		}
		return nil, err
	}
	session := &localPTY{file: file, command: command, temporary: temporary, done: make(chan struct{})}
	go func() { session.waitErr = command.Wait(); close(session.done) }()
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-session.done:
		}
	}()
	return session, nil
}

// localLoginCommand installs cwd synchronization through each shell's startup
// mechanism. This is materially different from typing a bootstrap into the
// PTY: startup files run after the user's profile/rc, so no command is echoed
// and the user's environment, aliases and prompt are already in place.
func localLoginCommand(shell string) (*exec.Cmd, string, error) {
	name := strings.ToLower(filepath.Base(shell))
	switch name {
	case "zsh":
		directory, err := os.MkdirTemp("", "dragfm-zdot-*")
		if err != nil {
			return nil, "", err
		}
		original := os.Getenv("ZDOTDIR")
		if original == "" {
			original, _ = os.UserHomeDir()
		}
		for _, file := range []string{".zshenv", ".zprofile", ".zshrc", ".zlogin", ".zlogout"} {
			body := fmt.Sprintf("[[ -r %s ]] && source %s\nexport ZDOTDIR=%s\n", posixQuote(filepath.Join(original, file)), posixQuote(filepath.Join(original, file)), posixQuote(directory))
			if file == ".zlogin" {
				body += zshCWDHook()
			}
			if err := os.WriteFile(filepath.Join(directory, file), []byte(body), 0600); err != nil {
				_ = os.RemoveAll(directory)
				return nil, "", err
			}
		}
		command := exec.Command(shell, "-l", "-i")
		command.Env = append(os.Environ(), "ZDOTDIR="+directory)
		return command, directory, nil
	case "bash":
		file, err := os.CreateTemp("", "dragfm-bashrc-*")
		if err != nil {
			return nil, "", err
		}
		path := file.Name()
		body := `[ -r "$HOME/.bashrc" ] && . "$HOME/.bashrc"
` + bashCWDHook()
		if _, err = file.WriteString(body); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return nil, "", err
		}
		if err = file.Close(); err != nil {
			_ = os.Remove(path)
			return nil, "", err
		}
		return exec.Command(shell, "-l", "-c", `exec "$0" --noprofile --rcfile "$1" -i`, shell, path), path, nil
	case "fish":
		return exec.Command(shell, "-l", "-i", "-C", fishCWDHook()), "", nil
	default:
		file, err := os.CreateTemp("", "dragfm-env-*")
		if err != nil {
			return nil, "", err
		}
		path := file.Name()
		if _, err = file.WriteString(posixCWDHook()); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return nil, "", err
		}
		if err = file.Close(); err != nil {
			_ = os.Remove(path)
			return nil, "", err
		}
		command := exec.Command(shell, "-l", "-i")
		command.Env = append(os.Environ(), "ENV="+path)
		return command, path, nil
	}
}

func posixQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

type localPTY struct {
	file      *os.File
	command   *exec.Cmd
	temporary string
	once      sync.Once
	done      chan struct{}
	waitErr   error
	closeErr  error
}

func (p *localPTY) Input() io.WriteCloser { return p.file }
func (p *localPTY) Output() io.Reader     { return p.file }
func (p *localPTY) Resize(rows, columns uint) error {
	return pty.Setsize(p.file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(columns)})
}
func (p *localPTY) Wait() error { <-p.done; return p.waitErr }
func (p *localPTY) Close() error {
	var err error
	p.once.Do(func() {
		err = p.file.Close()
		// pty.Start starts a new session/process group. Terminate that owned
		// group, not unrelated shells, and reap the login-shell process.
		select {
		case <-p.done: // A reaped PID must never be signalled again.
		default:
			_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGHUP)
		}
		select {
		case <-p.done:
		case <-time.After(200 * time.Millisecond):
			_ = syscall.Kill(-p.command.Process.Pid, syscall.SIGKILL)
			select {
			case <-p.done:
			case <-time.After(2 * time.Second):
				err = errors.Join(err, errors.New("PTY did not exit after SIGKILL"))
			}
		}
		var cleanupErr error
		if p.temporary != "" {
			if info, statErr := os.Stat(p.temporary); statErr == nil && info.IsDir() {
				cleanupErr = os.RemoveAll(p.temporary)
			} else if statErr == nil {
				cleanupErr = os.Remove(p.temporary)
			}
		}
		p.closeErr = errors.Join(err, cleanupErr)
	})
	return p.closeErr
}
