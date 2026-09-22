import { describe, expect, it } from 'vitest'
import { mergeJobUpdates } from './jobState'
import type { JobUpdate } from './types'

const job: JobUpdate = { id: 'job', revision: 2, state: 'running', description: 'copy', message: '', progress: 0, progressKnown: false, indeterminate: true }

describe('authoritative job snapshots', () => {
  it('recovers missing running and pending events', () => {
    const result = mergeJobUpdates([], [job, { ...job, id: 'next', revision: 3, state: 'pending' }])
    expect(result.map((item) => item.state)).toEqual(['running', 'pending'])
  })
  it('does not resurrect a job when an older snapshot arrives after completion', () => {
    const finished = { ...job, revision: 4, state: 'succeeded' as const }
    expect(mergeJobUpdates([finished], [job])).toEqual([finished])
  })
  it('keeps live updates that raced with a snapshot', () => {
    const newer = { ...job, revision: 5, progress: .5, progressKnown: true }
    expect(mergeJobUpdates([newer], [job])).toEqual([newer])
  })
  it('bounds history without evicting pending/running tasks', () => {
    const finished = Array.from({ length: 600 }, (_, index): JobUpdate => ({ ...job, id: String(index), state: 'succeeded', finishedAt: new Date(index).toISOString() }))
    const result = mergeJobUpdates([], [job, ...finished])
    expect(result).toHaveLength(501)
    expect(result[0]).toEqual(job)
    expect(result.some((item) => item.id === '0')).toBe(false)
  })
})
