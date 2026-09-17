import { describe, expect, it } from 'vitest'
import { parentPath } from './FilePane'
import { fileShortcut, resolveDropTarget, shouldNavigateOnBackspace } from './Workspace'

describe('cross-pane drop targeting', () => {
  it('drops on the exact directory row', () => {
    document.body.innerHTML = `<section class="file-pane" data-pane="right"><div data-drop-directory="/srv/archive"><span id="name">archive</span></div></section>`
    const under = document.getElementById('name')
    expect(resolveDropTarget(under, 'left', { left: '/tmp', right: '/srv' })).toEqual({ pane: 'right', directory: '/srv/archive' })
  })

  it('uses the open directory over empty space and rejects the source pane', () => {
    document.body.innerHTML = `<section class="file-pane" data-pane="right"><div id="empty"></div></section><section class="file-pane" data-pane="left"><div id="source"></div></section>`
    expect(resolveDropTarget(document.getElementById('empty'), 'left', { left: '/tmp', right: '/srv' })).toEqual({ pane: 'right', directory: '/srv' })
    expect(resolveDropTarget(document.getElementById('source'), 'left', { left: '/tmp', right: '/srv' })).toBeNull()
  })
})

describe('Backspace navigation scope', () => {
  it('navigates without requiring a selected row and preserves text editing', () => {
    document.body.innerHTML = `
      <div class="file-viewport"><div class="file-row" id="row">file</div></div>
      <input id="path" />
      <div class="terminal-host"><textarea id="terminal"></textarea></div>
      <div class="cm-editor"><div contenteditable="true" id="editor"></div></div>`
    const event = (target: EventTarget | null, key = 'Backspace') => ({ key, target, ctrlKey: false, altKey: false, metaKey: false })
    expect(shouldNavigateOnBackspace(event(document.body), false)).toBe(true)
    expect(shouldNavigateOnBackspace(event(document.getElementById('row')), false)).toBe(true)
    expect(shouldNavigateOnBackspace(event(document.getElementById('row'), 'Delete'), false)).toBe(false)
    expect(shouldNavigateOnBackspace(event(document.getElementById('path')), false)).toBe(false)
    expect(shouldNavigateOnBackspace(event(document.getElementById('terminal')), false)).toBe(false)
    expect(shouldNavigateOnBackspace(event(document.getElementById('editor')), false)).toBe(false)
    expect(shouldNavigateOnBackspace(event(document.body), true)).toBe(false)
  })

  it('calculates POSIX and Windows parents', () => {
    expect(parentPath('/tmp/powerlog')).toBe('/tmp')
    expect(parentPath('/')).toBe('/')
    expect(parentPath('C:\\Users\\demo')).toBe('C:\\Users')
    expect(parentPath('C:\\')).toBe('C:\\')
  })
})

describe('file shortcuts', () => {
  it('maps plain d/h only outside editors, terminals and dialogs', () => {
    document.body.innerHTML = `<div id="file"></div><input id="input" /><div class="terminal-host"><textarea id="terminal"></textarea></div>`
    const event = (key: string, target: EventTarget | null, repeat = false) => ({ key, target, repeat, ctrlKey: false, altKey: false, metaKey: false })
    expect(fileShortcut(event('d', document.getElementById('file')), false)).toBe('delete')
    expect(fileShortcut(event('H', document.getElementById('file')), false)).toBe('hash')
    expect(fileShortcut(event('d', document.getElementById('input')), false)).toBeNull()
    expect(fileShortcut(event('h', document.getElementById('terminal')), false)).toBeNull()
    expect(fileShortcut(event('d', document.getElementById('file')), true)).toBeNull()
    expect(fileShortcut(event('h', document.getElementById('file'), true), false)).toBeNull()
  })
})
