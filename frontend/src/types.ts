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
	warning?: string
	connectionID?: number
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
  output?: string
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
  output?: string
  method?: string
  state?: JobUpdate['state']
  finishedAt: string
}

export interface ConfigTexts {
  markdown: string
  revision?: string
}

export interface ConnectionRow {
  id: string
  name: string
  disabled: boolean
  relayAllowed: boolean
  relayReady: boolean
  lastRTT: number
  lastSuccess: string
}

export interface RelayCache {
  endpointAID: string
  endpointBID: string
  endpointA: string
  endpointB: string
  relay: string
  lastSuccess: string
}

export interface ConnectionOverview {
  revision: string
  hosts: ConnectionRow[]
  socks: ConnectionRow[]
  relays: RelayCache[]
}

export interface Challenge {
  id: string
  kind: 'confirm-host-key' | 'password' | 'confirm'
  title: string
  message: string
  secret?: boolean
  allowSave?: boolean
  allowSkip?: boolean
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
