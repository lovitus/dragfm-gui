import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from './api'

describe('desktop bridge fails closed', () => {
  afterEach(() => { delete window.go })
  it('does not pretend a missing native bridge is an unlocked vault', async () => {
    delete window.go
    await expect(api.vaultStatus()).rejects.toThrow('no operation was performed')
  })
  it('does not simulate a successful transfer', async () => {
    delete window.go
    await expect(api.queueCommand('控制机', 'anything')).rejects.toThrow('QueueCommand')
  })
  it('uses the native snapshot method', async () => {
    const snapshot = vi.fn(async () => [])
    window.go = { webgui: { App: { JobSnapshot: snapshot } } }
    await expect(api.jobSnapshot()).resolves.toEqual([])
    expect(snapshot).toHaveBeenCalledOnce()
  })
})
