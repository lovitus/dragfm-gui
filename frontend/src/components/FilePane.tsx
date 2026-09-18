import { useEffect, useState, type FormEvent, type PointerEvent } from 'react'
import type { DirectoryListing, FileEntry, PaneID } from '../types'
import Icon from './Icon'
import TerminalPane from './TerminalPane'

export interface PaneModel {
  listing: DirectoryListing
  selected?: FileEntry
  loading: boolean
  error: string
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
  onSelect: (entry?: FileEntry) => void
  onBeginDrag: (event: PointerEvent, pane: PaneID, entry: FileEntry) => void
  onDelete: () => void
  onHash: () => void
}

export function parentPath(path: string): string {
  const separator = path.includes('\\') ? '\\' : '/'
  const stripped = path.replace(/[\\/]+$/, '')
  const index = stripped.lastIndexOf(separator)
  if (index < 0) return path
  if (index === 0) return separator
  if (index === 2 && stripped[1] === ':') return stripped.slice(0, 3)
  return stripped.slice(0, index)
}

export function isEditingTarget(target: EventTarget | null): boolean {
  const element = target as HTMLElement | null
  return Boolean(element?.closest('input, textarea, select, [contenteditable="true"], .xterm, .cm-editor'))
}

function sizeLabel(size: number, directory: boolean): string {
  if (directory) return '—'
  if (size < 1024) return `${size} B`
  if (size < 1024 ** 2) return `${(size / 1024).toFixed(1)} KB`
  if (size < 1024 ** 3) return `${(size / 1024 ** 2).toFixed(1)} MB`
  return `${(size / 1024 ** 3).toFixed(1)} GB`
}

function dateLabel(value: string): string {
  if (!value) return '—'
  const date = new Date(value)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

export default function FilePane(props: FilePaneProps) {
  const { pane, model } = props
  const [path, setPath] = useState(model.listing.path)
  const [terminalEndpoint, setTerminalEndpoint] = useState<string | null>(null)
  useEffect(() => setPath(model.listing.path), [model.listing.path])
  useEffect(() => {
    // Bootstrap describes saved endpoint choices, but the backend initially
    // owns local endpoints until List finishes reconnecting them. Do not race
    // StartTerminal against that reconnect and accidentally start a local PTY.
    // Once connected, keep the PTY mounted during ordinary directory refreshes.
    if (!model.loading && !model.error) setTerminalEndpoint(model.listing.endpoint)
  }, [model.loading, model.error, model.listing.endpoint])
  const submitPath = (event: FormEvent) => {
    event.preventDefault()
    if (path.trim()) props.onNavigate(path.trim())
  }
  const dropCurrent = props.dropTarget?.pane === pane && props.dropTarget.directory === model.listing.path
  return (
    <section className={`file-pane ${props.active ? 'active' : ''} ${dropCurrent ? 'drop-current' : ''}`} data-pane={pane} onPointerDown={props.onFocus}>
      <header className="pane-header">
        <div className="endpoint-select"><span className="connection-dot" /><select aria-label={`${pane}主机`} value={model.listing.endpoint} onChange={(event) => props.onEndpoint(event.target.value)}>{props.hosts.map((host) => <option key={host}>{host}</option>)}</select></div>
        <div className="pane-tools"><button className="icon-button" title="返回上一级 (Backspace)" onClick={() => props.onNavigate(parentPath(model.listing.path))}><Icon name="up" /></button><button className="icon-button" title="刷新" onClick={props.onRefresh}><Icon name="refresh" /></button></div>
      </header>
      <form className="path-form" onSubmit={submitPath}>
        <Icon name="folder" />
        <input aria-label={`${pane}路径`} value={path} onFocus={props.onFocus} onChange={(event) => setPath(event.target.value)} spellCheck={false} />
        <button type="submit" className="path-go" aria-label="进入路径"><Icon name="chevron" /></button>
      </form>
      <div className="file-columns" aria-hidden="true"><span>名称</span><span>权限</span><span>大小</span><span>修改时间</span></div>
      <div className="file-viewport" tabIndex={0} onFocus={props.onFocus} data-current-directory={model.listing.path} onPointerDown={(event) => { if (event.target === event.currentTarget) props.onSelect() }}>
        {model.loading && <div className="loading-line" />}
        {model.error && <div className="pane-error"><Icon name="alert" />{model.error}</div>}
        {model.listing.entries.map((entry) => {
          const selected = model.selected?.path === entry.path
          const drop = entry.directory && props.dropTarget?.pane === pane && props.dropTarget.directory === entry.path
          return (
            <div
              className={`file-row ${selected ? 'selected' : ''} ${drop ? 'drop-target' : ''}`}
              key={entry.path}
              data-file-path={entry.path}
              data-drop-directory={entry.directory ? entry.path : undefined}
              onClick={() => props.onSelect(entry)}
              onDoubleClick={() => entry.directory && props.onNavigate(entry.path)}
              onPointerDown={(event) => { props.onSelect(entry); props.onBeginDrag(event, pane, entry) }}
            >
              <span className="file-name" title={entry.name}><Icon name={entry.directory ? 'folder' : 'file'} /><span>{entry.name}</span>{entry.symlink && <small>↗</small>}</span>
              <span className="file-mode">{entry.mode}</span>
              <span className="file-size">{sizeLabel(entry.size, entry.directory)}</span>
              <span className="file-date">{dateLabel(entry.modified)}</span>
            </div>
          )
        })}
        {!model.loading && !model.error && model.listing.entries.length === 0 && <div className="empty-state">此目录为空</div>}
      </div>
      <footer className="file-status"><span>{model.listing.entries.length} 个项目{model.selected ? ` · 已选 ${model.selected.name}` : ''}</span><div><button disabled={!model.selected} onClick={props.onHash}><Icon name="hash" />SHA-256</button><button disabled={!model.selected} className="danger-link" onClick={props.onDelete}><Icon name="trash" />删除</button></div></footer>
      <div className="terminal-label"><div><Icon name="terminal" />交互终端 <span>{model.listing.endpoint === '本机' ? '本地 PTY' : 'SSH PTY'}</span></div><span className="terminal-shortcut">Backspace 上一级 · d 删除 · h 哈希</span></div>
      <div className="terminal-region">
        {terminalEndpoint === model.listing.endpoint
          ? <TerminalPane key={`${pane}:${model.listing.endpoint}`} pane={pane} path={model.listing.path} active={props.active} onCWD={(next) => props.onNavigate(next, true)} />
          : <div className="empty-state">等待端点连接后启动终端…</div>}
      </div>
    </section>
  )
}
