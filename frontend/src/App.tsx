import { useEffect, useState } from 'react'
import { api, onEvent } from './api'
import type { Bootstrap, Challenge, VaultStatus } from './types'
import Unlock from './components/Unlock'
import Workspace from './components/Workspace'
import Modal from './components/Modal'

export default function App() {
  const [status, setStatus] = useState<VaultStatus | null>(null)
  const [bootstrap, setBootstrap] = useState<Bootstrap | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [challenge, setChallenge] = useState<Challenge | null>(null)
  const [challengeValue, setChallengeValue] = useState('')
  const [challengeSave, setChallengeSave] = useState(false)

  useEffect(() => {
    api.vaultStatus().then(async (next) => {
      setStatus(next)
      if (next.unlocked) setBootstrap(await api.bootstrap())
    }).catch((reason) => setError(String(reason)))
    return onEvent('challenge', (next) => { setChallengeValue(''); setChallengeSave(false); setChallenge(next) })
  }, [])

  const run = async (operation: () => Promise<Bootstrap>) => {
    setBusy(true); setError('')
    try { setBootstrap(await operation()) }
    catch (reason) { setError(String(reason)) }
    finally { setBusy(false) }
  }
  const lock = async () => {
    await api.lock()
    setBootstrap(null)
    setStatus(await api.vaultStatus())
  }
  const resolve = async (accepted: boolean) => {
    if (!challenge) return
    await api.resolveChallenge(challenge.id, accepted, challengeValue, challengeSave)
    setChallenge(null)
  }

  if (!status) return <div className="app-loading"><span className="logo">df</span><p>{error || '正在加载安全工作区…'}</p></div>
  return <>
    {bootstrap?.unlocked ? <Workspace initial={bootstrap} onLock={() => void lock()} /> : <Unlock status={status} busy={busy} error={error} onUnlock={(password) => void run(() => api.unlock(password))} onCreate={(hint, password, confirm) => void run(() => api.createVault(hint, password, confirm))} />}
    {challenge && <Modal title={challenge.title}>
      <div className="challenge-body"><p>{challenge.message}</p>{challenge.secret && <label>认证密码<input autoFocus type="password" value={challengeValue} onChange={(event) => setChallengeValue(event.target.value)} /></label>}{challenge.allowSave && <label className="checkbox"><input type="checkbox" checked={challengeSave} onChange={(event) => setChallengeSave(event.target.checked)} />保存到加密保险库</label>}</div>
      <footer className="modal-footer"><span>连接会一直等待本次决定。</span><button className="secondary-button" onClick={() => void resolve(false)}>取消</button><button className="primary-button" onClick={() => void resolve(true)}>继续</button></footer>
    </Modal>}
  </>
}
