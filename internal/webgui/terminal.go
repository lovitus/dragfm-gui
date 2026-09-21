package webgui

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

func (a *App) StartTerminal(paneID PaneID, directory string, rows, columns int) (string, error) {
	ctx, cancel, generation, err := a.activeContext(context.Background())
	if err != nil {
		return "", err
	}
	pane, err := a.pane(paneID)
	if err != nil {
		cancel()
		return "", err
	}
	provider, ok := pane.endpoint.(endpoint.PTYProvider)
	if !ok {
		cancel()
		return "", errors.New("当前端点不支持 PTY")
	}
	if rows < 2 {
		rows = 24
	}
	if columns < 2 {
		columns = 80
	}
	if directory == "" {
		directory = pane.path
	}
	pty, err := provider.OpenPTY(ctx, directory, a.endpointShell(pane.name), uint(rows), uint(columns))
	if err != nil {
		cancel()
		return "", err
	}
	session := &terminalSession{id: a.nextID("terminal"), pane: paneID, pty: pty, cancel: cancel, ctx: ctx, endpoint: pane.endpoint}
	a.mu.Lock()
	current := a.panes[paneID]
	if a.store == nil || a.locking || generation != a.generation || current == nil || current.endpoint != pane.endpoint {
		a.mu.Unlock()
		cancel()
		_ = pty.Close()
		return "", context.Canceled
	}
	var previousSessions []*terminalSession
	for id, previous := range a.terminals {
		if previous.pane == paneID {
			delete(a.terminals, id)
			previousSessions = append(previousSessions, previous)
		}
	}
	a.terminals[session.id] = session
	a.mu.Unlock()
	for _, previous := range previousSessions {
		previous.cancel()
		_ = previous.pty.Close()
	}
	// Do not emit the initial prompt before the frontend has received this ID
	// and installed its session-specific event listener.
	return session.id, nil
}

func (a *App) TerminalReady(sessionID string) error {
	session, err := a.terminal(sessionID)
	if err != nil {
		return err
	}
	session.startOnce.Do(func() { go a.pumpTerminal(session) })
	return nil
}

func (a *App) endpointShell(name string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if host, ok := a.document.HostByName(name); ok {
		return host.Shell
	}
	return ""
}

func (a *App) TerminalInput(sessionID, data string) error {
	session, err := a.terminal(sessionID)
	if err != nil {
		return err
	}
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	if data != "" {
		session.busy.Store(true)
	}
	_, err = io.WriteString(session.pty.Input(), data)
	return err
}

func (a *App) TerminalResize(sessionID string, rows, columns int) error {
	session, err := a.terminal(sessionID)
	if err != nil {
		return err
	}
	if rows < 2 || columns < 2 {
		return nil
	}
	return session.pty.Resize(uint(rows), uint(columns))
}

func (a *App) TerminalChangeDirectory(sessionID, directory string) error {
	session, err := a.terminal(sessionID)
	if err != nil {
		return err
	}
	abs, err := session.endpoint.Abs(session.ctx, directory)
	if err != nil {
		return err
	}
	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	if !session.busy.CompareAndSwap(false, true) {
		return errors.New("终端正在编辑输入或执行命令；未向程序注入 cd，请先返回 Shell 提示符")
	}
	_, err = io.WriteString(session.pty.Input(), terminalCDCommand(abs))
	return err
}

func terminalCDCommand(directory string) string {
	// The final erase-line sequence removes the program-generated command from
	// xterm's visible buffer before the shell draws its new prompt. User-entered
	// commands and scrollback are not cleared.
	return "cd -- " + shellQuote(directory) + "; printf '\\033[2K\\r'\n"
}

func (a *App) CloseTerminal(sessionID string) {
	a.mu.Lock()
	session := a.terminals[sessionID]
	delete(a.terminals, sessionID)
	a.mu.Unlock()
	if session != nil {
		session.cancel()
		_ = session.pty.Close()
	}
}

func (a *App) terminal(sessionID string) (*terminalSession, error) {
	a.mu.RLock()
	session := a.terminals[sessionID]
	a.mu.RUnlock()
	if session == nil {
		return nil, errors.New("PTY 会话已经关闭")
	}
	return session, nil
}

func (a *App) pumpTerminal(session *terminalSession) {
	defer a.finishTerminal(session.id)
	filtered := filterCWDMarkers(session.pty.Output(), func(directory string) {
		session.busy.Store(false)
		a.mu.Lock()
		pane := a.panes[session.pane]
		if a.store == nil || a.locking || a.terminals[session.id] != session || pane == nil || pane.endpoint != session.endpoint {
			a.mu.Unlock()
			return
		}
		if pane != nil {
			pane.path = directory
		}
		if session.pane == LeftPane {
			a.document.UI.LeftPath = directory
		} else {
			a.document.UI.RightPath = directory
		}
		a.mu.Unlock()
		a.requestSave()
		a.emit("terminal:cwd", terminalCWDModel{Session: session.id, Pane: session.pane, Path: directory, Sequence: session.cwdSequence.Add(1)})
	})
	if closer, ok := filtered.(io.Closer); ok {
		defer closer.Close()
	}
	chunks := make(chan []byte, 16)
	go func() {
		defer close(chunks)
		buffer := make([]byte, 32*1024)
		for {
			count, err := filtered.Read(buffer)
			if count > 0 {
				select {
				case chunks <- append([]byte(nil), buffer[:count]...):
				case <-session.ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()
	var pending []byte
	flush := func() {
		if len(pending) == 0 {
			return
		}
		a.emit("terminal:data", terminalDataModel{Session: session.id, Pane: session.pane, Data: base64.StdEncoding.EncodeToString(pending)})
		pending = pending[:0]
	}
	for {
		select {
		case <-session.ctx.Done():
			return
		case chunk, ok := <-chunks:
			if !ok {
				flush()
				session.cancel()
				_ = session.pty.Close()
				_ = session.pty.Wait()
				return
			}
			pending = append(pending, chunk...)
			if len(pending) >= 64*1024 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (a *App) finishTerminal(sessionID string) {
	a.mu.Lock()
	session := a.terminals[sessionID]
	delete(a.terminals, sessionID)
	a.mu.Unlock()
	if session != nil {
		session.cancel()
		_ = session.pty.Close()
	}
}

func filterCWDMarkers(source io.Reader, update func(string)) io.Reader {
	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		prefix := []byte("\x1b]777;dragfm-cwd=")
		buffer := make([]byte, 32*1024)
		pending := make([]byte, 0, 32*1024)
		for {
			count, err := source.Read(buffer)
			if count > 0 {
				pending = append(pending, buffer[:count]...)
				for {
					start := bytes.Index(pending, prefix)
					if start < 0 {
						keep := matchingPrefixSuffix(pending, prefix)
						if len(pending) > keep {
							if _, writeErr := writer.Write(pending[:len(pending)-keep]); writeErr != nil {
								return
							}
							pending = append([]byte(nil), pending[len(pending)-keep:]...)
						}
						break
					}
					end := bytes.IndexByte(pending[start+len(prefix):], 7)
					if end < 0 {
						if len(pending) > 64*1024 {
							if _, writeErr := writer.Write(pending); writeErr != nil {
								return
							}
							pending = pending[:0]
							break
						}
						if start > 0 {
							if _, writeErr := writer.Write(pending[:start]); writeErr != nil {
								return
							}
							pending = append([]byte(nil), pending[start:]...)
						}
						break
					}
					end += start + len(prefix)
					if _, writeErr := writer.Write(pending[:start]); writeErr != nil {
						return
					}
					update(string(pending[start+len(prefix) : end]))
					pending = append([]byte(nil), pending[end+1:]...)
				}
			}
			if err != nil {
				if len(pending) > 0 {
					if _, writeErr := writer.Write(pending); writeErr != nil {
						return
					}
				}
				if !errors.Is(err, io.EOF) {
					_ = writer.CloseWithError(err)
				}
				return
			}
		}
	}()
	return reader
}

// matchingPrefixSuffix returns only the suffix that can genuinely be the
// beginning of a split marker. Keeping len(prefix)-1 bytes unconditionally
// delays the tail of every shell prompt until the next PTY write.
func matchingPrefixSuffix(data, prefix []byte) int {
	maximum := len(prefix) - 1
	if len(data) < maximum {
		maximum = len(data)
	}
	for size := maximum; size > 0; size-- {
		if bytes.Equal(data[len(data)-size:], prefix[:size]) {
			return size
		}
	}
	return 0
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
