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

interface Props {
  pane: PaneID
  model: PaneModel
  hosts: string[]
  active: boolean
  dropTarget: { pane: PaneID; directory: string } | null
  onFocus: () => void
  onNavigate: (path: string, fromTerminal?: boolean) => void
  onEndpoint: (endpoint: string) => void
  onRefresh: () => void
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
  if (value === '/') return '/'
  const trimmed = value.replace(/[\\/]+$/, '')
  const separator = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'))
  if (separator < 0) return '.'
  if (separator === 0) return trimmed[0]
  if (separator === 2 && /^[A-Za-z]:/.test(trimmed)) return `${trimmed.slice(0, 2)}\\`
  return trimmed.slice(0, separator)
}

function formatSize(value: number, directory: boolean): string {
  if (directory && value === 0) return '—'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++ }
  return `${unit === 0 ? size.toFixed(0) : size < 10 ? size.toFixed(1) : size.toFixed(0)} ${units[unit]}`
}

function formatDate(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function VirtualFileList({ pane, entries, selected, dropTarget, onSelect, onOpen, onBeginDrag }: {
  pane: PaneID
  entries: FileEntry[]
  selected?: FileEntry
  dropTarget: string
  onSelect: (entry: FileEntry) => void
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
    const start = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - 8)
    const end = Math.min(entries.length, Math.ceil((scrollTop + height) / ROW_HEIGHT) + 8)
    return { start, end }
  }, [entries.length, height, scrollTop])
  return (
    <div className="file-viewport" ref={viewport} tabIndex={0} onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)} data-drop-root data-pane={pane}>
      <div className="file-spacer" style={{ height: entries.length * ROW_HEIGHT }}>
        {entries.slice(range.start, range.end).map((entry, offset) => {
          const index = range.start + offset
          const selectedRow = selected?.path === entry.path
          const targetRow = dropTarget === entry.path
          return (
            <div
              className={`file-row${selectedRow ? ' selected' : ''}${targetRow ? ' drop-target' : ''}`}
              style={{ transform: `translateY(${index * ROW_HEIGHT}px)` }}
              key={entry.path}
              role="row"
              tabIndex={selectedRow ? 0 : -1}
              data-drop-directory={entry.directory ? entry.path : undefined}
              data-pane={pane}
              onClick={(event) => { event.currentTarget.focus(); onSelect(entry) }}
              onDoubleClick={() => onOpen(entry)}
              onPointerDown={(event) => {
                if (event.button === 0) {
                  event.preventDefault()
                  event.currentTarget.focus()
                  onSelect(entry)
                }
                onBeginDrag(event, pane, entry)
              }}
            >
              <div className="file-name" title={entry.name}><Icon name={entry.symlink ? 'link' : entry.directory ? 'folder' : 'file'} /><span>{entry.name}</span></div>
              <div className="file-size">{formatSize(entry.size, entry.directory)}</div>
              <div className="file-modified">{formatDate(entry.modified)}</div>
              <div className="file-mode">{entry.mode}</div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export default function FilePane(props: Props) {
  const { pane, model, hosts, active, dropTarget } = props
  const [path, setPath] = useState(model.listing.path)
  const [terminalHeight, setTerminalHeight] = useState(180)
  const body = useRef<HTMLDivElement>(null)
  useEffect(() => setPath(model.listing.path), [model.listing.path])
  const parent = parentPath(model.listing.path)
  const resizeTerminal = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault()
    const startY = event.clientY
    const startHeight = terminalHeight
    const move = (current: PointerEvent) => {
      const maximum = Math.max(120, (body.current?.clientHeight || 500) - 190)
      setTerminalHeight(Math.min(maximum, Math.max(110, startHeight + startY - current.clientY)))
    }
    const stop = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', stop) }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', stop, { once: true })
  }
  return (
    <section
      className={`file-pane${active ? ' active' : ''}`}
      data-pane={pane}
      onPointerDown={props.onFocus}
      ref={body}
    >
      <header className="pane-toolbar">
        <span className="pane-label">{pane === 'left' ? 'L' : 'R'}</span>
        <select aria-label={`${pane}端点`} value={model.listing.endpoint} onChange={(event) => props.onEndpoint(event.target.value)}>
          {hosts.map((host) => <option key={host}>{host}</option>)}
        </select>
        <form className="path-form" onSubmit={(event) => { event.preventDefault(); props.onNavigate(path) }}>
          <input aria-label={`${pane}路径`} spellCheck={false} value={path} onChange={(event) => setPath(event.target.value)} />
        </form>
        <button className="icon-button" type="button" title="上一级" onClick={() => props.onNavigate(parent)}><Icon name="up" /></button>
        <button className="icon-button" type="button" title="刷新" onClick={props.onRefresh}><Icon name="refresh" /></button>
      </header>
      <div className="file-table-header" role="row">
        <span>名称</span><span>大小</span><span>修改时间</span><span>权限</span>
      </div>
      <div className="file-list-shell">
        {model.loading && <div className="loading-line" />}
        {model.error && <div className="pane-error"><Icon name="alert" />{model.error}</div>}
        {!model.error && <VirtualFileList pane={pane} entries={model.listing.entries} selected={model.selected} dropTarget={dropTarget?.pane === pane ? dropTarget.directory : ''} onSelect={(entry) => props.onSelect(entry)} onOpen={(entry) => entry.directory ? props.onNavigate(entry.path) : props.onSelect(entry)} onBeginDrag={props.onBeginDrag} />}
      </div>
      <footer className="pane-statusbar">
        <div className="action-group">
          <button type="button" disabled={!model.selected} onClick={props.onDelete}><Icon name="trash" />删除</button>
          <button type="button" disabled={!model.selected} onClick={props.onHash}><Icon name="hash" />SHA-256</button>
        </div>
        <span title={model.listing.path}>{model.listing.entries.length} 项</span>
      </footer>
      <div className="split-handle horizontal" role="separator" aria-orientation="horizontal" onPointerDown={resizeTerminal} />
      <div className="terminal-panel" style={{ height: terminalHeight }}>
        <div className="terminal-title"><span><Icon name="terminal" />Shell</span><span>{model.listing.endpoint} · {model.listing.path}</span></div>
        <TerminalPane key={model.listing.endpoint} pane={pane} path={model.listing.path} active={active} onCWD={(next) => props.onNavigate(next, true)} />
      </div>
    </section>
  )
}
