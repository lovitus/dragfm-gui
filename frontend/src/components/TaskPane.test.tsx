import { cleanup, render, screen } from '@testing-library/react'
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
})
