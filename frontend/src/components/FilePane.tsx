import { useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import type { DirectoryListing, FileEntry, PaneID } from '../types'
import Icon from './Icon'
import TerminalPane from './TerminalPane'

export interface PaneModel {
  listing: DirectoryListing
  loading: boolean
  error: string
  selected?: FileEntry
}

export interface FilePaneProps {
  pane: PaneID
  model: PaneModel
  hosts: string[]
  active: boolean
  dropTarget: { pane: PaneID; directory: string } | null
  onFocus: () => void
  onNavigate: (path: string, fromTerminal?: boolean) => void
  onEndpoint: (endpoint: string) => void
  onRefresh: () => void
  onSyncFailure?: () => void
  onSelect: (entry?: FileEntry) => void
  onBeginDrag: (event: ReactPointerEvent, pane: PaneID, entry: FileEntry) => void
  onDelete: () => void
  onHash: () => void
}

const ROW_HEIGHT = 30

export function isEditingTarget(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest('input, textarea, select, [contenteditable="true"], .terminal-host, .cm-editor') !== null
}

export function parentPath(value: string): string {
  const windowsRoot = value.match(/^([A-Za-z]:)[\\/]*$/)
  if (windowsRoot) return `${windowsRoot[1]}\\`
  if (/^\\\\[^\\]+\\[^\\]+\\?$/.test(value)) return value.replace(/\\?$/, '\\')
  if (value === '/') return '/'
  const trimmed = value.replace(/[\\/]+$/, '')
  const separator = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'))
  if (separator < 0) return '.'
  if (separator === 0) return trimmed[0]
  if (separator === 2 && /^[A-Za-z]:/.test(trimmed)) return `${trimmed.slice(0, 2)}\\`
  return trimmed.slice(0, separator)
}

function formatSize(value: number, directory: boolean): string {
  if (directory) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let size = value, unit = 0
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++ }
  return `${unit === 0 ? size.toFixed(0) : size < 10 ? size.toFixed(1) : size.toFixed(0)} ${units[unit]}`
}

function formatDate(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function VirtualFileList({ pane, entries, selected, dropTarget, onSelect, onFocus, onOpen, onBeginDrag }: {
  pane: PaneID
  entries: FileEntry[]
  selected?: FileEntry
  dropTarget: string
  onSelect: (entry?: FileEntry) => void
  onFocus: () => void
  onOpen: (entry: FileEntry) => void
  onBeginDrag: (event: ReactPointerEvent, pane: PaneID, entry: FileEntry) => void
}) {
  const viewport = useRef<HTMLDivElement>(null)
  const [scrollTop, setScrollTop] = useState(0)
  const [height, setHeight] = useState(300)
  useEffect(() => {
    if (!viewport.current) return
    const observer = new ResizeObserver(([entry]) => setHeight(entry.contentRect.height))
    observer.observe(viewport.current)
    return () => observer.disconnect()
  }, [])
  const range = useMemo(() => {
    const maximum = Math.max(0, entries.length * ROW_HEIGHT - height)
    const top = Math.min(scrollTop, maximum)
    const start = Math.max(0, Math.floor(top / ROW_HEIGHT) - 8)
    const end = Math.min(entries.length, Math.ceil((top + height) / ROW_HEIGHT) + 8)
    return { start, end }
  }, [entries.length, height, scrollTop])
  return (
    <div className="file-viewport" ref={viewport} tabIndex={0} onFocus={onFocus} onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)} data-drop-root data-pane={pane} onPointerDown={(event) => { if (event.target === event.currentTarget || (event.target as HTMLElement).classList.contains('file-spacer')) onSelect() }}>
      <div className="file-spacer" style={{ height: entries.length * ROW_HEIGHT }}>
        {entries.slice(range.start, range.end).map((entry, offset) => {
          const index = range.start + offset
          const selectedRow = selected?.path === entry.path
          return (
            <div className={`file-row${selectedRow ? ' selected' : ''}${dropTarget === entry.path ? ' drop-target' : ''}`}
              style={{ transform: `translateY(${index * ROW_HEIGHT}px)` }} key={entry.path} role="row" tabIndex={selectedRow ? 0 : -1}
              data-file-path={entry.path} data-drop-directory={entry.directory ? entry.path : undefined} data-pane={pane}
              onClick={(event) => { event.currentTarget.focus(); onSelect(entry) }} onDoubleClick={() => onOpen(entry)}
              onPointerDown={(event) => {
                if (event.button === 0) { event.preventDefault(); event.currentTarget.focus(); onSelect(entry) }
                onBeginDrag(event, pane, entry)
              }}>
              <div className="file-name" title={entry.name}><Icon name={entry.symlink ? 'link' : entry.directory ? 'folder' : 'file'} /><span>{entry.name}</span></div>
              <div className="file-size">{formatSize(entry.size, entry.directory)}</div>
              <div className="file-modified">{formatDate(entry.modified)}</div>
              <div className="file-mode">{entry.mode}</div>
            </div>
          )
        })}
      </div>
      {entries.length === 0 && <div className="empty-state">此目录为空</div>}
    </div>
  )
}

export default function FilePane(props: FilePaneProps) {
  const { pane, model, hosts, active, dropTarget } = props
  const [path, setPath] = useState(model.listing.path)
  const [terminalHeight, setTerminalHeight] = useState(180)
  const [terminalEndpoint, setTerminalEndpoint] = useState<string | null>(null)
  const body = useRef<HTMLElement>(null)
  const resizeCleanup = useRef<(() => void) | null>(null)
  useEffect(() => () => resizeCleanup.current?.(), [])
  useEffect(() => setPath(model.listing.path), [model.listing.path])
  useEffect(() => {
    // Saved labels are not connected endpoints until the initial listing resolves.
    // Once connected, keep the PTY mounted during same-endpoint refreshes.
    if (!model.loading && !model.error) setTerminalEndpoint(model.listing.endpoint)
  }, [model.loading, model.error, model.listing.endpoint])
  const resizeTerminal = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault()
    resizeCleanup.current?.()
    const startY = event.clientY, startHeight = terminalHeight
    const move = (current: PointerEvent) => {
      const maximum = Math.max(120, (body.current?.clientHeight || 500) - 190)
      setTerminalHeight(Math.min(maximum, Math.max(110, startHeight + startY - current.clientY)))
    }
    const stop = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', stop); window.removeEventListener('pointercancel', stop); resizeCleanup.current = null }
    resizeCleanup.current = stop
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop, { once: true })
    window.addEventListener('pointercancel', stop, { once: true })
  }
  return (
    <section className={`file-pane${active ? ' active' : ''}${dropTarget?.pane === pane && dropTarget.directory === model.listing.path ? ' drop-current' : ''}`} data-pane={pane} onPointerDown={props.onFocus} ref={body}>
      <header className="pane-toolbar">
        <span className="pane-label">{pane === 'left' ? 'L' : 'R'}</span>
        <select aria-label={`${pane}主机`} value={model.listing.endpoint} onChange={(event) => props.onEndpoint(event.target.value)}>{hosts.map((host) => <option key={host}>{host}</option>)}</select>
        <form className="path-form" onSubmit={(event) => { event.preventDefault(); if (path.trim()) props.onNavigate(path.trim()) }}>
          <input aria-label={`${pane}路径`} spellCheck={false} value={path} onFocus={props.onFocus} onChange={(event) => setPath(event.target.value)} />
        </form>
        <button className="icon-button" type="button" title="上一级 (Backspace)" onClick={() => props.onNavigate(parentPath(model.listing.path))}><Icon name="up" /></button>
        <button className="icon-button" type="button" title="刷新" onClick={props.onRefresh}><Icon name="refresh" /></button>
      </header>
      <div className="file-table-header" role="row"><span>名称</span><span>大小</span><span>修改时间</span><span>权限</span></div>
      <div className="file-list-shell">
        {model.loading && <div className="loading-line" />}
        {model.error && <div className="pane-error"><Icon name="alert" />{model.error}</div>}
        {!model.error && <VirtualFileList key={`${model.listing.endpoint}:${model.listing.path}`} pane={pane} entries={model.listing.entries} selected={model.selected} dropTarget={dropTarget?.pane === pane ? dropTarget.directory : ''} onFocus={props.onFocus} onSelect={props.onSelect} onOpen={(entry) => entry.directory ? props.onNavigate(entry.path) : props.onSelect(entry)} onBeginDrag={props.onBeginDrag} />}
      </div>
      <footer className="pane-statusbar">
        <div className="action-group"><button type="button" disabled={!model.selected} onClick={props.onDelete}><Icon name="trash" />删除</button><button type="button" disabled={!model.selected} onClick={props.onHash}><Icon name="hash" />SHA-256</button></div>
        <span title={model.listing.path}>{model.listing.entries.length} 项 · d 删除 · h 哈希</span>
      </footer>
      <div className="split-handle horizontal" role="separator" aria-orientation="horizontal" aria-label="调整终端高度" onPointerDown={resizeTerminal} />
      <div className="terminal-panel" style={{ height: terminalHeight }}>
        <div className="terminal-title"><span><Icon name="terminal" />Shell</span><span>{model.listing.endpoint} · {model.listing.path}</span></div>
        {terminalEndpoint === model.listing.endpoint
          ? <TerminalPane key={`${pane}:${model.listing.endpoint}`} pane={pane} path={model.listing.path} active={active} onCWD={(next) => props.onNavigate(next, true)} onSyncFailure={props.onSyncFailure} />
          : <div className="empty-state">等待端点连接后启动终端…</div>}
      </div>
    </section>
  )
}
