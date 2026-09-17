import type {
  Bootstrap,
  Challenge,
  ConfigTexts,
  DirectoryListing,
  DropPreview,
  FileEntry,
  JobUpdate,
  PaneID,
  TerminalCWD,
  TerminalData,
  TransferRequest,
  VaultStatus,
} from './types'

type EventMap = {
  'job:update': JobUpdate
  'terminal:data': TerminalData
  'terminal:cwd': TerminalCWD
  challenge: Challenge
  locked: Record<string, never>
}

const localEvents = new EventTarget()

function hasBackend(): boolean {
  return Boolean(window.go?.webgui?.App)
}

async function invoke<T>(name: string, ...args: unknown[]): Promise<T> {
  const method = window.go?.webgui?.App?.[name]
  if (!method) throw new Error(`Wails method ${name} is unavailable`)
  return method(...args) as Promise<T>
}

export function onEvent<K extends keyof EventMap>(name: K, callback: (payload: EventMap[K]) => void): () => void {
  if (window.runtime?.EventsOn) {
    const cancel = window.runtime.EventsOn(name, (...args) => callback(args[0] as EventMap[K]))
    return typeof cancel === 'function' ? cancel : () => window.runtime?.EventsOff?.(name)
  }
  const listener = (event: Event) => callback((event as CustomEvent<EventMap[K]>).detail)
  localEvents.addEventListener(name, listener)
  return () => localEvents.removeEventListener(name, listener)
}

function emitMock<K extends keyof EventMap>(name: K, payload: EventMap[K]): void {
  localEvents.dispatchEvent(new CustomEvent(name, { detail: payload }))
}

const names = [
  'Documents', 'Downloads', 'Projects', '.config', '.ssh', 'archive', 'deploy', 'notes',
  'README.md', 'release-checklist.md', 'inventory.csv', 'transfer.log', 'hosts.txt',
]

function mockEntries(path: string): FileEntry[] {
  return names.map((name, index) => {
    const directory = index < 8
    return {
      name,
      path: `${path.replace(/\/$/, '')}/${name}`,
      mode: directory ? 'drwxr-xr-x' : '-rw-r--r--',
      size: directory ? [4096, 128, 544, 224][index % 4] : 18432 + index * 731,
      modified: new Date(Date.now() - index * 3_700_000).toISOString(),
      directory,
      symlink: false,
    }
  })
}

let mockUnlocked = new URLSearchParams(location.search).get('locked') !== '1'
let mockMarkdown = `#主机
#私钥
#socks池
`
type MockTransfer = { id: string; description: string }
const mockTransfers: MockTransfer[] = []
let mockTransferRunning = false

function runNextMockTransfer(): void {
  if (mockTransferRunning || mockTransfers.length === 0) return
  mockTransferRunning = true
  const job = mockTransfers.shift()!
  const startedAt = new Date().toISOString()
  emitMock('job:update', { ...job, state: 'running', message: '正在准备端点', progress: 0, progressKnown: true, indeterminate: true, stage: 'preflight', bytesTotal: 24 * 1024 * 1024, filesTotal: 1, startedAt })
  setTimeout(() => emitMock('job:update', { ...job, state: 'running', message: '安全传输中', progress: 0.67, progressKnown: true, indeterminate: false, stage: 'copy', method: 'direct · source-push · rsync', bytesDone: 16 * 1024 * 1024, bytesTotal: 24 * 1024 * 1024, filesDone: 0, filesTotal: 1, startedAt }), 420)
  setTimeout(() => {
    emitMock('job:update', { ...job, state: 'succeeded', message: '传输完成 · 1 项 · 25165824 bytes', progress: 1, progressKnown: true, indeterminate: false, stage: 'done', bytesDone: 24 * 1024 * 1024, bytesTotal: 24 * 1024 * 1024, filesDone: 1, filesTotal: 1, startedAt, finishedAt: new Date().toISOString() })
    mockTransferRunning = false
    window.setTimeout(runNextMockTransfer, 80)
  }, 1000)
}

const mock = {
  async vaultStatus(): Promise<VaultStatus> {
    return { exists: true, hint: '这是一份无头预览数据', unlocked: mockUnlocked }
  },
  async unlock(password: string): Promise<Bootstrap> {
    if (!password) throw new Error('请输入主密码')
    mockUnlocked = true
    return mock.bootstrap()
  },
  async createVault(hint: string, password: string, confirm: string): Promise<Bootstrap> {
    if (!hint || password.length < 8 || password !== confirm) throw new Error('请检查 hint 和两次主密码')
    mockUnlocked = true
    return mock.bootstrap()
  },
  async bootstrap(): Promise<Bootstrap> {
    return {
      unlocked: mockUnlocked,
      hosts: ['本机', 'production', 'archive'],
      leftEndpoint: '本机', rightEndpoint: 'production',
      leftPath: '/Users/demo/Projects', rightPath: '/srv/releases', theme: 'system', history: [],
    }
  },
  async list(pane: PaneID, endpoint: string, path: string): Promise<DirectoryListing> {
    await new Promise((resolve) => setTimeout(resolve, 60))
    return { pane, endpoint, path, entries: mockEntries(path) }
  },
  async changeEndpoint(pane: PaneID, endpoint: string): Promise<DirectoryListing> {
    const path = endpoint === '本机' ? '/Users/demo' : '/srv'
    return { pane, endpoint, path, entries: mockEntries(path) }
  },
  async prepareDrop(sourcePane: PaneID, sourcePath: string, destinationPane: PaneID, destinationDirectory: string): Promise<DropPreview> {
    const name = sourcePath.split('/').pop() || sourcePath
    return { sourcePane, sourcePath, destinationPane, destinationDirectory, targetPath: `${destinationDirectory}/${name}`, name, conflict: name === 'archive' }
  },
  async queueTransfer(request: TransferRequest): Promise<string> {
    const id = `mock-${Date.now()}`
    const description = `${request.move ? '移动' : '复制'} · ${request.name}`
    emitMock('job:update', { id, state: 'pending', description, message: '', progress: 0, progressKnown: false, indeterminate: false })
    mockTransfers.push({ id, description })
    window.setTimeout(runNextMockTransfer, 80)
    return id
  },
  async queueDelete(): Promise<string> { return `delete-${Date.now()}` },
  async queueHash(): Promise<string> { return `hash-${Date.now()}` },
  async queueCommand(): Promise<string> { return `command-${Date.now()}` },
  async cancelJob(): Promise<void> {},
  async startTerminal(pane: PaneID, path: string): Promise<string> {
    const session = `mock-terminal-${pane}-${Date.now()}`
    setTimeout(() => {
      const content = `\u001b[38;5;39mdragfm\u001b[0m  ${pane}  ${path}\r\n$ `
      emitMock('terminal:data', { session, pane, data: btoa(content) })
    }, 80)
    return session
  },
  async terminalInput(): Promise<void> {},
  async terminalResize(): Promise<void> {},
  async terminalChangeDirectory(): Promise<void> {},
  async closeTerminal(): Promise<void> {},
  async getConfigTexts(): Promise<ConfigTexts> { return { markdown: mockMarkdown } },
  async saveConfigTexts(markdown: string): Promise<Bootstrap> {
    mockMarkdown = markdown
    return mock.bootstrap()
  },
  async resolveChallenge(): Promise<void> {},
  async lock(): Promise<void> { mockUnlocked = false },
}

export const api = {
  isMock: !hasBackend(),
  vaultStatus: (): Promise<VaultStatus> => hasBackend() ? invoke('VaultStatus') : mock.vaultStatus(),
  unlock: (password: string): Promise<Bootstrap> => hasBackend() ? invoke('Unlock', password) : mock.unlock(password),
  createVault: (hint: string, password: string, confirm: string): Promise<Bootstrap> => hasBackend() ? invoke('CreateVault', hint, password, confirm) : mock.createVault(hint, password, confirm),
  bootstrap: (): Promise<Bootstrap> => hasBackend() ? invoke('Bootstrap') : mock.bootstrap(),
  list: (pane: PaneID, endpoint: string, path: string): Promise<DirectoryListing> => hasBackend() ? invoke('List', pane, endpoint, path) : mock.list(pane, endpoint, path),
  changeEndpoint: (pane: PaneID, endpoint: string, peer: string): Promise<DirectoryListing> => hasBackend() ? invoke('ChangeEndpoint', pane, endpoint, peer) : mock.changeEndpoint(pane, endpoint),
  prepareDrop: (sourcePane: PaneID, sourcePath: string, destinationPane: PaneID, destinationDirectory: string): Promise<DropPreview> => hasBackend() ? invoke('PrepareDrop', sourcePane, sourcePath, destinationPane, destinationDirectory) : mock.prepareDrop(sourcePane, sourcePath, destinationPane, destinationDirectory),
  queueTransfer: (request: TransferRequest): Promise<string> => hasBackend() ? invoke('QueueTransfer', request) : mock.queueTransfer(request),
  queueDelete: (pane: PaneID, path: string, recursive: boolean): Promise<string> => hasBackend() ? invoke('QueueDelete', pane, path, recursive) : mock.queueDelete(),
  queueHash: (pane: PaneID, path: string): Promise<string> => hasBackend() ? invoke('QueueHash', pane, path) : mock.queueHash(),
  queueCommand: (target: string, command: string): Promise<string> => hasBackend() ? invoke('QueueCommand', target, command) : mock.queueCommand(),
  cancelJob: (id: string): Promise<void> => hasBackend() ? invoke('CancelJob', id) : mock.cancelJob(),
  startTerminal: (pane: PaneID, path: string, rows: number, columns: number): Promise<string> => hasBackend() ? invoke('StartTerminal', pane, path, rows, columns) : mock.startTerminal(pane, path),
  terminalInput: (session: string, data: string): Promise<void> => hasBackend() ? invoke('TerminalInput', session, data) : mock.terminalInput(),
  terminalResize: (session: string, rows: number, columns: number): Promise<void> => hasBackend() ? invoke('TerminalResize', session, rows, columns) : mock.terminalResize(),
  terminalChangeDirectory: (session: string, path: string): Promise<void> => hasBackend() ? invoke('TerminalChangeDirectory', session, path) : mock.terminalChangeDirectory(),
  closeTerminal: (session: string): Promise<void> => hasBackend() ? invoke('CloseTerminal', session) : mock.closeTerminal(),
  getConfigTexts: (): Promise<ConfigTexts> => hasBackend() ? invoke('GetConfigTexts') : mock.getConfigTexts(),
  saveConfigTexts: (markdown: string): Promise<Bootstrap> => hasBackend() ? invoke('SaveConfigTexts', markdown) : mock.saveConfigTexts(markdown),
  setTheme: (theme: Bootstrap['theme']): Promise<void> => hasBackend() ? invoke('SetTheme', theme) : Promise.resolve(),
  resolveChallenge: (id: string, accepted: boolean, value: string, save: boolean): Promise<void> => hasBackend() ? invoke('ResolveChallenge', id, accepted, value, save) : mock.resolveChallenge(),
  lock: (): Promise<void> => hasBackend() ? invoke('Lock') : mock.lock(),
}
