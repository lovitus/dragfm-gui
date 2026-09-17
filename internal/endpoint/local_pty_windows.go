//go:build windows

package endpoint

import (
	"context"
	"io"
	"os"
	"sync"
	"syscall"

	"github.com/ActiveState/termtest/conpty"
)

func (l *Local) OpenPTY(ctx context.Context, directory, _ string, rows, columns uint) (PTYSession, error) {
	console, err := conpty.New(int16(columns), int16(rows))
	if err != nil {
		return nil, err
	}
	prompt := `function global:prompt { $e=[char]27; Write-Host -NoNewline ("${e}]777;dragfm-cwd=" + $PWD.Path + [char]7); "PS $($PWD.Path)> " }`
	pid, _, err := console.Spawn(`C:\WINDOWS\System32\WindowsPowerShell\v1.0\powershell.exe`, []string{"-NoLogo", "-NoExit", "-Command", prompt}, &syscall.ProcAttr{Env: os.Environ(), Dir: directory})
	if err != nil {
		_ = console.Close()
		return nil, err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		_ = console.Close()
		return nil, err
	}
	session := &windowsPTY{console: console, process: process, wait: make(chan error, 1)}
	go func() {
		_, err := process.Wait()
		session.wait <- err
		close(session.wait)
	}()
	go func() {
		<-ctx.Done()
		_ = session.Close()
	}()
	return session, nil
}

type windowsPTY struct {
	console *conpty.ConPty
	process *os.Process
	wait    chan error
	once    sync.Once
}

func (p *windowsPTY) Input() io.WriteCloser { return p.console.InPipe() }
func (p *windowsPTY) Output() io.Reader     { return p.console.OutPipe() }
func (p *windowsPTY) Resize(rows, columns uint) error {
	return p.console.Resize(uint16(columns), uint16(rows))
}
func (p *windowsPTY) Wait() error { return <-p.wait }
func (p *windowsPTY) Close() error {
	var err error
	p.once.Do(func() {
		_ = p.process.Kill()
		err = p.console.Close()
	})
	return err
}
