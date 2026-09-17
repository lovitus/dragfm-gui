import { useEffect, useMemo, useState, type FormEvent } from 'react'
import type { HistoryEntry, JobUpdate } from '../types'
import Icon from './Icon'

export function formatBytes(value = 0): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const unit = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  const amount = value / 1024 ** unit
  return `${amount >= 100 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`
}

function formatElapsed(startedAt: string | undefined, now: number): string {
  if (!startedAt) return '0:00'
  const seconds = Math.max(0, Math.floor((now - new Date(startedAt).getTime()) / 1000))
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const rest = seconds % 60
  return hours ? `${hours}:${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}` : `${minutes}:${String(rest).padStart(2, '0')}`
}

function eventTime(value?: string): string {
  if (!value) return '--:--:--'
  return new Date(value).toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function stateLabel(state: JobUpdate['state']): string {
  return { pending: '等待', running: '执行', succeeded: '完成', failed: '失败', cancelled: '取消' }[state]
}

export default function TaskPane({ jobs, activity, history, activePane, onCommand, onCancel }: {
  jobs: JobUpdate[]
  activity: JobUpdate[]
  history: HistoryEntry[]
  activePane: 'left' | 'right'
  onCommand: (target: string, command: string) => void
  onCancel: (id: string) => void
}) {
  const [target, setTarget] = useState('当前焦点')
  const [command, setCommand] = useState('')
  const [now, setNow] = useState(Date.now())
  const running = jobs.find((job) => job.state === 'running')
  const pending = jobs.filter((job) => job.state === 'pending')
  const output = useMemo(() => activity.filter((job) => job.message).slice(-200), [activity])
  useEffect(() => {
    if (!running) return
    setNow(Date.now())
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [running?.id])
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const value = command.trim()
    if (!value) return
    onCommand(target === '当前焦点' ? activePane === 'left' ? '左栏' : '右栏' : target, value)
    setCommand('')
  }
  const percentage = running?.progressKnown ? Math.round(Math.max(0, Math.min(1, running.progress)) * 100) : 0
  const transferred = running?.bytesDone ? `${formatBytes(running.bytesDone)} / ` : ''
  return (
    <aside className="task-pane">
      <header className="task-header">
        <div><span className={`status-dot ${running ? 'busy' : ''}`} />{running ? '正在执行' : '任务空闲'}</div>
        <span>等待 {pending.length} · 历史 {history.length}</span>
      </header>
      <section className="running-block">
        <div className="running-line"><strong title={running?.description}>{running?.description || '没有运行中的任务'}</strong>{running && <button className="icon-button danger" title="取消任务" onClick={() => onCancel(running.id)}><Icon name="stop" /></button>}</div>
        <div className={`progress-track ${running?.indeterminate ? 'indeterminate' : ''}`} aria-label={running ? running.indeterminate ? '传输正在进行，当前方法不提供字节进度' : `传输进度 ${percentage}%` : '没有运行中的任务'}><span style={{ width: running?.indeterminate ? '32%' : `${percentage}%` }} /></div>
        <div className="running-meta">
          <strong>{running ? running.indeterminate ? '传输中' : `${percentage}%` : '—'}</strong>
          <span>{running?.bytesTotal ? `${transferred}${formatBytes(running.bytesTotal)}` : '等待传输统计'}</span>
          <span>{running?.filesTotal ? `${running.filesDone || 0} / ${running.filesTotal} 项` : ''}</span>
          <time>{running ? formatElapsed(running.startedAt, now) : '0:00'}</time>
        </div>
        <p title={running?.message}>{running?.method || running?.stage || running?.message || '拖放或输入命令后，任务将在这里执行。'}</p>
      </section>
      <section className="output-block">
        <div className="section-label">事件时间线 · {output.length}</div>
        <div className="task-output" role="log">
          {output.length === 0 && <div className="empty-state">任务状态变化和策略尝试会逐条保留在这里</div>}
          {output.map((job, index) => <div className={`output-line ${job.state}`} key={`${job.id}-${index}-${job.observedAt}`}><time>{eventTime(job.observedAt || job.finishedAt || job.startedAt)}</time><b>{stateLabel(job.state)}</b><span>{job.message}</span></div>)}
        </div>
      </section>
      <section className="queue-block">
        <div className="queue-sections">
          <section className="queue-section">
            <header><strong>Pending</strong><span>{pending.length}</span></header>
            <div className="queue-list pending-list">
              {pending.length ? pending.map((job) => <div className="queue-row" key={job.id}><span title={job.description}>{job.description}</span><button className="icon-button" title="取消等待任务" onClick={() => onCancel(job.id)}><Icon name="close" /></button></div>) : <div className="empty-state">当前没有等待任务；正在执行的任务不计入 Pending</div>}
            </div>
          </section>
          <section className="queue-section history-section">
            <header><strong>History</strong><span>{history.length}</span></header>
            <div className="queue-list history-list">
              {history.length ? history.slice().reverse().map((item) => <div className={`history-row ${item.success ? 'success' : 'failure'}`} key={item.id}><Icon name={item.success ? 'check' : 'alert'} /><div><strong title={item.operation}>{item.operation}</strong><span title={item.message}>{eventTime(item.finishedAt)} · {item.message}</span></div></div>) : <div className="empty-state">任务完成、失败或取消后会出现在这里</div>}
            </div>
          </section>
        </div>
      </section>
      <form className="command-bar" onSubmit={submit}>
        <select value={target} onChange={(event) => setTarget(event.target.value)} aria-label="命令目标"><option>当前焦点</option><option>左栏</option><option>右栏</option><option>控制机</option></select>
        <div className="command-input"><span>$</span><input value={command} onChange={(event) => setCommand(event.target.value)} placeholder="输入命令，回车入队" /></div>
      </form>
    </aside>
  )
}
