import { useEffect, useRef, useState } from 'react'
import { api, onEvent } from '../api'
import { isFinished, mergeJobUpdates } from '../jobState'
import type { Bootstrap, ConnectionOverview, ConnectionRow, JobUpdate } from '../types'

export default function ConnectionsPanel({ dirty, revision, onChanged }: { dirty: boolean; revision?: string; onChanged: (value?: Bootstrap) => Promise<void> }) {
  const [overview, setOverview] = useState<ConnectionOverview>()
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [target, setTarget] = useState('')
  const [sort, setSort] = useState('name')
  const [results, setResults] = useState<Record<string, JobUpdate>>({})
  const testIDs = useRef<Record<string, string>>({})
  const activeTests = useRef<Record<string, string>>({})
  const refreshID = useRef(0)
  const alive = useRef(true)
  const refresh = async () => {
    const request = ++refreshID.current
    const value = await api.connectionOverview()
    if (alive.current && request === refreshID.current) {
      setOverview(value)
      setTarget((current) => value.hosts.some((host) => host.id === current && !host.disabled) ? current : '')
    }
  }
  const acceptResult = (key: string, job: JobUpdate) => {
    if (!alive.current || activeTests.current[key] !== job.id) return
    setResults((current) => {
      const old = current[key]
      return { ...current, [key]: mergeJobUpdates(old?.id === job.id ? [old] : [], [job])[0] }
    })
  }
  useEffect(() => {
    alive.current = true
    const unsubscribe = onEvent('job:update', (job) => {
      const row = testIDs.current[job.id]
      if (!row || activeTests.current[row] !== job.id) return
      acceptResult(row, job)
      if (isFinished(job)) void refresh().catch((reason) => { if (alive.current) setError(String(reason)) })
    })
    return () => { alive.current = false; refreshID.current++; unsubscribe() }
  }, [])
  useEffect(() => { void refresh().catch((reason) => { if (alive.current) setError(String(reason)) }) }, [revision])
  const action = async (run: () => Promise<void>) => {
    setBusy(true); setError('')
    try { await run() } catch (reason) { if (alive.current) setError(String(reason)) }
    finally { if (alive.current) setBusy(false) }
  }
  const policy = (kind: 'ssh' | 'socks', row: ConnectionRow, disabled: boolean, allowed: boolean) => action(async () => {
    if (!overview) return
    const next = await api.setConnectionPolicy(kind, row.id, disabled, allowed, overview.revision)
    await onChanged(next)
    await refresh()
  })
  const test = (kind: 'ssh' | 'socks', row: ConnectionRow) => action(async () => {
    if (!overview) return
    const id = await api.queueConnectionTest(kind, row.id, kind === 'socks' ? target : '', overview.revision)
    const key = kind + ':' + row.id
    testIDs.current[id] = key
    activeTests.current[key] = id
    // Subscribe before admission; snapshot repairs even a very fast result
    // emitted before the RPC returned its job ID. No network polling here.
    const snapshot = await api.jobSnapshot()
    const result = snapshot.find((job) => job.id === id)
    if (result) acceptResult(key, result)
    await refresh()
  })
  const ordered = (kind: 'ssh' | 'socks', rows: ConnectionRow[]) => rows.slice().sort((a, b) => {
    if (sort === 'success' && a.lastSuccess !== b.lastSuccess) return (Date.parse(b.lastSuccess) || 0) - (Date.parse(a.lastSuccess) || 0)
    if (sort === 'rtt' && kind === 'socks') {
      const left = a.lastSuccess ? a.lastRTT : Infinity, right = b.lastSuccess ? b.lastRTT : Infinity
      if (left !== right) return left - right
    }
    return a.name.localeCompare(b.name)
  })
  const renderRows = (kind: 'ssh' | 'socks', rows: ConnectionRow[]) => ordered(kind, rows).map((row) => {
    const result = results[kind + ':' + row.id]
    const testing = result?.state === 'pending' || result?.state === 'running'
    return <article className="connection-row" aria-label={row.name} key={row.id}>
      <div className="connection-identity"><strong>{row.name}</strong><span>{row.disabled ? '已停用' : kind === 'ssh' ? !row.relayAllowed ? '仅端点，不作跳板' : row.relayReady ? '已有登录或成功中转记录' : '首次作跳板前，请先验证登录' : row.lastSuccess ? `上次成功传输 · ${row.lastRTT} ms（当时发起端）` : '尚无成功传输记录'}</span></div>
      <div className="connection-actions">
        <label><input type="checkbox" checked={!row.disabled} disabled={dirty || busy} onChange={(event) => void policy(kind, row, !event.target.checked, row.relayAllowed)} />启用</label>
        {kind === 'ssh' && <label><input type="checkbox" checked={row.relayAllowed} disabled={dirty || busy || row.disabled} onChange={(event) => void policy(kind, row, row.disabled, event.target.checked)} />允许作跳板</label>}
        <button type="button" className="secondary-button" disabled={dirty || busy || row.disabled || testing || (kind === 'socks' && !target)} onClick={() => void test(kind, row)}>{testing ? result.state === 'pending' ? '已排队' : '测试中' : kind === 'ssh' ? '仅测试此会话' : '经此代理测试'}</button>
        {testing && <button type="button" className="secondary-button" onClick={() => void action(() => api.cancelJob(result.id))}>取消测试</button>}
      </div>
      {result && <p className={`connection-result ${result.state}`} role="status">{({ pending: '等待', running: '进行中', succeeded: '成功', failed: '失败', cancelled: '已取消' })[result.state]} · {result.message || result.description}</p>}
    </article>
  })
  return <section className="connections-panel" aria-label="连接状态与路由缓存">
    <div className="connections-toolbar"><span>这里仅显示已有记录。打开或刷新不会探测网络。</span><select aria-label="连接排序" value={sort} onChange={(event) => setSort(event.target.value)}><option value="name">按名称</option><option value="success">最近成功记录</option><option value="rtt">SOCKS 上次延迟</option></select><button type="button" className="secondary-button" disabled={busy} onClick={() => void action(refresh)}>刷新记录</button></div>
    {dirty && <p className="connection-notice">文本有未保存修改。请先“验证并保存”，再进行测试或更改状态。</p>}
    {error && <p className="connection-error" role="alert">{error}</p>}
    <p className="connection-notice">测试从控制机出发，只连接点选的完整 SSH 路由；SOCKS 测试必须选择一个已保存的 SSH 目标，不访问公共检测网站、不遍历池。测试总耗时包含认证/确认，不作为 TCP 延迟排名。</p>
    <h3>SSH 会话</h3>
    {overview ? overview.hosts.length ? renderRows('ssh', overview.hosts) : <p>在 Markdown 的 #主机 中添加会话。</p> : <p>正在读取…</p>}
    <h3>SOCKS 池</h3>
    <label className="socks-test-target">所选代理的测试目标<select aria-label="SOCKS 测试 SSH 目标" value={target} onChange={(event) => setTarget(event.target.value)}><option value="">先选一个 SSH 会话</option>{overview?.hosts.filter((host) => !host.disabled).map((host) => <option key={host.id} value={host.id}>{host.name}</option>)}</select></label>
    {overview && (overview.socks.length ? renderRows('socks', overview.socks) : <p>在 Markdown 的 #socks池 中添加代理。</p>)}
    <h3>记住的跳板路线</h3>
    <p className="connection-notice">记录来自曾成功的连接或传输，供需要跳板时优先尝试，不证明两远端当前互通；失效才探测其他已登录会话。设为“仅端点”的主机不会作跳板。策略变更仅用于新任务，已入队任务沿用其配置快照。</p>
    {overview?.relays.length ? overview.relays.map((relay) => <article className="connection-row" key={`${relay.endpointAID}:${relay.endpointBID}`}><div className="connection-identity"><strong>{relay.endpointA} ↔ {relay.endpointB}</strong><span>通过 {relay.relay} · {relay.lastSuccess ? new Date(relay.lastSuccess).toLocaleString() : '时间未记录'}</span></div><button type="button" className="secondary-button" disabled={dirty || busy} onClick={() => void action(async () => { await api.clearRelayCache(relay.endpointAID, relay.endpointBID, overview.revision); await onChanged(); await refresh() })}>忘记此路线</button></article>) : <p>尚无跳板路线记录；登录测试不等于传输成功。</p>}
  </section>
}
