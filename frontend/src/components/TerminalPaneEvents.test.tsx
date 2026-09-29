import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { TerminalCWD } from '../types'

const { listeners, ready, changeDirectory } = vi.hoisted(() => ({
  listeners: new Map<string, (event: TerminalCWD) => void>(),
  ready: vi.fn(async () => {}),
  changeDirectory: vi.fn(async () => {}),
}))
vi.mock('../api', () => ({
  api: { startTerminal: async () => 'session', terminalReady: ready, closeTerminal: async () => {}, terminalChangeDirectory: changeDirectory },
  onEvent: (name: string, callback: (event: TerminalCWD) => void) => { listeners.set(name, callback); return () => listeners.delete(name) },
}))
vi.mock('@xterm/xterm', () => ({ Terminal: class {
  rows = 24; cols = 80
  loadAddon() {} open() {} focus() {} dispose() {} writeln() {}
  onData() { return { dispose() {} } }
} }))
vi.mock('@xterm/addon-fit', () => ({ FitAddon: class { fit() {} } }))
import TerminalPane from './TerminalPane'

afterEach(() => { cleanup(); listeners.clear(); ready.mockClear(); changeDirectory.mockReset(); changeDirectory.mockResolvedValue(undefined) })

it('shows a refused navigation outside the terminal instead of drawing over the live prompt', async () => {
  const { rerender } = render(<TerminalPane pane="left" path="/old" active={false} onCWD={() => {}} />)
  await waitFor(() => expect(ready).toHaveBeenCalled())
  act(() => listeners.get('terminal:cwd')!({ pane: 'left', session: 'session', path: '/old' }))
  changeDirectory.mockRejectedValueOnce(new Error('终端正在编辑输入或执行命令'))
  rerender(<TerminalPane pane="left" path="/new" active={false} onCWD={() => {}} />)
  const message = await screen.findByRole('status')
  expect(message.textContent).toContain('终端正在编辑输入或执行命令')
  expect(message.closest('.terminal-host')).toBeNull()
})

it('ignores delayed duplicate cwd prompts without losing genuine shell navigation or command refresh', async () => {
  const onCWD = vi.fn()
  const { rerender } = render(<TerminalPane pane="left" path="/old" active={false} onCWD={onCWD} />)
  await waitFor(() => expect(ready).toHaveBeenCalled())
  const cwd = (path: string) => act(() => listeners.get('terminal:cwd')!({ pane: 'left', session: 'session', path }))
  cwd('/old')
  expect(onCWD).toHaveBeenCalledWith('/old')
  onCWD.mockClear()
  rerender(<TerminalPane pane="left" path="/new" active={false} onCWD={onCWD} />)
  await waitFor(() => expect(changeDirectory).toHaveBeenCalledWith('session', '/new'))
  cwd('/old')
  expect(onCWD).not.toHaveBeenCalled()
  cwd('/new')
  expect(onCWD).toHaveBeenLastCalledWith('/new')
  cwd('/new')
  expect(onCWD).toHaveBeenCalledTimes(2)
  cwd('/old')
  expect(onCWD).toHaveBeenLastCalledWith('/old')
  expect(changeDirectory).toHaveBeenCalledTimes(1)
})

it('ignores reordered native cwd events after a newer acknowledgement', async () => {
  const onCWD = vi.fn()
  render(<TerminalPane pane="left" path="/new" active={false} onCWD={onCWD} />)
  await waitFor(() => expect(ready).toHaveBeenCalled())
  act(() => listeners.get('terminal:cwd')!({pane: 'left', session: 'session', path: '/new', sequence: 2}))
  act(() => listeners.get('terminal:cwd')!({pane: 'left', session: 'session', path: '/old', sequence: 1}))
  expect(onCWD).toHaveBeenCalledTimes(1)
  expect(onCWD).toHaveBeenCalledWith('/new')
})
