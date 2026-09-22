import { useEffect, useRef, useState } from 'react'
import { EditorState } from '@codemirror/state'
import { Decoration, type DecorationSet, EditorView, ViewPlugin, type ViewUpdate, WidgetType, drawSelection, dropCursor, highlightActiveLine, highlightActiveLineGutter, highlightSpecialChars, keymap, lineNumbers, rectangularSelection } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { api } from '../api'
import type { Bootstrap, ConfigTexts } from '../types'
import Icon from './Icon'
import Modal from './Modal'

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
  const inline = /:(?:"(?:\\.|[^"])*"|'(?:\\.|[^'])*'|[^\s@]+)@/g
  let offset = 0
  for (const text of document.split('\n')) {
    const trimmed = text.trim()
    if (trimmed.startsWith('###')) {
      const field = trimmed.slice(3).trim()
      maskMetadataValue = field === 'sudo密码' || field === 'root密码' || field === '口令'
      offset += text.length + 1
      continue
    }
    if (trimmed.startsWith('#')) {
      maskMetadataValue = false
      offset += text.length + 1
      continue
    }
    if (maskMetadataValue && trimmed !== '') {
      const leading = text.length - text.trimStart().length
      const trailing = text.trimEnd().length
      ranges.push({ from: offset + leading, to: offset + trailing })
      maskMetadataValue = false
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

function CodeEditor({ value, onChange, revealSecrets }: { value: string; onChange: (value: string) => void; revealSecrets: boolean }) {
  const host = useRef<HTMLDivElement>(null)
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
    // A cleanup transaction would invoke onChange('') and erase the parent
    // draft when toggling password visibility. Destroy without editing it.
    return () => view.destroy()
    // Initial value is loaded once; subsequent edits belong to CodeMirror.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return <div className="code-editor" ref={host} />
}

export default function ConfigEditor({ onClose, onSaved }: { onClose: () => void; onSaved: (bootstrap: Bootstrap) => void }) {
  const [texts, setTexts] = useState<ConfigTexts | null>(null)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [revealSecrets, setRevealSecrets] = useState(false)
  useEffect(() => { api.getConfigTexts().then(setTexts).catch((reason) => setError(String(reason))) }, [])
  const save = async () => {
    if (!texts) return
    setSaving(true); setError('')
    try { onSaved(await api.saveConfigTexts(texts.markdown)) }
    catch (reason) { setError(String(reason)) }
    finally { setSaving(false) }
  }
  return (
    <Modal title="Markdown 配置" onClose={onClose} width={1180}>
      <div className="config-security-bar"><span>私钥正文保持可见；SSH、SOCKS、sudo、root 和私钥口令默认遮罩。</span><button type="button" className="secondary-button" onClick={() => setRevealSecrets((shown) => !shown)}>{revealSecrets ? '隐藏密码' : '显示密码'}</button></div>
      <div className="config-markdown-layout">
        <div className="config-content">
          {!texts && !error && <div className="empty-state">正在解密配置…</div>}
          {texts && <CodeEditor key={revealSecrets ? 'revealed' : 'masked'} value={texts.markdown} revealSecrets={revealSecrets} onChange={(markdown) => setTexts({ markdown })} />}
        </div>
        <aside className="config-reference">
          <strong>格式参考</strong>
          <p><code>##</code> 后面是名称，下一行是内容。不要缩进，不需要空行。</p>
          <pre>{`#主机
##主机名1
uhome:"EXAMPLE_PASSWORD_1"@10.1.1.100:41122,/bin/bash
##主机名2
uhome:"EXAMPLE_PASSWORD_2"@1.2.3.4:41122  uhome:"EXAMPLE_PASSWORD_3"@5.6.7.8:41122   uhome@1.1.2.2:41122 --keys ",,keyname1" ,/bin/zsh
##主机名3我不写shell你用登录默认的
1:1@1.1.1.1:22
#私钥
##keyname1
abc...
#socks池
##socksname1
user1:pass1@1.1.1.1:1080`}</pre>
          <p>主机可写一跳或整条 FlySSH 多跳路由。末尾的 <code>,/bin/zsh</code> 是指定 Shell；省略时使用登录默认 Shell。<code>--keys</code> 引用本配置中同名私钥。</p>
          <strong>可选字段</strong>
          <pre>{`##需要提权的主机
user@host:22
###sudo密码
sudo-password
###root用户
root
###root密码
root-password
###默认socks
socksname1
###禁用
true
##有口令的私钥
###口令
key-passphrase
###私钥
-----BEGIN OPENSSH PRIVATE KEY-----
...
-----END OPENSSH PRIVATE KEY-----`}</pre>
        </aside>
      </div>
      <footer className="modal-footer">
        <div className={`save-status ${error ? 'error' : ''}`}>{error ? <><Icon name="alert" />{error}</> : '保存后自动去掉空行，并验证主机、私钥引用和 SOCKS。'}</div>
        <button className="secondary-button" onClick={onClose}>取消</button>
        <button className="primary-button" disabled={!texts || saving} onClick={save}>{saving ? '验证中…' : '验证并保存'}</button>
      </footer>
    </Modal>
  )
}
