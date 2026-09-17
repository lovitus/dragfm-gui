package gui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/configtext"
	"github.com/lovitus/dragfm-gui/internal/routespec"
	"golang.org/x/crypto/ssh"
)

type numberedEditor struct {
	entry  *widget.Entry
	gutter *widget.TextGrid
	status *widget.Label
	body   fyne.CanvasObject
}

func newNumberedEditor(value string) *numberedEditor {
	entry := widget.NewMultiLineEntry()
	entry.TextStyle.Monospace = true
	entry.Wrapping = fyne.TextWrapOff
	entry.Scroll = fyne.ScrollNone
	entry.SetMinRowsVisible(30)
	gutter := widget.NewTextGrid()
	gutter.ShowLineNumbers = true
	gutter.Scroll = fyne.ScrollNone
	status := widget.NewLabel("")
	editor := &numberedEditor{entry: entry, gutter: gutter, status: status}
	update := func(text string) {
		lineCount := strings.Count(text, "\n") + 1
		// TextGrid's line-number renderer needs at least one physical cell per
		// row; an all-empty grid can panic while its digit width grows.
		gutter.SetText(strings.Repeat(" \n", max(0, lineCount-1)) + " ")
		status.SetText(fmt.Sprintf("第 %d 行，第 %d 列 · 共 %d 行 · 自动换行已关闭", entry.CursorRow+1, entry.CursorColumn+1, lineCount))
	}
	entry.OnChanged = update
	entry.OnCursorChanged = func() { update(entry.Text) }
	entry.SetText(value)
	content := container.NewHBox(gutter, entry)
	scroll := container.NewScroll(content)
	scroll.Direction = container.ScrollBoth
	editor.body = container.NewBorder(nil, status, nil, nil, scroll)
	return editor
}

func (e *numberedEditor) object() fyne.CanvasObject { return e.body }

func (c *controller) showSettings() {
	w := c.app.NewWindow("dragfm-gui · 文本配置")
	w.Resize(fyne.NewSize(1120, 760))
	connections := newNumberedEditor(configtext.Connections(c.document))
	keys := newNumberedEditor(configtext.Keys(c.document))
	status := widget.NewLabel("保险库已解锁：配置正文可能包含密码和私钥，请注意屏幕共享与剪贴板。")
	status.Wrapping = fyne.TextWrapWord

	save := widget.NewButton("验证并保存", func() {
		hosts, proxies, err := configtext.ParseConnections(connections.entry.Text, c.document)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		parsedKeys, err := configtext.ParseKeys(keys.entry.Text, c.document)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		lookup := func(name string) (*config.PrivateKey, bool) {
			for index := range parsedKeys {
				if parsedKeys[index].Name == name || parsedKeys[index].ID == name {
					return &parsedKeys[index], true
				}
			}
			return nil, false
		}
		for _, key := range parsedKeys {
			if key.Passphrase == "" {
				_, err = ssh.ParsePrivateKey([]byte(key.PEM))
			} else {
				_, err = ssh.ParsePrivateKeyWithPassphrase([]byte(key.PEM), []byte(key.Passphrase))
			}
			if err != nil {
				dialog.ShowError(fmt.Errorf("私钥 %q：%w", key.Name, err), w)
				return
			}
		}
		for _, host := range hosts {
			if _, err = routespec.ParseSSH(host.RouteSpec, lookup); err != nil {
				dialog.ShowError(fmt.Errorf("SSH %q：%w", host.Name, err), w)
				return
			}
		}
		for _, proxy := range proxies {
			if _, err = routespec.ParseSOCKS(proxy.Spec); err != nil {
				dialog.ShowError(fmt.Errorf("SOCKS %q：%w", proxy.Name, err), w)
				return
			}
		}
		c.mu.Lock()
		c.document.Hosts, c.document.SOCKS, c.document.Keys = hosts, proxies, parsedKeys
		c.mu.Unlock()
		if err = c.save(); err != nil {
			dialog.ShowError(err, w)
			return
		}
		c.refreshHostSelectors()
		status.SetText("已验证并加密保存。SSH 主机同时进入保守跳板候选池。")
	})
	save.Importance = widget.HighImportance

	help := widget.NewRichTextFromMarkdown(`
## 连接配置

每条物理行只放一个配置，编辑器不进行软换行：

    ssh 名称 = user:"password"@hop1 user@target --keys ",vaultKey"
    socks 名称 = user:pass@127.0.0.1:1080

在行首添加 **disabled** 可停用：

    disabled ssh 临时主机 = user@192.0.2.10:22

所有 SSH 主机都具备跳板能力，但 dragfm 不会为了“测速”而遍历登录全部主机。只有本次解锁期间已经成功连接过的 SSH 会话，或某对端点曾经成功使用过的跳板，才会进入自动候选；缓存失败时会移除，再从已知安全候选中选择。

## 私钥配置

    key vaultKey
    passphrase optional passphrase
    -----BEGIN OPENSSH PRIVATE KEY-----
    ...
    -----END OPENSSH PRIVATE KEY-----
    endkey

`)
	help.Wrapping = fyne.TextWrapWord
	tabs := container.NewAppTabs(
		container.NewTabItem("SSH / SOCKS", connections.object()),
		container.NewTabItem("保险库私钥", keys.object()),
		container.NewTabItem("格式说明", container.NewVScroll(help)),
	)
	footer := container.NewBorder(nil, nil, status, save)
	w.SetContent(container.NewPadded(container.NewBorder(nil, footer, nil, nil, tabs)))
	w.Show()
}
