import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Bootstrap, DirectoryListing, PaneID } from '../types'

const { list, queueHash, queueDelete, prepareDrop } = vi.hoisted(() => ({
  list: vi.fn<(pane: PaneID, endpoint: string, path: string) => Promise<DirectoryListing>>(),
  queueHash: vi.fn(),
  queueDelete: vi.fn(),
  prepareDrop: vi.fn(),
}))

vi.mock('../api', () => ({
  api: {
    list,
    changeEndpoint: vi.fn(),
    setTheme: vi.fn(),
    queueHash,
    queueDelete,
    queueCommand: vi.fn(),
    cancelJob: vi.fn(),
    prepareDrop,
    queueTransfer: vi.fn(),
  },
  onEvent: vi.fn(() => () => {}),
}))

vi.mock('./TerminalPane', () => ({
  default: ({ pane, path, onCWD }: { pane: PaneID; path: string; onCWD: (path: string) => void }) =>
    <button data-testid={`prompt-${pane}`} onClick={() => onCWD(path)}>prompt</button>,
}))

import Workspace from './Workspace'

const initial: Bootstrap = {
  unlocked: true,
  hosts: ['本机'],
  leftEndpoint: '本机',
  rightEndpoint: '本机',
  leftPath: '/left',
  rightPath: '/right',
  theme: 'system',
  history: [],
}

describe('Workspace refresh integration', () => {
  afterEach(cleanup)
  beforeEach(() => {
    list.mockReset()
    queueHash.mockReset()
    queueDelete.mockReset()
    prepareDrop.mockReset()
    list.mockImplementation(async (pane, endpoint, path) => ({ pane, endpoint, path, entries: [] }))
  })

  it('refreshes the pane after every terminal prompt even when cwd is unchanged', async () => {
    render(<Workspace initial={initial} onLock={() => {}} />)
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2))
    fireEvent.click(screen.getByTestId('prompt-left'))
    await waitFor(() => expect(list).toHaveBeenCalledTimes(3))
    expect(list).toHaveBeenLastCalledWith('left', '本机', '/left')
  })

  it('navigates to the active pane parent with no selected row', async () => {
    render(<Workspace initial={initial} onLock={() => {}} />)
    await waitFor(() => expect(list).toHaveBeenCalledTimes(2))
    fireEvent.keyDown(document.body, { key: 'Backspace' })
    await waitFor(() => expect(list).toHaveBeenCalledTimes(3))
    expect(list).toHaveBeenLastCalledWith('left', '本机', '/')
  })

  it('runs d and h only for the selected file in the active pane', async () => {
    list.mockImplementation(async (pane, endpoint, path) => ({ pane, endpoint, path, entries: [{ name: `${pane}.txt`, path: `${path}/${pane}.txt`, mode: '-rw-------', size: 4, modified: new Date(0).toISOString(), directory: false, symlink: false }] }))
    render(<Workspace initial={initial} onLock={() => {}} />)
    const leftName = await screen.findByText('left.txt')
    fireEvent.pointerDown(leftName.closest('.file-row')!, { button: 0, clientX: 10, clientY: 10 })
    fireEvent.keyDown(document.body, { key: 'h' })
    await waitFor(() => expect(queueHash).toHaveBeenCalledWith('left', '/left/left.txt'))
    fireEvent.keyDown(document.body, { key: 'd' })
    expect(await screen.findByText('确认删除')).toBeInTheDocument()
    fireEvent.click(document.querySelector('.danger-button')!)
    expect(queueDelete).toHaveBeenCalledWith('left', '/left/left.txt', false)
  })

  it('drops across panes onto the hovered directory without selecting page text', async () => {
    list.mockImplementation(async (pane, endpoint, path) => ({ pane, endpoint, path, entries: pane === 'left'
      ? [{ name: 'source.txt', path: '/left/source.txt', mode: '-rw-------', size: 4, modified: new Date(0).toISOString(), directory: false, symlink: false }]
      : [{ name: 'archive', path: '/right/archive', mode: 'drwx------', size: 0, modified: new Date(0).toISOString(), directory: true, symlink: false }] }))
    prepareDrop.mockResolvedValue({ sourcePane: 'left', sourcePath: '/left/source.txt', destinationPane: 'right', destinationDirectory: '/right/archive', targetPath: '/right/archive/source.txt', name: 'source.txt', conflict: false })
    render(<Workspace initial={initial} onLock={() => {}} />)
    const source = (await screen.findByText('source.txt')).closest('.file-row')!
    const destination = (await screen.findByText('archive')).closest('.file-row')!
    Object.defineProperty(document, 'elementFromPoint', { configurable: true, value: () => destination })
    fireEvent.pointerDown(source, { button: 0, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(window, { clientX: 40, clientY: 40 })
    fireEvent.pointerUp(window, { clientX: 40, clientY: 40 })
    await waitFor(() => expect(prepareDrop).toHaveBeenCalledWith('left', '/left/source.txt', 'right', '/right/archive'))
    expect(document.getSelection()?.toString() || '').toBe('')
  })
})
