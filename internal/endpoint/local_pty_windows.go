//go:build windows

package endpoint

import (
	"context"
	"crypto/rand"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/ActiveState/termtest/conpty"
)

func (l *Local) OpenPTY(ctx context.Context, directory, _ string, rows, columns uint) (PTYSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	shell, err := exec.LookPath("powershell.exe")
	if err != nil {
		return nil, err
	}
	console, err := conpty.New(int16(columns), int16(rows))
	if err != nil {
		return nil, err
	}
	nonce := rand.Text()
	prompt := `$global:__dragfm_original_prompt = $function:prompt; function global:prompt { $e=[char]27; $p=[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($PWD.Path)); Write-Host -NoNewline ("${e}]777;dragfm-cwd=v1;` + nonce + `;" + $p + [char]7); if ($global:__dragfm_original_prompt) { & $global:__dragfm_original_prompt } else { "PS $($PWD.Path)> " } }`
	pid, _, err := console.Spawn(shell, []string{"-NoLogo", "-NoExit", "-Command", prompt}, &syscall.ProcAttr{Env: os.Environ(), Dir: directory})
	if err != nil {
		_ = console.Close()
		return nil, err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		_ = console.Close()
		return nil, err
	}
	session := &windowsPTY{console: console, process: process, nonce: nonce, done: make(chan struct{})}
	go func() { _, session.waitErr = process.Wait(); close(session.done) }()
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-session.done:
		}
	}()
	return session, nil
}

type windowsPTY struct {
	console           *conpty.ConPty
	process           *os.Process
	nonce             string
	done              chan struct{}
	waitErr, closeErr error
	once              sync.Once
}

func (p *windowsPTY) CWDNonce() string      { return p.nonce }
func (p *windowsPTY) Input() io.WriteCloser { return p.console.InPipe() }
func (p *windowsPTY) Output() io.Reader     { return p.console.OutPipe() }
func (p *windowsPTY) Resize(rows, columns uint) error {
	return p.console.Resize(uint16(columns), uint16(rows))
}
func (p *windowsPTY) Wait() error { <-p.done; return p.waitErr }
func (p *windowsPTY) Close() error {
	p.once.Do(func() {
		select {
		case <-p.done:
		default:
			_ = p.process.Kill()
		}
		p.closeErr = p.console.Close()
	})
	return p.closeErr
}
