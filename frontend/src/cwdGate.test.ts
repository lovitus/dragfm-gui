import { expect, it } from 'vitest'
import { CWDGate } from './cwdGate'

it('keeps the latest user intent through listing and delayed distinct prompts', () => {
  const gate = new CWDGate()
  gate.begin()
  expect(gate.accept('/old')).toBe(false)
  gate.resolve('/new', '/old')
  expect(gate.accept('/older')).toBe(false)
  expect(gate.accept('/old')).toBe(false)
  expect(gate.accept('/new')).toBe(true)
  expect(gate.accept('/elsewhere')).toBe(true)
  gate.begin()
  gate.clear()
  expect(gate.accept('/old')).toBe(true)
})
it('does not wait for a cd when the user selected the same normalized directory', () => {
  const gate = new CWDGate()
  gate.begin(); gate.resolve('/same', '/same')
  expect(gate.accept('/next')).toBe(true)
})
