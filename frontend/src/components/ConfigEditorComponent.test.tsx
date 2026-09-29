import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { EditorView } from '@codemirror/view'
import type { JobUpdate } from '../types'

const backend = vi.hoisted(() => ({
  saveConfigTexts: vi.fn(async () => ({})), getConfigTexts: vi.fn(), connectionOverview: vi.fn(),
  setConnectionPolicy: vi.fn(), clearRelayCache: vi.fn(), queueConnectionTest: vi.fn(), jobSnapshot: vi.fn(),
  onEvent: vi.fn(), cancelJob: vi.fn(),
}))
const markdown = '#主机\n##test\nuser:secret@host:22\n#私钥\n#socks池\n'
vi.mock('../api', () => ({ api: backend, onEvent: backend.onEvent }))
import ConfigEditor from './ConfigEditor'

describe('configuration draft lifetime', () => {
  afterEach(cleanup)
  beforeEach(() => {
    vi.resetAllMocks()
    backend.getConfigTexts.mockResolvedValue({ markdown })
    backend.saveConfigTexts.mockResolvedValue({})
    backend.onEvent.mockReturnValue(() => {})
    backend.connectionOverview.mockResolvedValue({ revision: 'initial', hosts: [{ id: 'test', name: 'test', disabled: false, relayAllowed: true, relayReady: false, lastSuccess: '', lastRTT: 0 }], socks: [], relays: [] })
  })
  it('does not erase the Markdown draft when showing/hiding passwords', async () => {
    render(<ConfigEditor onClose={() => {}} onSaved={() => {}} />)
    await waitFor(() => expect(document.querySelector('.cm-content')).not.toBeNull())
    fireEvent.click(screen.getByText('显示密码'))
    fireEvent.click(screen.getByText('隐藏密码'))
    fireEvent.click(screen.getByText('验证并保存'))
    await waitFor(() => expect(backend.saveConfigTexts).toHaveBeenCalledWith(markdown))
  })

  it('preserves edits made while a connection policy save is in flight', async () => {
    let finish!: (value: object) => void
    backend.setConnectionPolicy.mockReturnValue(new Promise((resolve) => { finish = resolve }))
    render(<ConfigEditor onClose={() => {}} onSaved={() => {}} />)
    await waitFor(() => expect(document.querySelector('.cm-content')).not.toBeNull())
    fireEvent.click(screen.getByText('连接、池与缓存'))
    const row = await screen.findByRole('article', { name: 'test' })
    fireEvent.click(within(row).getByRole('checkbox', { name: '允许作跳板' }))
    fireEvent.click(screen.getByText('文本配置'))
    // Real CodeMirror document transaction. This checks draft lifetime, not
    // native typing; production WebView/OS input has a separate hosted gate.
    act(() => {
      const editor = EditorView.findFromDOM(document.querySelector('.cm-editor') as HTMLElement)!
      editor.dispatch({ changes: { from: editor.state.doc.length, insert: '##new-host\nuser@192.0.2.11:22\n' } })
    })
    await act(async () => { finish({}); await Promise.resolve() })
    expect(await screen.findByText(/当前草稿已保留/)).toBeVisible()
    expect(document.querySelector('.cm-content')?.textContent).toContain('new-host')
    expect(screen.getByText('文本配置 · 未保存')).toBeVisible()
    // Explicit discard still waits for confirmation and must never happen as
    // a side effect of the policy response.
    fireEvent.click(screen.getByText('重新载入已保存配置'))
    fireEvent.click(screen.getByText('保留草稿'))
    expect(document.querySelector('.cm-content')?.textContent).toContain('new-host')
  })

  it('keeps the selected test successful when its older running snapshot arrives later', async () => {
    let snapshot!: (jobs: JobUpdate[]) => void
    let publish!: (job: JobUpdate) => void
    backend.onEvent.mockImplementation((_name, callback) => { publish = callback; return () => {} })
    backend.queueConnectionTest.mockResolvedValue('selected-test')
    backend.jobSnapshot.mockReturnValue(new Promise((resolve) => { snapshot = resolve }))
    render(<ConfigEditor onClose={() => {}} onSaved={() => {}} />)
    fireEvent.click(screen.getByText('连接、池与缓存'))
    const row = await screen.findByRole('article', { name: 'test' })
    fireEvent.click(within(row).getByRole('button', { name: '仅测试此会话' }))
    // Admission returns an ID before the read snapshot is released.
    await act(async () => { await Promise.resolve() })
    const job: JobUpdate = { id: 'selected-test', revision: 9, state: 'succeeded', description: 'Selected route', message: '完成当前路由登录', progress: 1, progressKnown: true, indeterminate: false }
    act(() => publish(job))
    expect(within(row).getByRole('status')).toHaveTextContent('成功')
    await act(async () => { snapshot([{ ...job, revision: 7, state: 'running', message: 'older running snapshot' }]); await Promise.resolve() })
    expect(within(row).getByRole('status')).toHaveTextContent('成功 · 完成当前路由登录')
    expect(within(row).getByRole('button', { name: '仅测试此会话' })).toBeEnabled()
  })
})
