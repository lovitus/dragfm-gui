import type { JobUpdate } from './types'

export const isFinished = (job: JobUpdate): boolean => ['succeeded', 'failed', 'cancelled'].includes(job.state)

export function mergeJobUpdates(previous: JobUpdate[], incoming: JobUpdate[]): JobUpdate[] {
  const byID = new Map(previous.map((job) => [job.id, job]))
  for (const update of incoming) {
    const old = byID.get(update.id)
    if (old && (update.revision || 0) <= (old.revision || 0) && (old.revision || update.revision)) continue
    if (old && isFinished(old) && !isFinished(update)) continue
    byID.set(update.id, update)
  }
  const all = Array.from(byID.values())
  const active = all.filter((job) => !isFinished(job))
  const finished = all.filter(isFinished).sort((a, b) => (a.finishedAt || '').localeCompare(b.finishedAt || '')).slice(-500)
  return [...active, ...finished]
}
