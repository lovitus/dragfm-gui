import { useEffect, useRef, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { api, onEvent } from '../api'
import type { PaneID } from '../types'

function decodeBase64(value: string): Uint8Array {
  const binary = atob(value)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index)
  return bytes
}

export default function TerminalPane({ pane, path, active, onCWD, onSyncFailure }: { pane: PaneID; path: string; active: boolean; onCWD: (path: string) => void; onSyncFailure?: () => void }) {
  const host = useRef<HTMLDivElement>(null)
  const sessionRef = useRef('')
  const shellReady = useRef(false)
  const [error, setError] = useState('')
  const reportErrorRef = useRef<(reason: unknown) => void>(() => {})
  const initialPath = useRef(path)
  const displayedPath = useRef(path)
  displayedPath.current = path
  const onCWDRef = useRef(onCWD)
  onCWDRef.current = onCWD
  const onSyncFailureRef = useRef(onSyncFailure)
  onSyncFailureRef.current = onSyncFailure
  const activeRef = useRef(active)
  activeRef.current = active

  useEffect(() => {
    if (!host.current) return
    let disposed = false
    let session = ''
    let shellPath: string | undefined
    let cwdSequence = 0
    const terminal = new Terminal({
      allowProposedApi: false,
      convertEol: false,
      cursorBlink: true,
      cursorStyle: 'bar',
      fontFamily: '"SFMono-Regular", "Cascadia Mono", "Liberation Mono", monospace',
      fontSize: 12.5,
      fontWeight: '400',
      lineHeight: 1.16,
      letterSpacing: 0,
      minimumContrastRatio: 4.5,
      scrollback: 10_000,
      theme: {
        background: '#0d1117', foreground: '#d5dae1', cursor: '#80aaff', cursorAccent: '#0d1117',
        selectionBackground: '#264f78', black: '#1b1f24', red: '#ff7b72', green: '#7ee787', yellow: '#d29922',
        blue: '#79c0ff', magenta: '#d2a8ff', cyan: '#56d4dd', white: '#e6edf3', brightBlack: '#6e7681',
        brightRed: '#ffa198', brightGreen: '#aff5b4', brightYellow: '#e3b341', brightBlue: '#a5d6ff',
        brightMagenta: '#d2a8ff', brightCyan: '#a5f3fc', brightWhite: '#f0f6fc',
      },
    })
    // UI errors are not PTY output. Writing them into xterm changes the
    // cursor behind readline/ZLE's back, leaving fragments on the next edit.
    const reportError = (reason: unknown) => { if (!disposed) setError(String(reason)) }
    reportErrorRef.current = reportError
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(host.current)
    const resize = new ResizeObserver(() => {
      requestAnimationFrame(() => {
        if (disposed || !host.current || host.current.clientWidth < 40 || host.current.clientHeight < 30) return
        fit.fit()
        if (session) void api.terminalResize(session, terminal.rows, terminal.cols).catch(reportError)
      })
    })
    resize.observe(host.current)
    const removeData = onEvent('terminal:data', (event) => {
      if (event.session === session) terminal.write(decodeBase64(event.data))
    })
    const removeCWD = onEvent('terminal:cwd', (event) => {
      if (event.session === session && event.pane === pane) {
        if (event.sequence !== undefined) {
          if (event.sequence <= cwdSequence) return
          cwdSequence = event.sequence
        }
        const firstPrompt = shellPath === undefined
        const unchanged = shellPath === event.path
        shellPath = event.path
        shellReady.current = true
        if (firstPrompt && initialPath.current !== path) {
          // Navigation during slow login waits for the actual first prompt;
          // it must never become input to a program started by a profile.
          void api.terminalChangeDirectory(session, initialPath.current).catch((reason) => {
            onSyncFailureRef.current?.()
            reportError(reason)
          })
          return
        }
        // A delayed duplicate prompt from the previous directory is not a
        // shell navigation. It must not undo a newer file-pane navigation
        // while the PTY is acknowledging the requested cd.
        if (unchanged && event.path !== displayedPath.current) return
        // Avoid echoing a genuine shell-originated cd back into the PTY.
        initialPath.current = event.path
        onCWDRef.current(event.path)
      }
    })
    const input = terminal.onData((data) => {
      if (session) {
        setError('')
        void api.terminalInput(session, data).catch(reportError)
      }
    })
    const startFrame = requestAnimationFrame(() => {
      if (disposed) return
      fit.fit()
      api.startTerminal(pane, path, terminal.rows, terminal.cols).then(async (id) => {
        if (disposed) await api.closeTerminal(id)
        else {
          session = id
          sessionRef.current = id
          await api.terminalReady(id)
          if (disposed) return
          if (activeRef.current) terminal.focus()
        }
      }).catch(reportError)
    })
    return () => {
      disposed = true
      cancelAnimationFrame(startFrame)
      if (session) void api.closeTerminal(session).catch(() => {})
      sessionRef.current = ''
      shellReady.current = false
      input.dispose()
      removeData()
      removeCWD()
      resize.disconnect()
      terminal.dispose()
    }
    // A pane keeps one PTY while navigation is synchronized through cd/cwd;
    // endpoint changes remount the component via its key in FilePane.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pane])

  useEffect(() => {
    if (path === initialPath.current) return
    initialPath.current = path
    if (sessionRef.current && shellReady.current) void api.terminalChangeDirectory(sessionRef.current, path).catch((reason) => {
      // A refused cd (editing/running program) is not an acknowledgement.
      // Release the navigation gate so the next genuine shell prompt can
      // reconcile the pane; never silently queue keystrokes into that program.
      onSyncFailureRef.current?.()
      reportErrorRef.current(reason)
    })
  }, [path])

  return <div className="terminal-view">
    <div className="terminal-host" ref={host} data-testid={`terminal-${pane}`} />
    {error && <div className="terminal-error" role="status">{error}</div>}
  </div>
}
