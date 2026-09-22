import type { ReactNode } from 'react'
import Icon from './Icon'

export default function Modal({ title, children, onClose, width = 520 }: { title: string; children: ReactNode; onClose?: () => void; width?: number }) {
  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal" role="dialog" aria-modal="true" aria-label={title} style={{ width }}>
        <header className="modal-header">
          <h2>{title}</h2>
          {onClose && <button className="icon-button" type="button" aria-label="关闭" onClick={onClose}><Icon name="close" /></button>}
        </header>
        {children}
      </section>
    </div>
  )
}
