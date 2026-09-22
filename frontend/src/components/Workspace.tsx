import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { api, onEvent } from '../api'
import { CWDGate } from '../cwdGate'
import { isFinished, mergeJobUpdates } from '../jobState'
import type { Bootstrap, DirectoryListing, DropPreview, FileEntry, HistoryEntry, JobUpdate, PaneID } from '../types'
import type { PaneModel } from './FilePane'
import FilePane, { isEditingTarget, parentPath } from './FilePane'
import TaskPane from './TaskPane'
import ConfigEditor from './ConfigEditor'
import Icon from './Icon'
import Modal from './Modal'
import './Workspace.css'

function emptyListing(pane: PaneID, endpoint: string, path: string): DirectoryListing {
  return { pane, endpoint, path, entries: [] }
}

type DragState = { pane: PaneID; entry: FileEntry; x: number; y: number }

export function resolveDropTarget(under: HTMLElement | null, sourcePane: PaneID, panePaths: Record<PaneID, string>): { pane: PaneID; directory: string } | null {
  const paneElement = under?.closest<HTMLElement>('.file-pane[data-pane]')
  const destinationPane = paneElement?.dataset.pane as PaneID | undefined
  if (!destinationPane || destinationPane === sourcePane) return null
  const directoryRow = under?.closest<HTMLElement>('[data-drop-directory]')
  return { pane: destinationPane, directory: directoryRow?.dataset.dropDirectory || panePaths[destinationPane] }
}

export function shouldNavigateOnBackspace(event: Pick<KeyboardEvent, 'key' | 'ctrlKey' | 'altKey' | 'metaKey' | 'target'>, modalOpen: boolean): boolean {
  return !modalOpen && event.key === 'Backspace' && !event.ctrlKey && !event.altKey && !event.metaKey && !isEditingTarget(event.target)
}

export function fileShortcut(event: Pick<KeyboardEvent, 'key' | 'ctrlKey' | 'altKey' | 'metaKey' | 'target' | 'repeat'>, modalOpen: boolean): 'delete' | 'hash' | null {
  if (modalOpen || event.repeat || event.ctrlKey || event.altKey || event.metaKey || isEditingTarget(event.target)) return null
  if (event.key.toLowerCase() === 'd') return 'delete'
  if (event.key.toLowerCase() === 'h') return 'hash'
  return null
}

export default function Workspace({ initial, onLock, challengeOpen = false }: { initial: Bootstrap; onLock: () => void; challengeOpen?: boolean }) {
  const [bootstrap, setBootstrap] = useState(initial)
  const [activePane, setActivePane] = useState<PaneID>('left')
  const [models, setModels] = useState<Record<PaneID, PaneModel>>({
    left: { listing: emptyListing('left', initial.leftEndpoint, initial.leftPath), loading: true, error: '' },
    right: { listing: emptyListing('right', initial.rightEndpoint, initial.rightPath), loading: true, error: '' },
  })
  const modelsRef = useRef(models)
  modelsRef.current = models
  const revisions = useRef<Record<PaneID, number>>({ left: 0, right: 0 })
  const cwdGates = useRef({ left: new CWDGate(), right: new CWDGate() })
  const listingInFlight = useRef<Record<PaneID, boolean>>({ left: false, right: false })
  const [jobs, setJobs] = useState<JobUpdate[]>([])
  const [activity, setActivity] = useState<JobUpdate[]>([])
  const [drag, setDrag] = useState<DragState | null>(null)
  const [dropTarget, setDropTarget] = useState<{ pane: PaneID; directory: string } | null>(null)
  const [dropPreview, setDropPreview] = useState<DropPreview | null>(null)
  const [deleteEntry, setDeleteEntry] = useState<{ pane: PaneID; entry: FileEntry } | null>(null)
  const [settings, setSettings] = useState(false)
  const [operationError, setOperationError] = useState('')
  const dragCleanup = useRef<(() => void) | null>(null)
  useEffect(() => () => dragCleanup.current?.(), [])
  const runAction = useCallback(async (operation: () => Promise<unknown>): Promise<boolean> => {
    setOperationError('')
    try { await operation(); return true }
    catch (reason) { setOperationError(String(reason)); return false }
  }, [])

  const load = useCallback(async (pane: PaneID, path?: string, endpoint?: string, preserveSelection = false, fromTerminal = false) => {
    // A completion refresh must not replace a newer user navigation with
    // the old, still-rendered directory while its request is in flight.
    if (path === undefined && endpoint === undefined && listingInFlight.current[pane]) return
    const userNavigation = !fromTerminal && (path !== undefined || endpoint !== undefined)
    if (userNavigation) cwdGates.current[pane].begin()
    listingInFlight.current[pane] = true
    const current = modelsRef.current[pane]
    const revision = ++revisions.current[pane]
    const requestedPath = path || current.listing.path
    const requestedEndpoint = endpoint || current.listing.endpoint
    setModels((old) => ({ ...old, [pane]: { ...old[pane], loading: true, error: '', selected: preserveSelection ? old[pane].selected : undefined } }))
    try {
      const listing = await api.list(pane, requestedEndpoint, requestedPath)
      if (revision !== revisions.current[pane]) return
      if (userNavigation) cwdGates.current[pane].resolve(listing.path, current.listing.path)
      // Publish the authoritative location before React commits its render;
      // a prompt/refresh arriving in that interval must not reload the old path.
      modelsRef.current = { ...modelsRef.current, [pane]: { ...modelsRef.current[pane], listing } }
      setModels((old) => {
        // Preserve the latest selection, including a click made while this
        // background listing was pending. Never retain a deleted/stale entry.
        const sameDirectory = old[pane].listing.path === listing.path && old[pane].listing.endpoint === listing.endpoint
        const selected = preserveSelection && sameDirectory ? listing.entries.find((entry) => entry.path === old[pane].selected?.path) : undefined
        return { ...old, [pane]: { listing, selected, loading: false, error: '' } }
      })
    } catch (reason) {
      if (revision !== revisions.current[pane]) return
      if (userNavigation) cwdGates.current[pane].clear()
      setModels((old) => ({ ...old, [pane]: { ...old[pane], loading: false, error: String(reason) } }))
    } finally {
      if (revision === revisions.current[pane]) listingInFlight.current[pane] = false
    }
  }, [])

  useEffect(() => { void load('left'); void load('right') }, [load])
  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      const modalOpen = challengeOpen || settings || dropPreview !== null || deleteEntry !== null
      if (shouldNavigateOnBackspace(event, modalOpen)) {
        event.preventDefault()
        const current = modelsRef.current[activePane].listing.path
        void load(activePane, parentPath(current))
        return
      }
      if (modalOpen) return
      const action = fileShortcut(event, false)
      const selected = modelsRef.current[activePane].selected
      if (action && selected) {
        event.preventDefault()
        if (action === 'delete') setDeleteEntry({ pane: activePane, entry: selected })
        else void runAction(() => api.queueHash(activePane, selected.path))
        return
      }
      if (!event.ctrlKey || !event.shiftKey || event.altKey || event.metaKey) return
      if (event.key.toLowerCase() === 'l') {
        event.preventDefault()
        onLock()
      }
      if (event.key.toLowerCase() === 't') {
        event.preventDefault()
        setBootstrap((current) => {
          const theme = current.theme === 'system' ? 'light' : current.theme === 'light' ? 'dark' : 'system'
          void runAction(() => api.setTheme(theme))
          return { ...current, theme }
        })
      }
    }
    window.addEventListener('keydown', shortcut)
    return () => window.removeEventListener('keydown', shortcut)
  }, [activePane, challengeOpen, deleteEntry, dropPreview, load, onLock, runAction, settings])
  useEffect(() => {
    let disposed = false
    let latest: JobUpdate[] = []
    let refreshTimer: number | undefined
    const apply = (updates: JobUpdate[]) => {
      if (disposed) return
      const old = new Map(latest.map((job) => [job.id, job]))
      latest = mergeJobUpdates(latest, updates)
      setJobs(latest)
      const changes = latest.filter((job) => old.get(job.id) !== job)
      if (changes.length) setActivity((activity) => [...activity, ...changes.map((job) => ({ ...job, observedAt: new Date().toISOString() }))].slice(-500))
      if (changes.some((job) => isFinished(job) && !old.get(job.id)?.finishedAt)) {
        window.clearTimeout(refreshTimer)
        refreshTimer = window.setTimeout(() => { void load('left', undefined, undefined, true); void load('right', undefined, undefined, true) }, 80)
      }
    }
    // Subscribe first; revision-aware merging closes the subscribe/snapshot race.
    const unsubscribe = onEvent('job:update', (update) => apply([update]))
    const reconcile = () => {
      if (document.visibilityState === 'hidden') return
      void api.jobSnapshot().then(apply).catch((reason) => { if (!disposed) setOperationError(String(reason)) })
    }
    reconcile()
    const timer = window.setInterval(reconcile, 2000)
    window.addEventListener('focus', reconcile)
    document.addEventListener('visibilitychange', reconcile)
    return () => {
      disposed = true
      unsubscribe()
      window.clearInterval(timer)
      window.clearTimeout(refreshTimer)
      window.removeEventListener('focus', reconcile)
      document.removeEventListener('visibilitychange', reconcile)
    }
  }, [load])

  const changeEndpoint = async (pane: PaneID, endpoint: string) => {
    cwdGates.current[pane].clear()
    listingInFlight.current[pane] = true
    const peer: PaneID = pane === 'left' ? 'right' : 'left'
    const revision = ++revisions.current[pane]
    setModels((old) => ({ ...old, [pane]: { ...old[pane], loading: true, error: '' } }))
    try {
      const listing = await api.changeEndpoint(pane, endpoint, modelsRef.current[peer].listing.endpoint)
      if (revision !== revisions.current[pane]) return
      setModels((old) => ({ ...old, [pane]: { listing, loading: false, error: '' } }))
    } catch (reason) {
      if (revision !== revisions.current[pane]) return
      setModels((old) => ({ ...old, [pane]: { ...old[pane], loading: false, error: String(reason) } }))
    } finally {
      if (revision === revisions.current[pane]) listingInFlight.current[pane] = false
    }
  }

  const beginDrag = (event: ReactPointerEvent, pane: PaneID, entry: FileEntry) => {
    if (event.button !== 0) return
    event.preventDefault()
    dragCleanup.current?.()
    document.getSelection()?.removeAllRanges()
    const startX = event.clientX, startY = event.clientY
    let started = false
    let latestTarget: { pane: PaneID; directory: string } | null = null
    const move = (current: PointerEvent) => {
      if (!started && Math.hypot(current.clientX - startX, current.clientY - startY) < 7) return
      if (!started) {
        started = true
        document.documentElement.classList.add('dragging-files')
      }
      const under = document.elementFromPoint(current.clientX, current.clientY) as HTMLElement | null
      latestTarget = resolveDropTarget(under, pane, { left: modelsRef.current.left.listing.path, right: modelsRef.current.right.listing.path })
      setDropTarget(latestTarget)
      setDrag({ pane, entry, x: current.clientX, y: current.clientY })
    }
    const finish = (cancelled: boolean) => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', cancel)
      document.documentElement.classList.remove('dragging-files')
      dragCleanup.current = null
      setDrag(null); setDropTarget(null)
      if (cancelled || !started || !latestTarget) return
      const target = latestTarget
      void runAction(async () => setDropPreview(await api.prepareDrop(pane, entry.path, target.pane, target.directory)))
    }
    const up = () => finish(false)
    const cancel = () => finish(true)
    dragCleanup.current = cancel
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up, { once: true })
    window.addEventListener('pointercancel', cancel, { once: true })
  }

  const transfer = async (move: boolean) => {
    if (!dropPreview) return
    const request = { ...dropPreview, move, overwrite: dropPreview.conflict }
    if (await runAction(() => api.queueTransfer(request))) setDropPreview(null)
  }
  const currentHistory: HistoryEntry[] = jobs.filter((job) => ['succeeded', 'failed', 'cancelled'].includes(job.state)).map((job) => ({ id: job.id, operation: job.description, success: job.state === 'succeeded', message: job.message, finishedAt: job.finishedAt || '' }))
  const currentIDs = new Set(currentHistory.map((item) => item.id))
  const history = [...bootstrap.history.filter((item) => !currentIDs.has(item.id)), ...currentHistory]
  const select = (pane: PaneID, entry?: FileEntry) => setModels((old) => ({ ...old, [pane]: { ...old[pane], selected: entry } }))
  const themeLabel = bootstrap.theme === 'dark' ? '深色' : bootstrap.theme === 'light' ? '亮色' : '默认'

  return (
    <div className="workspace" data-theme={bootstrap.theme}>
      <header className="appbar">
        <div className="app-identity"><span className="logo">df</span><strong>dragfm</strong><span>安全双端点工作区</span></div>
        <div className="app-actions">
          <span className="theme-indicator" title="Ctrl+Shift+T 切换默认 / 亮色 / 深色">{themeLabel}</span>
          <button onClick={() => setSettings(true)}><Icon name="settings" />连接配置</button>
          <button onClick={onLock}><Icon name="lock" />锁定</button>
        </div>
      </header>
      {operationError && <div className="operation-error" role="alert"><span>{operationError}</span><button onClick={() => setOperationError('')} aria-label="关闭错误">关闭</button></div>}
      <main className="workspace-grid">
        {(['left', 'right'] as PaneID[]).map((pane) => <FilePane
          key={pane}
          pane={pane}
          model={models[pane]}
          hosts={bootstrap.hosts}
          active={activePane === pane}
          dropTarget={dropTarget}
          onFocus={() => setActivePane(pane)}
          onNavigate={(path, fromTerminal) => {
            if (fromTerminal && !cwdGates.current[pane].accept(path)) return
            // An unchanged shell prompt is a refresh, not new navigation. In
            // particular, it must not supersede an in-flight Backspace/List.
            if (fromTerminal && path === modelsRef.current[pane].listing.path) void load(pane, undefined, undefined, true)
            else void load(pane, path, undefined, false, Boolean(fromTerminal))
          }}
          onEndpoint={(endpoint) => void changeEndpoint(pane, endpoint)}
          onRefresh={() => void load(pane)}
          onSyncFailure={() => cwdGates.current[pane].clear()}
          onSelect={(entry) => select(pane, entry)}
          onBeginDrag={beginDrag}
          onDelete={() => models[pane].selected && setDeleteEntry({ pane, entry: models[pane].selected! })}
          onHash={() => models[pane].selected && void runAction(() => api.queueHash(pane, models[pane].selected!.path))}
        />)}
        <TaskPane jobs={jobs} activity={activity} history={history} activePane={activePane} onCommand={(target, command) => runAction(() => api.queueCommand(target, command))} onCancel={(id) => void runAction(() => api.cancelJob(id))} />
      </main>
      {drag && <div className="drag-ghost" style={{ transform: `translate(${drag.x + 14}px, ${drag.y + 12}px)` }}><Icon name={drag.entry.directory ? 'folder' : 'file'} /><span>{drag.entry.name}</span>{dropTarget && <small>放入 {dropTarget.directory}</small>}</div>}
      {dropPreview && <Modal title={dropPreview.conflict ? '目标中已有同名项目' : '确认传输'} onClose={() => setDropPreview(null)}>
        <div className="drop-confirm">
          <div className="transfer-path"><span>{dropPreview.name}</span><Icon name="move" /><span>{dropPreview.destinationDirectory}</span></div>
          <p>{dropPreview.conflict ? '目录使用合并语义；文件将原子替换。移动操作会在完整哈希校验后才删除源。' : '选择复制或移动。任务会进入全局单并发队列。'}</p>
          <div className="choice-grid"><button className="choice-button" onClick={() => void transfer(false)}><Icon name="copy" /><strong>{dropPreview.conflict ? '复制并覆盖' : '复制'}</strong><span>保留源项目</span></button><button className="choice-button warning" onClick={() => void transfer(true)}><Icon name="move" /><strong>{dropPreview.conflict ? '移动并覆盖' : '移动'}</strong><span>校验后删除源</span></button></div>
        </div>
      </Modal>}
      {deleteEntry && <Modal title="确认删除" onClose={() => setDeleteEntry(null)}>
        <div className="delete-confirm"><Icon name="trash" /><div><strong>{deleteEntry.entry.name}</strong><p>{deleteEntry.entry.path}</p></div></div>
        <footer className="modal-footer"><span>删除任务将进入队列，并在完成后刷新两栏。</span><button className="secondary-button" onClick={() => setDeleteEntry(null)}>取消</button><button className="danger-button" onClick={() => { void runAction(() => api.queueDelete(deleteEntry.pane, deleteEntry.entry.path, deleteEntry.entry.directory)).then((success) => { if (success) setDeleteEntry(null) }) }}>删除</button></footer>
      </Modal>}
      {settings && <ConfigEditor onClose={() => setSettings(false)} onSaved={(next) => { setBootstrap(next); setSettings(false) }} />}
    </div>
  )
}
