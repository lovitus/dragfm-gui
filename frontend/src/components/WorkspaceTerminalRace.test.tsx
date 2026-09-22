import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { Bootstrap, DirectoryListing, PaneID } from '../types'

const { list, prompts } = vi.hoisted(() => ({
  list: vi.fn<(pane: PaneID, endpoint: string, path: string) => Promise<DirectoryListing>>(),
  prompts: new Map<PaneID, (path: string) => void>(),
}))
vi.mock('../api', () => ({ api: { list, jobSnapshot: async () => [] }, onEvent: () => () => {} }))
vi.mock('./TerminalPane', () => ({ default: ({ pane, onCWD }: { pane: PaneID; onCWD: (path: string) => void }) => { prompts.set(pane, onCWD); return null } }))
import Workspace from './Workspace'

afterEach(() => { cleanup(); list.mockReset(); prompts.clear() })

it('does not let a delayed shell prompt replace a pending Backspace navigation', async () => {
  const initial: Bootstrap = { unlocked: true, hosts: ['本机'], leftEndpoint: '本机', rightEndpoint: '本机', leftPath: '/source', rightPath: '/target', theme: 'system', history: [] }
  let finish!: (listing: DirectoryListing) => void
  const navigation = new Promise<DirectoryListing>((resolve) => { finish = resolve })
  list.mockImplementation(async (pane, endpoint, path) => pane === 'left' && path === '/' ? navigation : { pane, endpoint, path, entries: [] })
  render(<Workspace initial={initial} onLock={() => {}} />)
  await waitFor(() => expect(list).toHaveBeenCalledTimes(2))
  fireEvent.keyDown(document.body, { key: 'Backspace' })
  await waitFor(() => expect(list).toHaveBeenCalledWith('left', '本机', '/'))
  act(() => prompts.get('left')!('/source'))
  expect(list.mock.calls.filter(([pane, , path]) => pane === 'left' && path === '/source')).toHaveLength(1)
  await act(async () => { finish({ pane: 'left', endpoint: '本机', path: '/', entries: [] }); await navigation })
  await waitFor(() => expect(screen.getByLabelText('left路径')).toHaveValue('/'))
})
