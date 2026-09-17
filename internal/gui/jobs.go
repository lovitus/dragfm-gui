package gui

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/lovitus/dragfm-gui/internal/config"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
	"github.com/lovitus/dragfm-gui/internal/jobs"
)

type jobPane struct {
	controller *controller
	running    *widget.Label
	progress   *widget.ProgressBar
	output     *widget.Entry
	pending    *widget.List
	history    *widget.List
	command    *widget.Entry
	target     *widget.Select
	mu         sync.Mutex
	pendingIDs []string
	pendingMap map[string]jobs.Update
	historyLog []config.HistoryEntry
}

func newJobPane(controller *controller) *jobPane {
	pane := &jobPane{controller: controller, running: widget.NewLabel("— idle —"), progress: widget.NewProgressBar(), output: widget.NewMultiLineEntry(), pendingMap: make(map[string]jobs.Update), historyLog: append([]config.HistoryEntry(nil), controller.document.History...)}
	pane.output.Disable()
	pane.output.SetMinRowsVisible(10)
	pane.pending = widget.NewList(func() int { pane.mu.Lock(); defer pane.mu.Unlock(); return len(pane.pendingIDs) }, func() fyne.CanvasObject { return widget.NewLabel("pending") }, func(id widget.ListItemID, object fyne.CanvasObject) {
		pane.mu.Lock()
		defer pane.mu.Unlock()
		if id < len(pane.pendingIDs) {
			update := pane.pendingMap[pane.pendingIDs[id]]
			object.(*widget.Label).SetText("⏳ " + update.Description)
		}
	})
	pane.history = widget.NewList(func() int { pane.mu.Lock(); defer pane.mu.Unlock(); return len(pane.historyLog) }, func() fyne.CanvasObject { return widget.NewLabel("history") }, func(id widget.ListItemID, object fyne.CanvasObject) {
		pane.mu.Lock()
		defer pane.mu.Unlock()
		if id < len(pane.historyLog) {
			item := pane.historyLog[len(pane.historyLog)-1-id]
			status := "失败"
			if item.Success {
				status = "完成"
			}
			object.(*widget.Label).SetText(status + " · " + item.Operation)
		}
	})
	pane.target = widget.NewSelect([]string{"当前焦点", "左栏", "右栏", "控制机"}, nil)
	pane.target.SetSelected("当前焦点")
	pane.command = widget.NewEntry()
	pane.command.SetPlaceHolder("输入命令，回车加入队列")
	pane.command.OnSubmitted = pane.submitCommand
	return pane
}

func (p *jobPane) object() fyne.CanvasObject {
	queueTabs := container.NewAppTabs(
		container.NewTabItem("Pending", p.pending),
		container.NewTabItem("History", p.history),
	)
	commandBar := container.NewBorder(nil, nil, p.target, nil, p.command)
	runningCard := widget.NewCard("正在执行", "全局仅运行一个任务", container.NewVBox(p.running, p.progress))
	content := container.NewBorder(
		runningCard,
		container.NewVBox(widget.NewSeparator(), widget.NewLabelWithStyle("命令", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), commandBar, widget.NewLabel("⌘⇧L 锁定 · ⌘⇧T 主题")),
		nil, nil,
		container.NewVSplit(widget.NewCard("输出", "运行日志已脱敏", p.output), queueTabs),
	)
	return widget.NewCard("任务与命令", "Running · Pending · History", content)
}

func (p *jobPane) appendOutput(message string) {
	p.mu.Lock()
	text := p.output.Text
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += time.Now().Format("15:04:05") + "  " + redact(message) + "\n"
	p.mu.Unlock()
	p.output.SetText(text)
	p.output.CursorRow = len(strings.Split(text, "\n"))
	p.output.Refresh()
}

func (c *controller) consumeQueue() {
	go func() {
		for update := range c.queue.Updates() {
			update := update
			fyne.Do(func() { c.applyJobUpdate(update) })
		}
	}()
}

func (c *controller) applyJobUpdate(update jobs.Update) {
	p := c.jobView
	p.mu.Lock()
	switch update.State {
	case jobs.Pending:
		if _, exists := p.pendingMap[update.ID]; !exists {
			p.pendingIDs = append(p.pendingIDs, update.ID)
		}
		p.pendingMap[update.ID] = update
	case jobs.Running:
		for index, id := range p.pendingIDs {
			if id == update.ID {
				p.pendingIDs = append(p.pendingIDs[:index], p.pendingIDs[index+1:]...)
				break
			}
		}
		delete(p.pendingMap, update.ID)
	default:
		entry := config.HistoryEntry{ID: update.ID, StartedAt: update.StartedAt, FinishedAt: update.FinishedAt, Operation: redact(update.Description), Success: update.State == jobs.Succeeded, Message: redact(update.Message)}
		p.historyLog = append(p.historyLog, entry)
		c.document.History = append(c.document.History, entry)
		if len(c.document.History) > 500 {
			c.document.History = c.document.History[len(c.document.History)-500:]
		}
		go func() { _ = c.save() }()
	}
	p.mu.Unlock()
	p.pending.Refresh()
	p.history.Refresh()
	if update.State == jobs.Running {
		p.running.SetText(update.Description)
		if update.Message != "" {
			p.appendOutput(update.Message)
		}
		if update.Progress >= 0 && update.Progress <= 1 {
			p.progress.SetValue(update.Progress)
		}
	} else if update.State != jobs.Pending {
		p.running.SetText("— idle —")
		p.progress.SetValue(0)
		p.appendOutput(update.Description + " · " + update.Message)
		c.left.refresh()
		c.right.refresh()
	}
}

func (p *jobPane) submitCommand(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	target := p.target.Selected
	ep, directory := p.controller.commandTarget(target)
	preview := redact(command)
	_, err := p.controller.queue.Submit(jobs.Job{Description: "命令 · " + target + " · " + preview, Run: func(ctx context.Context, emit func(jobs.Update)) error {
		var output bytes.Buffer
		emit(jobs.Update{Message: "$ " + preview})
		err := ep.Exec(ctx, command, endpoint.ExecOptions{Directory: directory, Stdout: &output, Stderr: &output})
		emit(jobs.Update{Message: output.String()})
		return err
	}})
	if err != nil {
		p.controller.reportError("命令入队失败", err)
		return
	}
	p.command.SetText("")
}

func (c *controller) commandTarget(target string) (endpoint.Endpoint, string) {
	switch target {
	case "左栏":
		return c.left.currentEndpoint(), c.left.path.Text
	case "右栏":
		return c.right.currentEndpoint(), c.right.path.Text
	case "控制机":
		return endpoint.NewLocal(), ""
	default:
		if c.right.focused {
			return c.right.currentEndpoint(), c.right.path.Text
		}
		return c.left.currentEndpoint(), c.left.path.Text
	}
}

func redact(value string) string {
	if strings.Contains(value, "-----BEGIN ") && strings.Contains(value, "PRIVATE KEY-----") {
		return "[REDACTED PRIVATE KEY]"
	}
	words := strings.Fields(value)
	maskNext := false
	for index, word := range words {
		if maskNext {
			words[index] = "***"
			maskNext = false
			continue
		}
		lower := strings.ToLower(word)
		if lower == "--password" || lower == "--passwords" || lower == "--socks-pass" || lower == "--passphrase" || lower == "--sudo-password" {
			maskNext = true
			continue
		}
		if strings.Contains(lower, "password=") || strings.Contains(lower, "passphrase=") || strings.Contains(lower, "sudo_password=") || strings.Contains(lower, "sudo-password=") {
			if at := strings.Index(word, "="); at >= 0 {
				words[index] = word[:at+1] + "***"
			}
		}
		words[index] = inlineCredentialPattern.ReplaceAllString(words[index], `${1}:***@`)
	}
	return strings.Join(words, " ")
}

var inlineCredentialPattern = regexp.MustCompile(`([[:alnum:]_.-]+):(?:"[^"]*"|'[^']*'|[^@[:space:]]+)@`)

func formatBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	number := float64(value)
	for _, unit := range units {
		number /= 1024
		if number < 1024 {
			return fmt.Sprintf("%.1f %s", number, unit)
		}
	}
	return fmt.Sprintf("%.1f PiB", number/1024)
}
