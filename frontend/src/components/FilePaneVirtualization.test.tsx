import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import FilePane, { type FilePaneProps } from './FilePane'
vi.mock('./TerminalPane', () => ({ default: () => null }))
afterEach(cleanup)

it('positions distinct virtual rows and resets scrolling on directory navigation', () => {
 const entries = Array.from({ length: 10000 }, (_, i) => ({ name: `file-${i}`, path: `/a/file-${i}`, mode: '-rw-------', size: i, modified: new Date(0).toISOString(), directory: false, symlink: false }))
 const props: FilePaneProps = { pane: 'left', model: { listing: { pane: 'left', endpoint: '本机', path: '/a', entries }, loading: false, error: '' }, hosts: ['本机'], active: true, dropTarget: null, onFocus: vi.fn(), onNavigate: vi.fn(), onEndpoint: vi.fn(), onRefresh: vi.fn(), onSelect: vi.fn(), onBeginDrag: vi.fn(), onDelete: vi.fn(), onHash: vi.fn() }
 const { rerender } = render(<FilePane {...props} />)
 expect(document.querySelectorAll('.file-row').length).toBeLessThan(40)
 expect(screen.getByText('file-0').closest('.file-row')).toHaveStyle({ transform: 'translateY(0px)' })
 expect(screen.getByText('file-1').closest('.file-row')).toHaveStyle({ transform: 'translateY(30px)' })
 const viewport = document.querySelector('.file-viewport')!
 fireEvent.scroll(viewport, { target: { scrollTop: 30000 } })
 expect(screen.queryByText('file-0')).toBeNull()
 rerender(<FilePane {...props} model={{ ...props.model, listing: { ...props.model.listing, path: '/b', entries: [entries[0]] } }} />)
 expect(screen.getByText('file-0')).toBeInTheDocument()
 expect(document.querySelector('.file-viewport')).not.toBe(viewport)
})
