import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

const { saveConfigTexts } = vi.hoisted(() => ({ saveConfigTexts: vi.fn(async () => ({})) }))
const markdown = '#主机\n##test\nuser:secret@host:22\n#私钥\n#socks池\n'
vi.mock('../api', () => ({ api: {
  getConfigTexts: async () => ({ markdown }), saveConfigTexts,
} }))
import ConfigEditor from './ConfigEditor'

describe('configuration draft lifetime', () => {
  afterEach(cleanup)
  it('does not erase the Markdown draft when showing/hiding passwords', async () => {
    render(<ConfigEditor onClose={() => {}} onSaved={() => {}} />)
    await waitFor(() => expect(document.querySelector('.cm-content')).not.toBeNull())
    fireEvent.click(screen.getByText('显示密码'))
    fireEvent.click(screen.getByText('隐藏密码'))
    fireEvent.click(screen.getByText('验证并保存'))
    await waitFor(() => expect(saveConfigTexts).toHaveBeenCalledWith(markdown))
  })
})
