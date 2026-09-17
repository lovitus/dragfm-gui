import type {
  Bootstrap, Challenge, ConfigTexts, DirectoryListing, DropPreview, JobUpdate,
  PaneID, TerminalCWD, TerminalData, TransferRequest, VaultStatus,
} from './types'

type EventMap = {
  'job:update': JobUpdate
  'terminal:data': TerminalData
  'terminal:cwd': TerminalCWD
  challenge: Challenge
  locked: Record<string, never>
}

// Never substitute simulated files or successful operations for a broken
// desktop bridge. Component tests inject mocks explicitly via their test runner.
async function invoke<T>(name: string, ...args: unknown[]): Promise<T> {
  const method = window.go?.webgui?.App?.[name]
  if (typeof method !== 'function') throw new Error(`Wails method ${name} is unavailable; no operation was performed`)
  return method(...args) as Promise<T>
}

export function onEvent<K extends keyof EventMap>(name: K, callback: (payload: EventMap[K]) => void): () => void {
  if (!window.runtime?.EventsOn) return () => {}
  const cancel = window.runtime.EventsOn(name, (...args) => callback(args[0] as EventMap[K]))
  return typeof cancel === 'function' ? cancel : () => window.runtime?.EventsOff?.(name)
}

export const api = {
  vaultStatus: (): Promise<VaultStatus> => invoke('VaultStatus'),
  unlock: (password: string): Promise<Bootstrap> => invoke('Unlock', password),
  createVault: (hint: string, password: string, confirm: string): Promise<Bootstrap> => invoke('CreateVault', hint, password, confirm),
  bootstrap: (): Promise<Bootstrap> => invoke('Bootstrap'),
  jobSnapshot: (): Promise<JobUpdate[]> => invoke('JobSnapshot'),
  list: (pane: PaneID, endpoint: string, path: string): Promise<DirectoryListing> => invoke('List', pane, endpoint, path),
  changeEndpoint: (pane: PaneID, endpoint: string, peer: string): Promise<DirectoryListing> => invoke('ChangeEndpoint', pane, endpoint, peer),
  prepareDrop: (sourcePane: PaneID, sourcePath: string, destinationPane: PaneID, destinationDirectory: string): Promise<DropPreview> => invoke('PrepareDrop', sourcePane, sourcePath, destinationPane, destinationDirectory),
  queueTransfer: (request: TransferRequest): Promise<string> => invoke('QueueTransfer', request),
  queueDelete: (pane: PaneID, path: string, recursive: boolean): Promise<string> => invoke('QueueDelete', pane, path, recursive),
  queueHash: (pane: PaneID, path: string): Promise<string> => invoke('QueueHash', pane, path),
  queueCommand: (target: string, command: string): Promise<string> => invoke('QueueCommand', target, command),
  cancelJob: (id: string): Promise<void> => invoke('CancelJob', id),
  startTerminal: (pane: PaneID, path: string, rows: number, columns: number): Promise<string> => invoke('StartTerminal', pane, path, rows, columns),
  terminalReady: (session: string): Promise<void> => invoke('TerminalReady', session),
  terminalInput: (session: string, data: string): Promise<void> => invoke('TerminalInput', session, data),
  terminalResize: (session: string, rows: number, columns: number): Promise<void> => invoke('TerminalResize', session, rows, columns),
  terminalChangeDirectory: (session: string, path: string): Promise<void> => invoke('TerminalChangeDirectory', session, path),
  closeTerminal: (session: string): Promise<void> => invoke('CloseTerminal', session),
  getConfigTexts: (): Promise<ConfigTexts> => invoke('GetConfigTexts'),
  saveConfigTexts: (markdown: string): Promise<Bootstrap> => invoke('SaveConfigTexts', markdown),
  setTheme: (theme: Bootstrap['theme']): Promise<void> => invoke('SetTheme', theme),
  resolveChallenge: (id: string, accepted: boolean, value: string, save: boolean): Promise<void> => invoke('ResolveChallenge', id, accepted, value, save),
  lock: (): Promise<void> => invoke('Lock'),
}
