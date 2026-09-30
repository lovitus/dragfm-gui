import { useLayoutEffect, useRef, type ReactNode } from 'react'
import Icon from './Icon'

export default function Modal({ title, children, onClose, width = 520 }: { title: string; children: ReactNode; onClose?: () => void; width?: number }) {
  const dialog = useRef<HTMLElement>(null)
  useLayoutEffect(() => {
    const node = dialog.current
    if (!node) return
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const topmost = () => [...document.querySelectorAll('.modal')].at(-1) === node
    const controls = () => [...node.querySelectorAll<HTMLElement>('button, input, select, textarea, a[href], [tabindex], [contenteditable="true"]')]
      .filter((item) => item.tabIndex >= 0 && !item.matches(':disabled') && item.getClientRects().length > 0)
    const initial = () => {
      const items = controls()
      // Preserve password autofocus; otherwise choose an input or the first
      // (close/cancel) control, not the destructive confirmation button.
      const target = items.find((item) => item.matches('input:not([type=checkbox]), textarea, [contenteditable="true"]')) || items[0] || node
      target.focus({ preventScroll: true })
    }
    if (!node.contains(document.activeElement)) initial()
    const trapTab = (event: KeyboardEvent) => {
      if (!topmost() || event.key !== 'Tab' || event.defaultPrevented) return
      const items = controls(), index = items.indexOf(document.activeElement as HTMLElement)
      event.preventDefault()
      if (!items.length) { node.focus(); return }
      const next = index < 0 ? event.shiftKey ? items.length - 1 : 0 : (index + (event.shiftKey ? -1 : 1) + items.length) % items.length
      items[next].focus()
    }
    const retainFocus = (event: FocusEvent) => {
      if (topmost() && !node.contains(event.target as Node)) initial()
    }
    document.addEventListener('keydown', trapTab)
    document.addEventListener('focusin', retainFocus)
    return () => {
      document.removeEventListener('keydown', trapTab)
      document.removeEventListener('focusin', retainFocus)
      if (previous?.isConnected) previous.focus({ preventScroll: true })
    }
  }, [])
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal" ref={dialog} tabIndex={-1} role="dialog" aria-modal="true" aria-label={title} style={{ width }}>
        <header className="modal-header">
          <h2>{title}</h2>
          {onClose && <button className="icon-button" type="button" aria-label="关闭" onClick={onClose}><Icon name="close" /></button>}
        </header>
        {children}
      </section>
    </div>
  )
}
