import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
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

function phaseLabel(stage?: string): string {
  return ({ preflight: '预检与源清单', attempt: '尝试连接与传输', snapshot: '检查源文件', copy: '复制数据', verify: '校验内容与源变化', command: '命令执行中', cleanup: '检查与清理过期工作区', 'native-cp': '同机复制', 'native-mv': '同机移动', done: '完成' } as Record<string, string>)[stage || ''] || stage || ''
}

export default function TaskPane({ jobs, activity, history, activePane, onCommand, onCancel }: {
  jobs: JobUpdate[]
  activity: JobUpdate[]
  history: HistoryEntry[]
  activePane: 'left' | 'right'
  onCommand: (target: string, command: string) => Promise<boolean> | void
  onCancel: (id: string) => void
}) {
  const [target, setTarget] = useState('当前焦点')
  const [command, setCommand] = useState('')
  const [now, setNow] = useState(Date.now())
  const [viewedID, setViewedID] = useState<string>()
  const [view, setView] = useState<'output' | 'events'>()
  const logRef = useRef<HTMLDivElement>(null)
  const followOutput = useRef(true)
  const running = jobs.find((job) => job.state === 'running')
  const pending = jobs.filter((job) => job.state === 'pending')
  const output = useMemo(() => activity.filter((job) => job.message).slice(-200), [activity])
  const latest = jobs.filter((job) => job.output).at(-1)
  const selectedID = viewedID || running?.id || latest?.id || history.at(-1)?.id
  const selectedJob = jobs.find((job) => job.id === selectedID)
  const selectedHistory = history.find((item) => item.id === selectedID)
  const transcript = selectedJob?.output || selectedHistory?.output || ''
  const selectedDescription = selectedJob?.description || selectedHistory?.operation
  const selectedMethod = selectedJob?.method || selectedHistory?.method
  const selectedMessage = selectedJob?.message || selectedHistory?.message
  const visibleView = view || (transcript || selectedJob?.stage === 'command' ? 'output' : 'events')
  useEffect(() => {
    followOutput.current = true
    if (logRef.current) logRef.current.scrollTop = 0
  }, [selectedID, visibleView])
  useEffect(() => {
    if (visibleView === 'output' && followOutput.current && logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [transcript, selectedID, visibleView])
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
    void Promise.resolve(onCommand(target === '当前焦点' ? activePane === 'left' ? '左栏' : '右栏' : target, value)).then((success) => { if (success !== false) setCommand((current) => current.trim() === value ? '' : current) })
  }
  const unknownProgress = Boolean(running && (running.indeterminate || !running.progressKnown))
  const isCommand = running?.stage === 'command' || running?.description.startsWith('命令 · ')
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
        <div className={`progress-track ${unknownProgress ? 'indeterminate' : ''}`} aria-label={running ? unknownProgress ? '任务正在进行，当前阶段不提供完成百分比' : `任务进度 ${percentage}%` : '没有运行中的任务'}><span style={{ width: unknownProgress ? '32%' : `${percentage}%` }} /></div>
        <div className="running-meta">
          <strong>{running ? unknownProgress ? isCommand ? '执行中' : running.method ? '传输中' : '处理中' : `${percentage}%` : '—'}</strong>
          <span>{running?.bytesTotal ? `${transferred}${formatBytes(running.bytesTotal)}` : isCommand ? '完整行实时输出' : '暂无字节统计'}</span>
          <span>{running?.filesTotal ? `${running.filesDone || 0} / ${running.filesTotal} 文件` : ''}</span>
          <time>{running ? formatElapsed(running.startedAt, now) : '0:00'}</time>
        </div>
        <p title={running?.method || running?.message}>{running?.method || running?.message || '拖放或输入命令后，任务将在这里执行。'}</p>
        {running && <p>{phaseLabel(running.stage)}</p>}
      </section>
      <section className="output-block">
        <div className="output-tabs"><button type="button" aria-pressed={visibleView === 'output'} onClick={() => setView('output')}>输出与详情</button><button type="button" aria-pressed={visibleView === 'events'} onClick={() => setView('events')}>事件时间线 · {output.length}</button>{viewedID && <button type="button" onClick={() => { setViewedID(undefined); setView(undefined) }}>回到当前</button>}</div>
        {visibleView === 'output' ? <div className="task-output" ref={logRef} role="log" aria-label="命令输出" onScroll={(event) => { const node = event.currentTarget; followOutput.current = node.scrollHeight - node.scrollTop - node.clientHeight < 32 }}>
          {selectedDescription && <div className="output-detail"><strong>{selectedDescription}</strong><span>{selectedMethod}</span><span>{selectedMessage}</span></div>}
          <pre className="command-output">{transcript || (running?.id === selectedID && isCommand ? '等待完整输出行；未换行的尾部在命令结束时显示。' : '该任务没有命令输出；传输尝试请查看事件时间线。')}</pre>
        </div> : <div className="task-output" role="log" aria-label="任务事件">
          {output.length === 0 && <div className="empty-state">任务状态变化和策略尝试会逐条保留在这里</div>}
          {output.map((job, index) => <div className={`output-line ${job.state}`} key={`${job.id}-${index}-${job.observedAt}`}><time>{eventTime(job.observedAt || job.finishedAt || job.startedAt)}</time><b>{stateLabel(job.state)}</b><span>{job.message}</span></div>)}
        </div>}
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
              {history.length ? history.slice().reverse().map((item) => <button type="button" className={`history-row ${item.success ? 'success' : item.state === 'cancelled' ? 'cancelled' : 'failure'}`} key={item.id} aria-label={`查看任务详情：${item.operation}`} aria-pressed={viewedID === item.id} onClick={() => { setViewedID(item.id); setView('output') }}><Icon name={item.success ? 'check' : item.state === 'cancelled' ? 'stop' : 'alert'} /><div><strong title={item.operation}>{item.operation}</strong><span title={item.message}>{eventTime(item.finishedAt)} · {item.state === 'cancelled' ? '已取消 · ' : ''}{item.message}</span></div></button>) : <div className="empty-state">任务完成、失败或取消后会出现在这里</div>}
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
