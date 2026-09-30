import { useEffect, useRef, useState } from 'react'
import { Compartment, EditorState } from '@codemirror/state'
import { Decoration, type DecorationSet, EditorView, ViewPlugin, type ViewUpdate, WidgetType, drawSelection, dropCursor, highlightActiveLine, highlightActiveLineGutter, highlightSpecialChars, keymap, lineNumbers, rectangularSelection } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { api } from '../api'
import type { Bootstrap, ConfigTexts } from '../types'
import Icon from './Icon'
import Modal from './Modal'
import ConnectionsPanel from './ConnectionsPanel'

class MaskWidget extends WidgetType {
  toDOM(): HTMLElement {
    const span = document.createElement('span')
    span.className = 'secret-mask'
    span.textContent = '••••••'
    span.title = '密码已隐藏；点击右上角“显示密码”查看'
    return span
  }
}

export type SecretRange = { from: number; to: number }

export function secretRanges(document: string): SecretRange[] {
  const ranges: SecretRange[] = []
  let maskMetadataValue = false
  let socksSection = false
  const inline = /:(?:"(?:\\.|[^"])*"|'(?:\\.|[^'])*'|[^\s@]+)@/g
  let offset = 0
  for (const text of document.split('\n')) {
    const trimmed = text.trim()
    if (trimmed.startsWith('###')) {
      const field = trimmed.slice(3).trim()
      maskMetadataValue = field === 'sudo密码' || field === 'root密码' || field === '口令' || field === '待核对旧密码'
      offset += text.length + 1
      continue
    }
    if (trimmed.startsWith('#')) {
      if (!trimmed.startsWith('##')) socksSection = trimmed === '#socks池'
      maskMetadataValue = false
      offset += text.length + 1
      continue
    }
    if (maskMetadataValue && trimmed !== '') {
      const leading = text.length - text.trimStart().length
      const trailing = text.trimEnd().length
      ranges.push({ from: offset + leading, to: offset + trailing })
      maskMetadataValue = false
    } else if (socksSection) {
      // Shorthand passwords may contain literal @, %, # and colons. The
      // final @ separates the address; the SSH-oriented regex stops too early.
      const start = text.indexOf('socks5://')
      const colon = text.indexOf(':', start >= 0 ? start + 'socks5://'.length : 0)
      const at = text.lastIndexOf('@')
      if (colon >= 0 && at > colon) ranges.push({ from: offset + colon + 1, to: offset + at })
    } else {
      inline.lastIndex = 0
      let match: RegExpExecArray | null
      while ((match = inline.exec(text)) !== null) {
        ranges.push({ from: offset + match.index + 1, to: offset + match.index + match[0].length - 1 })
      }
    }
    offset += text.length + 1
  }
  return ranges
}

function secretMasks(view: EditorView): DecorationSet {
  return Decoration.set(secretRanges(view.state.doc.toString()).map(({ from, to }) => Decoration.replace({ widget: new MaskWidget() }).range(from, to)), true)
}

const secretMaskPlugin = ViewPlugin.fromClass(class {
  decorations: DecorationSet
  constructor(view: EditorView) { this.decorations = secretMasks(view) }
  update(update: ViewUpdate) { if (update.docChanged) this.decorations = secretMasks(update.view) }
}, { decorations: (plugin) => plugin.decorations })

function CodeEditor({ value, onChange, revealSecrets, readOnly }: { value: string; onChange: (value: string) => void; revealSecrets: boolean; readOnly: boolean }) {
  const host = useRef<HTMLDivElement>(null)
  const editor = useRef<EditorView | null>(null)
  const editing = useRef(new Compartment())
  const change = useRef(onChange)
  change.current = onChange
  useEffect(() => {
    if (!host.current) return
    const view = new EditorView({
      parent: host.current,
      state: EditorState.create({
        doc: value,
        extensions: [
          lineNumbers(), highlightActiveLineGutter(), highlightSpecialChars(), history(), drawSelection(), dropCursor(), rectangularSelection(), highlightActiveLine(),
          keymap.of([...defaultKeymap, ...historyKeymap]),
          editing.current.of(EditorState.readOnly.of(readOnly)),
          ...(revealSecrets ? [] : [secretMaskPlugin]),
          EditorView.updateListener.of((update) => { if (update.docChanged) change.current(update.state.doc.toString()) }),
          EditorView.theme({
            '&': { height: '100%', fontSize: '12px', backgroundColor: 'var(--editor-bg)', color: 'var(--text)' },
            '.cm-scroller': { overflow: 'auto', fontFamily: 'var(--mono)', lineHeight: '1.55' },
            '.cm-content': { minWidth: 'max-content', padding: '10px 0', caretColor: 'var(--accent)' },
            '.cm-line': { padding: '0 14px' },
            '.cm-gutters': { backgroundColor: 'var(--editor-gutter)', color: 'var(--muted)', borderRight: '1px solid var(--border)' },
            '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'var(--active-line)' },
            '.cm-selectionBackground, &.cm-focused .cm-selectionBackground': { backgroundColor: 'var(--selection) !important' },
          }, { dark: false }),
          EditorView.contentAttributes.of({ 'aria-label': 'Markdown 连接与私钥配置', spellcheck: 'false' }),
        ],
      }),
    })
    editor.current = view
    // A cleanup transaction would invoke onChange('') and erase the parent
    // draft when toggling password visibility. Destroy without editing it.
    return () => { editor.current = null; view.destroy() }
    // Initial value is loaded once; subsequent edits belong to CodeMirror.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  useEffect(() => { editor.current?.dispatch({ effects: editing.current.reconfigure(EditorState.readOnly.of(readOnly)) }) }, [readOnly])
  return <div className="code-editor" ref={host} />
}

export default function ConfigEditor({ onClose, onSaved }: { onClose: () => void; onSaved: (bootstrap: Bootstrap, close?: boolean) => void }) {
  const [texts, setTexts] = useState<ConfigTexts | null>(null)
  const [savedText, setSavedText] = useState('')
  const [editorRevision, setEditorRevision] = useState(0)
  const [tab, setTab] = useState<'markdown' | 'connections'>('markdown')
  const [openedConnections, setOpenedConnections] = useState(false)
  const [discard, setDiscard] = useState(false)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [revealSecrets, setRevealSecrets] = useState(false)
  const draft = useRef<ConfigTexts | null>(null)
  const saved = useRef('')
  const edits = useRef(0)
  const loadID = useRef(0)
  const alive = useRef(true)
  const reload = async (discardDraft = false) => {
    if (!discardDraft && draft.current && draft.current.markdown !== saved.current) {
      setError('已保存配置发生变化；当前草稿已保留。请复制需要的修改，再重新载入并合并。')
      return
    }
    const request = ++loadID.current, before = edits.current
    const next = await api.getConfigTexts()
    if (!alive.current || request !== loadID.current) return
    if (before !== edits.current) {
      setError('载入期间有新的文本修改，已保留草稿。需要丢弃时请再次确认重新载入。')
      return
    }
    draft.current = next; saved.current = next.markdown
    setTexts(next); setSavedText(next.markdown); setEditorRevision((value) => value + 1); setDiscard(false); setError('')
  }
  useEffect(() => {
    alive.current = true
    void reload().catch((reason) => { if (alive.current) setError(String(reason)) })
    return () => { alive.current = false; loadID.current++ }
  }, [])
  const dirty = texts !== null && texts.markdown !== savedText
  const save = async () => {
    if (!texts) return
    setSaving(true); setError('')
    try { onSaved(await (texts.revision ? api.saveConfigTexts(texts.markdown, texts.revision) : api.saveConfigTexts(texts.markdown))) }
    catch (reason) { setError(String(reason)) }
    finally { setSaving(false) }
  }
  return (
    <Modal title="Markdown 配置" onClose={onClose} width={1180}>
      <nav className="config-tabs" aria-label="配置页面"><button type="button" aria-pressed={tab === 'markdown'} onClick={() => setTab('markdown')}>文本配置{dirty ? ' · 未保存' : ''}</button><button type="button" aria-pressed={tab === 'connections'} onClick={() => { setOpenedConnections(true); setTab('connections') }}>连接、池与缓存</button><button type="button" disabled={saving} onClick={() => { if (dirty) setDiscard(true); else void reload().catch((reason) => setError(String(reason))) }}>重新载入已保存配置</button></nav>
      {discard && <div className="config-discard" role="alert">重新载入会丢弃当前未保存的文本修改。<button type="button" onClick={() => void reload(true).catch((reason) => setError(String(reason)))}>丢弃草稿并载入</button><button type="button" onClick={() => setDiscard(false)}>保留草稿</button></div>}
      <div className="config-tab-content" hidden={tab !== 'markdown'}>
      <div className="config-security-bar"><span>私钥正文保持可见；SSH、SOCKS、sudo、root 和私钥口令默认遮罩。</span><button type="button" className="secondary-button" onClick={() => setRevealSecrets((shown) => !shown)}>{revealSecrets ? '隐藏密码' : '显示密码'}</button></div>
      <div className="config-markdown-layout">
        <div className="config-content">
          {!texts && !error && <div className="empty-state">正在解密配置…</div>}
          {texts && <CodeEditor key={`${editorRevision}:${revealSecrets ? 'revealed' : 'masked'}`} value={texts.markdown} revealSecrets={revealSecrets} readOnly={saving} onChange={(markdown) => {
            edits.current++
            if (draft.current) draft.current = { ...draft.current, markdown }
            setTexts(draft.current)
          }} />}
        </div>
        <aside className="config-reference">
          <strong>格式参考</strong>
          <p><code>##</code> 后面是名称，下一行是内容。不要缩进，不需要空行。</p>
          <pre>{`#主机
##主机名1
uhome:"EXAMPLE_PASSWORD_1"@192.0.2.10:41122,/bin/bash
##主机名2
uhome:"EXAMPLE_PASSWORD_2"@192.0.2.20:41122  uhome:"EXAMPLE_PASSWORD_3"@192.0.2.30:41122   uhome@192.0.2.40:41122 --keys ",,keyname1" ,/bin/zsh
##主机名3我不写shell你用登录默认的
demo:EXAMPLE_PASSWORD@192.0.2.50:22
#私钥
##keyname1
abc...
#socks池
##socksname1
user1:EXAMPLE_PROXY_PASSWORD@192.0.2.60:1080`}</pre>
          <p>主机可写一跳或整条 FlySSH 多跳路由。末尾的 <code>,/bin/zsh</code> 是指定 Shell；省略时使用登录默认 Shell。<code>--keys</code> 引用本配置中同名私钥。</p>
          <p>SOCKS 简写的密码按字面输入，<code>% # @</code> 不需要编码。旧配置迁移后的 <code>socks5://</code> 前缀表示保留 URL 编码语义。</p>
          <p><code>###root私钥</code> 每行写一个本保险库的私钥名，仅用于最后一跳的高权账户；不填写 <code>###root用户</code> 时使用 root。可只配私钥、不配 root 密码。普通路由的 <code>--keys</code> 不会自动借给不同的 root 账户；前面的跳板凭据不变。</p>
          <p><code>###待核对旧密码</code> 仅保留旧版归属不可靠的密码，不参与连接。请显示密码、核对所属主机，将需要的密码写入正确的路由行，再删除这个字段；不要按旧下标自动分配。</p>
          <strong>可选字段</strong>
          <pre>{`##需要提权的主机
user@host:22
###sudo密码
sudo-password
###root用户
root
###root密码
root-password
###root私钥
keyname1
###默认socks
socksname1
###禁用
true
###允许跳板
false
##有口令的私钥
###口令
key-passphrase
###私钥
-----BEGIN OPENSSH PRIVATE KEY-----
...
-----END OPENSSH PRIVATE KEY-----`}</pre>
        </aside>
      </div>
      </div>
      <div className="config-tab-content" hidden={tab !== 'connections'}>{openedConnections && <ConnectionsPanel dirty={dirty || saving} revision={texts?.revision} onChanged={async (next) => { if (!alive.current) return; if (next) onSaved(next, false); await reload() }} />}</div>
      <footer className="modal-footer">
        <div className={`save-status ${error ? 'error' : ''}`}>{error ? <><Icon name="alert" />{error}</> : '保存后自动去掉空行，并验证主机、私钥引用和 SOCKS。'}</div>
        <button className="secondary-button" onClick={onClose}>取消</button>
        <button className="primary-button" disabled={!texts || saving} onClick={save}>{saving ? '验证中…' : '验证并保存'}</button>
      </footer>
    </Modal>
  )
}
