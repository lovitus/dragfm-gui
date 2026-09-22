import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { FilePaneProps } from './FilePane'
vi.mock('./TerminalPane', () => ({ default: ({ pane }: { pane: string }) => <span data-testid={`pty-${pane}`} /> }))
import FilePane from './FilePane'
afterEach(cleanup)

it('waits for saved SSH endpoint reconnection before mounting its PTY and keeps it during refreshes', async () => {
  const props: FilePaneProps = {
    pane: 'right', model: { listing: { pane: 'right', endpoint: 'Saved SSH', path: '/remote', entries: [] }, loading: true, error: '' },
    hosts: ['本机', 'Saved SSH'], active: false, dropTarget: null,
    onFocus: vi.fn(), onNavigate: vi.fn(), onEndpoint: vi.fn(), onRefresh: vi.fn(), onSelect: vi.fn(), onBeginDrag: vi.fn(), onDelete: vi.fn(), onHash: vi.fn(),
  }
  const { rerender } = render(<FilePane {...props} />)
  expect(screen.queryByTestId('pty-right')).toBeNull()
  rerender(<FilePane {...props} model={{ ...props.model, loading: false }} />)
  await waitFor(() => expect(screen.getByTestId('pty-right')).toBeInTheDocument())
  const terminal = screen.getByTestId('pty-right')
  rerender(<FilePane {...props} />)
  expect(screen.getByTestId('pty-right')).toBe(terminal)
})
