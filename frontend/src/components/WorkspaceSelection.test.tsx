import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { Bootstrap, DirectoryListing, FileEntry, JobUpdate, PaneID } from '../types'

const { list, listeners } = vi.hoisted(() => ({
  list: vi.fn<(pane: PaneID, endpoint: string, path: string) => Promise<DirectoryListing>>(),
  listeners: new Map<string, (value: JobUpdate) => void>(),
}))
vi.mock('../api', () => ({ api: { list, jobSnapshot: async () => [] }, onEvent: (name: string, callback: (value: JobUpdate) => void) => { listeners.set(name, callback); return () => listeners.delete(name) } }))
vi.mock('./TerminalPane', () => ({ default: () => null }))
import Workspace from './Workspace'

afterEach(() => { cleanup(); list.mockReset(); listeners.clear() })

it('preserves a new selection made while a completion refresh is pending', async () => {
  const initial: Bootstrap = { unlocked: true, hosts: ['本机'], leftEndpoint: '本机', rightEndpoint: '本机', leftPath: '/source', rightPath: '/target', theme: 'system', history: [] }
  const entries: FileEntry[] = ['first.txt', 'second.txt'].map((name) => ({ name, path: '/source/' + name, directory: false, symlink: false, size: 4, mode: '-rw-------', modified: new Date(0).toISOString() }))
  let finishRefresh!: (listing: DirectoryListing) => void
  const refreshing = new Promise<DirectoryListing>((resolve) => { finishRefresh = resolve })
  let leftCalls = 0
  list.mockImplementation(async (pane, endpoint, path) => pane === 'left' && ++leftCalls > 1 ? refreshing : { pane, endpoint, path, entries: pane === 'left' ? entries : [] })
  render(<Workspace initial={initial} onLock={() => {}} />)
  const first = (await screen.findByText('first.txt')).closest('.file-row')!
  fireEvent.click(first)
  expect(first).toHaveClass('selected')
  act(() => listeners.get('job:update')!({ id: 'hash-finished', revision: 1, state: 'succeeded', description: 'hash', message: 'done', progress: 1, progressKnown: true, indeterminate: false, finishedAt: new Date().toISOString() }))
  await waitFor(() => expect(leftCalls).toBe(2))
  expect(first).toHaveClass('selected')
  const second = screen.getByText('second.txt').closest('.file-row')!
  fireEvent.click(second)
  await act(async () => { finishRefresh({ pane: 'left', endpoint: '本机', path: '/source', entries }); await refreshing })
  expect(screen.getByText('second.txt').closest('.file-row')).toHaveClass('selected')
  expect(screen.getByText('first.txt').closest('.file-row')).not.toHaveClass('selected')
})
