import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Bootstrap, DirectoryListing, JobUpdate, PaneID } from '../types'

const { list, listeners } = vi.hoisted(() => ({
  list: vi.fn<(pane: PaneID, endpoint: string, path: string) => Promise<DirectoryListing>>(),
  listeners: new Map<string, (value: JobUpdate) => void>(),
}))
vi.mock('../api', () => ({
  api: { list, jobSnapshot: async () => [] },
  onEvent: (name: string, callback: (value: JobUpdate) => void) => { listeners.set(name, callback); return () => listeners.delete(name) },
}))
vi.mock('./TerminalPane', () => ({ default: () => null }))
import Workspace from './Workspace'

const initial: Bootstrap = {
  unlocked: true, hosts: ['本机'], leftEndpoint: '本机', rightEndpoint: '本机',
  leftPath: '/source', rightPath: '/target', theme: 'system', history: [],
}

describe('navigation wins over automatic task refresh', () => {
  afterEach(() => { cleanup(); list.mockReset(); listeners.clear() })
  it('does not replace pending Backspace navigation with the old rendered path', async () => {
    let finishNavigation!: (listing: DirectoryListing) => void
    const navigation = new Promise<DirectoryListing>((resolve) => { finishNavigation = resolve })
    list.mockImplementation(async (pane, endpoint, path) => path === '/' && pane === 'left'
      ? navigation : { pane, endpoint, path, entries: [] })
    render(<Workspace initial={initial} onLock={() => {}} />)
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2))
    fireEvent.keyDown(document.body, { key: 'Backspace' })
    await waitFor(() => expect(list).toHaveBeenCalledWith('left', '本机', '/'))
    act(() => listeners.get('job:update')!({
      id: 'completed-transfer', revision: 1, state: 'succeeded', description: 'copy',
      message: 'done', progress: 1, progressKnown: true, indeterminate: false,
      finishedAt: new Date().toISOString(),
    }))
    // The right pane proves that the completion-refresh timer really fired.
    await waitFor(() => expect(list.mock.calls.filter(([pane]) => pane === 'right')).toHaveLength(2))
    expect(list.mock.calls.filter(([pane, , path]) => pane === 'left' && path === '/source')).toHaveLength(1)
    await act(async () => { finishNavigation({ pane: 'left', endpoint: '本机', path: '/', entries: [] }); await navigation })
    await waitFor(() => expect(screen.getByLabelText('left路径')).toHaveValue('/'))
  })
})
