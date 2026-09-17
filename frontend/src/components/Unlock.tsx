import { useState, type FormEvent } from 'react'
import type { VaultStatus } from '../types'
import Icon from './Icon'

interface Props {
  status: VaultStatus
  busy: boolean
  error: string
  onUnlock: (password: string) => void
  onCreate: (hint: string, password: string, confirm: string) => void
}

export default function Unlock({ status, busy, error, onUnlock, onCreate }: Props) {
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [hint, setHint] = useState('')
  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (status.exists) onUnlock(password)
    else onCreate(hint, password, confirm)
  }
  return (
    <main className="unlock-screen">
      <section className="unlock-panel">
        <div className="brand-mark"><Icon name="terminal" /></div>
        <div className="unlock-copy">
          <p className="eyebrow">SECURE FILE WORKSPACE</p>
          <h1>dragfm</h1>
          <p>双端点文件管理、真实 PTY 和安全传输队列。</p>
        </div>
        <form onSubmit={submit}>
          {!status.exists && (
            <label>主密码提示<input autoFocus value={hint} onChange={(event) => setHint(event.target.value)} placeholder="必填；不要填写密码本身" /></label>
          )}
          {status.exists && <div className="hint"><span>提示</span>{status.hint || '未设置'}</div>}
          <label>主密码<input autoFocus={status.exists} type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="current-password" /></label>
          {!status.exists && <label>确认主密码<input type="password" value={confirm} onChange={(event) => setConfirm(event.target.value)} autoComplete="new-password" /></label>}
          {error && <div className="form-error"><Icon name="alert" />{error}</div>}
          <button className="primary-button unlock-button" type="submit" disabled={busy}>{busy ? '处理中…' : status.exists ? '解锁工作区' : '创建加密保险库'}</button>
        </form>
        <footer>Argon2id · XChaCha20-Poly1305 · 凭据加密保存</footer>
      </section>
    </main>
  )
}
