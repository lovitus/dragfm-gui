package gui

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/jobs"
	"github.com/lovitus/dragfm-gui/internal/vault"
)

type controller struct {
	app              fyne.App
	window           fyne.Window
	vault            *vault.Store
	password         []byte
	document         config.Document
	queue            *jobs.Queue
	mu               sync.Mutex
	left             *filePane
	right            *filePane
	jobView          *jobPane
	runtimePasswords map[string]map[int]string
	sessionSSH       map[string]bool
	dragSource       *filePane
}

func Start(application fyne.App, vaultPath string) {
	c := &controller{app: application, queue: jobs.New(128), runtimePasswords: make(map[string]map[int]string), sessionSSH: make(map[string]bool)}
	if _, err := os.Stat(vaultPath); errors.Is(err, os.ErrNotExist) {
		c.showSetup(vaultPath)
		return
	}
	c.showUnlock(vaultPath)
}

func (c *controller) showSetup(path string) {
	w := c.app.NewWindow("dragfm-gui · 创建保险库")
	w.Resize(fyne.NewSize(520, 330))
	hint := widget.NewEntry()
	hint.SetPlaceHolder("必填；请勿填写密码本身")
	password := widget.NewPasswordEntry()
	confirm := widget.NewPasswordEntry()
	status := widget.NewLabel("")
	create := widget.NewButton("创建并解锁", func() {
		if hint.Text == "" {
			status.SetText("必须填写主密码提示")
			return
		}
		if len(password.Text) < 8 {
			status.SetText("主密码至少 8 个字符")
			return
		}
		if password.Text != confirm.Text {
			status.SetText("两次输入的主密码不一致")
			return
		}
		store, err := vault.Create(path, hint.Text, []byte(password.Text), config.NewDocument())
		if err != nil {
			status.SetText(err.Error())
			return
		}
		c.vault, c.password, c.document = store, []byte(password.Text), config.NewDocument()
		w.Close()
		c.showMain()
	})
	create.Importance = widget.HighImportance
	w.SetContent(container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("首次启动", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("主密码用于加密主机、密码、私钥和代理配置。程序不会保存主密码。"),
		widget.NewForm(
			widget.NewFormItem("提示", hint),
			widget.NewFormItem("主密码", password),
			widget.NewFormItem("确认密码", confirm),
		), status, create,
	)))
	w.SetCloseIntercept(c.app.Quit)
	w.Show()
}

func (c *controller) showUnlock(path string) {
	header, err := vault.ReadHeader(path)
	if err != nil {
		w := c.app.NewWindow("dragfm-gui · 保险库错误")
		w.SetContent(container.NewPadded(container.NewVBox(widget.NewLabel(err.Error()), widget.NewButton("退出", c.app.Quit))))
		w.Show()
		return
	}
	w := c.app.NewWindow("dragfm-gui · 解锁")
	w.Resize(fyne.NewSize(500, 260))
	password := widget.NewPasswordEntry()
	status := widget.NewLabel("")
	unlock := func() {
		store, document, err := vault.Open(path, []byte(password.Text))
		if err != nil {
			status.SetText(err.Error())
			password.SetText("")
			return
		}
		c.vault, c.password, c.document = store, []byte(password.Text), document
		w.Close()
		c.showMain()
	}
	password.OnSubmitted = func(string) { unlock() }
	button := widget.NewButton("解锁", unlock)
	button.Importance = widget.HighImportance
	w.SetContent(container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("输入主密码", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("提示："+header.Hint), password, status, button,
	)))
	w.SetCloseIntercept(c.app.Quit)
	w.Show()
	w.Canvas().Focus(password)
}

func (c *controller) showMain() {
	w := c.app.NewWindow("dragfm-gui")
	c.window = w
	w.Resize(fyne.NewSize(1500, 800))
	home, _ := os.UserHomeDir()
	leftPath, rightPath := c.document.UI.LeftPath, c.document.UI.RightPath
	if leftPath == "" {
		leftPath = home
	}
	if rightPath == "" {
		rightPath = home
	}
	c.jobView = newJobPane(c)
	c.left = newFilePane(c, "左栏", leftPath, true)
	c.right = newFilePane(c, "右栏", rightPath, false)
	c.left.other, c.right.other = c.right, c.left

	files := container.NewHSplit(c.left.object(), c.right.object())
	files.Offset = 0.5
	content := container.NewHSplit(files, c.jobView.object())
	content.Offset = 0.74
	settings := widget.NewButtonWithIcon("连接配置", theme.SettingsIcon(), c.showSettings)
	lock := widget.NewButtonWithIcon("锁定", theme.VisibilityOffIcon(), c.lock)
	brand := widget.NewLabelWithStyle("dragfm", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	caption := widget.NewLabel("双端点文件工作区 · 横向拖动，悬停目录后松开即可放入")
	header := container.NewBorder(nil, nil, container.NewHBox(brand, widget.NewSeparator(), caption), container.NewHBox(settings, lock))
	w.SetContent(container.NewPadded(container.NewBorder(header, nil, nil, nil, content)))
	c.applyTheme(c.document.UI.Theme)
	w.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyL, Modifier: fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift}, func(fyne.Shortcut) { c.lock() })
	w.Canvas().AddShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyT, Modifier: fyne.KeyModifierShortcutDefault | fyne.KeyModifierShift}, func(fyne.Shortcut) { c.cycleTheme() })
	w.SetCloseIntercept(func() {
		c.persistUI()
		c.queue.Close()
		c.closePanes()
		c.wipePassword()
		w.Close()
		c.app.Quit()
	})
	w.Show()
	c.consumeQueue()
	c.left.refresh()
	c.right.refresh()
}

func (c *controller) save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vault.Save(c.password, c.document)
}

func (c *controller) persistUI() {
	if c.left == nil || c.right == nil {
		return
	}
	c.document.UI.LeftPath, c.document.UI.RightPath = c.left.path.Text, c.right.path.Text
	c.document.UI.LeftEndpoint, c.document.UI.RightEndpoint = c.left.endpointName, c.right.endpointName
	_ = c.save()
}

func (c *controller) lock() {
	c.persistUI()
	c.queue.Close()
	c.closePanes()
	c.wipePassword()
	if c.window != nil {
		c.window.Close()
	}
	c.queue = jobs.New(128)
	c.showUnlock(c.vault.Path)
}

func (c *controller) closePanes() {
	if c.left != nil {
		c.left.close()
	}
	if c.right != nil {
		c.right.close()
	}
}

func (c *controller) wipePassword() {
	for i := range c.password {
		c.password[i] = 0
	}
	c.password = nil
	for hostID, passwords := range c.runtimePasswords {
		for hop := range passwords {
			delete(passwords, hop)
		}
		delete(c.runtimePasswords, hostID)
	}
	for hostID := range c.sessionSSH {
		delete(c.sessionSSH, hostID)
	}
}

func (c *controller) reportError(title string, err error) {
	if err == nil {
		return
	}
	fyne.Do(func() { dialog.ShowError(fmt.Errorf("%s: %w", title, err), c.window) })
}

func (c *controller) refreshHostSelectors() {
	if c.left != nil {
		c.left.reloadHostOptions()
	}
	if c.right != nil {
		c.right.reloadHostOptions()
	}
}
