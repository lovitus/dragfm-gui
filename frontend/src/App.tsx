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
  const [challenges, setChallenges] = useState<Challenge[]>([])
  const challenge = challenges[0]
  const [challengeValue, setChallengeValue] = useState('')
  const [challengeSave, setChallengeSave] = useState(false)
  const [challengeBusy, setChallengeBusy] = useState(false)
  const [challengeError, setChallengeError] = useState('')

  useEffect(() => {
    let disposed = false
    api.vaultStatus().then(async (next) => {
      if (disposed) return
      setStatus(next)
      if (next.unlocked) {
        const state = await api.bootstrap()
        if (!disposed) setBootstrap(state)
      }
    }).catch((reason) => { if (!disposed) setError(String(reason)) })
    const stopChallenge = onEvent('challenge', (next) => {
      setChallenges((queue) => queue.some((item) => item.id === next.id) ? queue : [...queue, next])
    })
    const stopLock = onEvent('locked', () => { setChallenges([]); setBootstrap(null) })
    return () => { disposed = true; stopChallenge(); stopLock() }
  }, [])

  useEffect(() => {
    setChallengeValue(''); setChallengeSave(false); setChallengeError('')
  }, [challenge?.id])

  const run = async (operation: () => Promise<Bootstrap>) => {
    setBusy(true); setError('')
    try { setBootstrap(await operation()) }
    catch (reason) { setError(String(reason)) }
    finally { setBusy(false) }
  }
  const lock = async () => {
    try { await api.lock() }
    catch (reason) { setError(String(reason)) }
    finally {
      // The backend clears secrets even when persisting history fails.
      setBootstrap(null); setChallenges([])
      try { setStatus(await api.vaultStatus()) }
      catch (reason) { setError(String(reason)) }
    }
  }
  const resolve = async (accepted: boolean) => {
    if (!challenge || challengeBusy) return
    setChallengeBusy(true); setChallengeError('')
    try {
      await api.resolveChallenge(challenge.id, accepted, challengeValue, challengeSave)
      setChallenges((queue) => queue.filter((item) => item.id !== challenge.id))
    } catch (reason) { setChallengeError(String(reason)) }
    finally { setChallengeBusy(false) }
  }

  if (!status) return <div className="app-loading"><span className="logo">df</span><p>{error || '正在加载安全工作区…'}</p></div>
  return <>
    {bootstrap?.unlocked ? <Workspace initial={bootstrap} challengeOpen={Boolean(challenge)} onLock={() => void lock()} /> : <Unlock status={status} busy={busy} error={error} onUnlock={(password) => void run(() => api.unlock(password))} onCreate={(hint, password, confirm) => void run(() => api.createVault(hint, password, confirm))} />}
    {challenge && <Modal title={challenge.title}>
      <div className="challenge-body"><p>{challenge.message}</p>{challenge.secret && <label>认证密码<input autoFocus type="password" value={challengeValue} onChange={(event) => setChallengeValue(event.target.value)} /></label>}{challenge.allowSave && <label className="checkbox"><input type="checkbox" checked={challengeSave} onChange={(event) => setChallengeSave(event.target.checked)} />保存到加密保险库</label>}{challengeError && <p role="alert">{challengeError}</p>}</div>
      <footer className="modal-footer"><span>连接正在等待本次决定。{challenges.length > 1 ? `还有 ${challenges.length - 1} 个确认。` : ''}</span><button className="secondary-button" disabled={challengeBusy} onClick={() => void resolve(false)}>取消</button><button className="primary-button" disabled={challengeBusy} onClick={() => void resolve(true)}>继续</button></footer>
    </Modal>}
  </>
}
