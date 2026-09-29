import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { HistoryEntry, JobUpdate } from '../types'
import TaskPane, { formatBytes } from './TaskPane'

const base: JobUpdate = {
  id: 'running', state: 'running', description: '复制 · A:/source → B:/target', message: '策略 direct · rsync',
  progress: 0, progressKnown: true, indeterminate: true, stage: 'running', method: 'direct · source-push · rsync',
  bytesTotal: 24 * 1024 * 1024, filesTotal: 1, startedAt: new Date(Date.now() - 5000).toISOString(),
}

describe('TaskPane status ledger', () => {
  afterEach(cleanup)

  it('keeps Pending and History visible at the same time and explains running semantics', () => {
    const pending: JobUpdate = { ...base, id: 'pending', state: 'pending', description: '移动 · queued', indeterminate: false }
    const history: HistoryEntry[] = [{ id: 'done', operation: '复制 · finished', success: true, message: '完成并校验', finishedAt: new Date().toISOString() }]
    render(<TaskPane jobs={[base, pending]} activity={[base]} history={history} activePane="left" onCommand={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.getByText('Pending')).toBeVisible()
    expect(screen.getByText('History')).toBeVisible()
    expect(screen.getByText('移动 · queued')).toBeVisible()
    expect(screen.getByText('复制 · finished')).toBeVisible()
    expect(screen.getByText('传输中')).toBeVisible()
    expect(screen.getByText('24.0 MiB')).toBeVisible()
  })

  it('renders every activity event instead of only the last update for a job', () => {
    const events: JobUpdate[] = [
      { ...base, message: '预检完成', observedAt: '2026-08-20T01:02:03Z' },
      { ...base, message: '策略 direct · rsync', observedAt: '2026-08-20T01:02:04Z' },
      { ...base, message: '安全传输中', progress: 0.5, indeterminate: false, observedAt: '2026-08-20T01:02:05Z' },
    ]
    render(<TaskPane jobs={[events[2]]} activity={events} history={[]} activePane="left" onCommand={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.getByText('预检完成')).toBeVisible()
    expect(screen.getAllByText('策略 direct · rsync').length).toBeGreaterThan(0)
    expect(screen.getByText('安全传输中')).toBeVisible()
    expect(screen.getByText('事件时间线 · 3')).toBeVisible()
  })

  it('formats byte totals compactly', () => {
    expect(formatBytes(24 * 1024 * 1024)).toBe('24.0 MiB')
  })

  it('replaces live transcript snapshots without duplicating lines and keeps failed output accessible', () => {
    const command = { ...base, description: '命令 · 控制机 · $ example', stage: 'command', method: '', message: '命令执行中', output: 'first-line\n' }
    const props = { jobs: [command], activity: [command], history: [], activePane: 'left' as const, onCommand: vi.fn(), onCancel: vi.fn() }
    const { rerender } = render(<TaskPane {...props} />)
    expect(screen.getByRole('log', { name: '命令输出' })).toHaveTextContent('first-line')
    expect(screen.queryByText('传输中')).not.toBeInTheDocument()
    rerender(<TaskPane {...props} jobs={[{ ...command, output: 'first-line\nsecond-line\n' }]} />)
    expect(screen.getByRole('log', { name: '命令输出' }).textContent?.match(/first-line/g)).toHaveLength(1)
    const history = [{ id: command.id, operation: command.description, success: false, state: 'failed' as const, output: 'first-line\nsecond-line\n', message: 'exit status 19', finishedAt: new Date().toISOString() }]
    rerender(<TaskPane {...props} jobs={[]} history={history} />)
    fireEvent.click(screen.getByRole('button', { name: `查看任务详情：${command.description}` }))
    expect(screen.getByRole('log', { name: '命令输出' })).toHaveTextContent('exit status 19')
    expect(screen.getByRole('log', { name: '命令输出' })).toHaveTextContent('second-line')
  })
})

it('does not display a made-up zero percent when the method has no byte progress', () => {
  render(<TaskPane jobs={[{ ...base, progressKnown: false, indeterminate: false }]} activity={[]} history={[]} activePane="left" onCommand={vi.fn()} onCancel={vi.fn()} />)
  expect(screen.queryByText('0%')).not.toBeInTheDocument()
  expect(screen.getByText('传输中')).toBeVisible()
  cleanup()
})
