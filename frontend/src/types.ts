export type PaneID = 'left' | 'right'

export interface VaultStatus {
  exists: boolean
  hint: string
  unlocked: boolean
}

export interface Bootstrap {
  unlocked: boolean
  hosts: string[]
  leftEndpoint: string
  rightEndpoint: string
  leftPath: string
  rightPath: string
  theme: 'system' | 'light' | 'dark'
  history: HistoryEntry[]
}

export interface FileEntry {
  name: string
  path: string
  mode: string
  size: number
  modified: string
  directory: boolean
  symlink: boolean
}

export interface DirectoryListing {
  pane: PaneID
  endpoint: string
  path: string
  entries: FileEntry[]
}

export interface DropPreview {
  sourcePane: PaneID
  destinationPane: PaneID
  sourcePath: string
  destinationDirectory: string
  targetPath: string
  name: string
  conflict: boolean
}

export interface TransferRequest extends DropPreview {
  move: boolean
  overwrite: boolean
}

export interface JobUpdate {
  revision?: number
  id: string
  state: 'pending' | 'running' | 'succeeded' | 'failed' | 'cancelled'
  description: string
  message: string
  progress: number
  progressKnown: boolean
  indeterminate: boolean
  stage?: string
  method?: string
  bytesDone?: number
  bytesTotal?: number
  filesDone?: number
  filesTotal?: number
  startedAt?: string
  finishedAt?: string
  observedAt?: string
}

export interface HistoryEntry {
  id: string
  operation: string
  success: boolean
  message: string
  finishedAt: string
}

export interface ConfigTexts {
  markdown: string
}

export interface Challenge {
  id: string
  kind: 'confirm-host-key' | 'password'
  title: string
  message: string
  secret?: boolean
  allowSave?: boolean
}

export interface TerminalData {
  session: string
  pane: PaneID
  data: string
}

export interface TerminalCWD {
  sequence?: number
  session: string
  pane: PaneID
  path: string
}
