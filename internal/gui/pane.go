package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/fyne-io/terminal"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

type filePane struct {
	controller   *controller
	title        string
	isLeft       bool
	other        *filePane
	path         *widget.Entry
	host         *widget.Select
	list         *widget.List
	status       *widget.Label
	terminalBox  *fyne.Container
	term         *terminal.Terminal
	pty          endpoint.PTYSession
	endpoint     endpoint.Endpoint
	endpointName string
	entries      []endpoint.Entry
	selected     int
	focused      bool
	dropTarget   string
	mu           sync.RWMutex
	cancelTerm   context.CancelFunc
}

func newFilePane(controller *controller, title, initialPath string, left bool) *filePane {
	pane := &filePane{controller: controller, title: title, isLeft: left, path: widget.NewEntry(), status: widget.NewLabel(""), endpoint: endpoint.NewLocal(), endpointName: "本机", selected: -1}
	pane.path.SetText(initialPath)
	hosts := []string{"本机"}
	for _, host := range controller.document.Hosts {
		if !host.Disabled && host.Name != "" {
			hosts = append(hosts, host.Name)
		}
	}
	pane.host = widget.NewSelect(hosts, pane.changeHost)
	pane.host.SetSelected("本机")
	pane.path.OnSubmitted = func(value string) { pane.navigate(value, true) }
	pane.list = widget.NewList(
		func() int { pane.mu.RLock(); defer pane.mu.RUnlock(); return len(pane.entries) },
		func() fyne.CanvasObject { return newFileRow(pane) },
		func(id widget.ListItemID, object fyne.CanvasObject) { object.(*fileRow).bind(id) },
	)
	pane.terminalBox = container.NewStack(widget.NewLabel("正在启动终端…"))
	pane.startTerminal()
	return pane
}

func (p *filePane) object() fyne.CanvasObject {
	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), p.refresh)
	up := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() { p.navigate(p.currentEndpoint().Dir(p.path.Text), true) })
	deleteButton := widget.NewButtonWithIcon("删除", theme.DeleteIcon(), p.deleteSelected)
	hashButton := widget.NewButtonWithIcon("SHA-256", theme.InfoIcon(), p.hashSelected)
	heading := container.NewBorder(nil, nil,
		container.NewHBox(widget.NewLabelWithStyle(p.title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), p.host),
		container.NewHBox(up, refresh),
		p.path,
	)
	columns := widget.NewLabel("   权限          大小   修改时间          名称")
	columns.TextStyle.Monospace = true
	footer := container.NewBorder(nil, nil, container.NewHBox(deleteButton, hashButton), nil, p.status)
	fileArea := container.NewBorder(heading, footer, nil, nil, container.NewBorder(columns, nil, nil, nil, p.list))
	terminalTitle := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle("Shell", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("登录式交互环境 · cwd 双向同步"),
	)
	split := container.NewVSplit(fileArea, container.NewBorder(terminalTitle, nil, nil, nil, p.terminalBox))
	split.Offset = 0.62
	return widget.NewCard("", "", split)
}

func (p *filePane) currentEndpoint() endpoint.Endpoint {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.endpoint
}

func (p *filePane) refresh() {
	ep, directory := p.currentEndpoint(), p.path.Text
	p.status.SetText("读取中…")
	go func() {
		entries, err := ep.List(context.Background(), directory)
		fyne.Do(func() {
			if err != nil {
				p.status.SetText("读取失败：" + err.Error())
				return
			}
			p.mu.Lock()
			p.entries, p.selected = entries, -1
			p.mu.Unlock()
			p.list.Refresh()
			p.status.SetText(fmt.Sprintf("%s · %d 项", directory, len(entries)))
		})
	}()
}

func (p *filePane) navigate(value string, syncTerminal bool) {
	ep := p.currentEndpoint()
	go func() {
		absolute, err := ep.Abs(context.Background(), strings.TrimSpace(value))
		if err == nil {
			var entry endpoint.Entry
			entry, err = ep.Stat(context.Background(), absolute)
			if err == nil && !entry.IsDir() {
				err = errors.New("路径不是目录")
			}
		}
		fyne.Do(func() {
			if err != nil {
				p.status.SetText(err.Error())
				return
			}
			p.focus()
			p.path.SetText(absolute)
			p.refresh()
			if syncTerminal && p.term != nil {
				_, _ = p.term.Write([]byte("cd -- " + shellQuote(absolute) + "\n"))
			}
		})
	}()
}

func (p *filePane) startTerminal() {
	if p.cancelTerm != nil {
		p.cancelTerm()
	}
	if p.term != nil {
		p.term.Exit()
	}
	if p.pty != nil {
		_ = p.pty.Close()
	}
	term := terminal.New()
	term.SetStartDir(p.path.Text)
	p.term = term
	p.terminalBox.Objects = []fyne.CanvasObject{term}
	p.terminalBox.Refresh()
	listen := make(chan terminal.Config, 16)
	term.AddListener(listen)
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelTerm = cancel

	if provider, ok := p.currentEndpoint().(endpoint.PTYProvider); ok {
		go func() {
			pty, err := provider.OpenPTY(ctx, p.path.Text, p.hostShell(), 24, 80)
			if err != nil {
				p.controller.reportError(p.title+" SSH 终端", err)
				return
			}
			p.pty = pty
			filtered := filterCWDMarkers(pty.Output(), func(directory string) {
				fyne.Do(func() {
					if directory != "" {
						if directory != p.path.Text {
							p.path.SetText(directory)
						}
						p.refresh()
					}
				})
			})
			go func() {
				for config := range listen {
					_ = pty.Resize(config.Rows, config.Columns)
				}
			}()
			if err := term.RunWithConnection(pty.Input(), filtered); err != nil && ctx.Err() == nil {
				p.controller.reportError(p.title+" SSH 终端", err)
			}
		}()
		return
	}
	go func() {
		go func() {
			for config := range listen {
				if config.PWD != "" && config.PWD != p.path.Text {
					fyne.Do(func() { p.path.SetText(config.PWD); p.refresh() })
				}
			}
		}()
		if err := term.RunLocalShell(); err != nil && ctx.Err() == nil {
			p.controller.reportError(p.title+" 本地终端", err)
		}
	}()
}

func (p *filePane) hostShell() string {
	if p.endpointName == "本机" {
		return ""
	}
	for _, host := range p.controller.document.Hosts {
		if host.Name == p.endpointName && host.Shell != "" {
			return host.Shell
		}
	}
	return ""
}

func (p *filePane) selectEntry(index int) {
	p.mu.Lock()
	if index >= 0 && index < len(p.entries) {
		p.selected = index
	}
	p.mu.Unlock()
	p.focus()
	p.list.Refresh()
}

func (p *filePane) selectedEntry() (endpoint.Entry, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.selected < 0 || p.selected >= len(p.entries) {
		return endpoint.Entry{}, false
	}
	return p.entries[p.selected], true
}

func (p *filePane) setDropTarget(path string) {
	p.mu.Lock()
	if p.dropTarget == path {
		p.mu.Unlock()
		return
	}
	p.dropTarget = path
	p.mu.Unlock()
	p.list.Refresh()
}

func (p *filePane) dropDestination() string {
	p.mu.RLock()
	target := p.dropTarget
	p.mu.RUnlock()
	if target != "" {
		return target
	}
	return p.path.Text
}

func (p *filePane) openEntry(index int) {
	p.selectEntry(index)
	entry, ok := p.selectedEntry()
	if ok && entry.IsDir() {
		p.navigate(entry.Path, true)
	}
}

func (p *filePane) focus() {
	p.focused = true
	if p.other != nil {
		p.other.focused = false
	}
}

func (p *filePane) changeHost(name string) {
	if name == "" || name == p.endpointName {
		return
	}
	p.status.SetText("正在连接 " + name + "…")
	peer := "本机"
	if p.other != nil && p.other.endpointName != "" {
		peer = p.other.endpointName
	}
	p.controller.connectEndpoint(name, peer, func(ep endpoint.Endpoint, directory string, err error) {
		if err != nil {
			p.status.SetText("连接失败：" + err.Error())
			p.host.SetSelected(p.endpointName)
			return
		}
		old := p.currentEndpoint()
		p.mu.Lock()
		p.endpoint, p.endpointName = ep, name
		p.mu.Unlock()
		if old != nil {
			_ = old.Close()
		}
		p.path.SetText(directory)
		p.startTerminal()
		p.refresh()
	})
}

func (p *filePane) reloadHostOptions() {
	options := []string{"本机"}
	for _, host := range p.controller.document.Hosts {
		if !host.Disabled && host.Name != "" {
			options = append(options, host.Name)
		}
	}
	p.host.Options = options
	p.host.Refresh()
}

func (p *filePane) close() {
	if p.cancelTerm != nil {
		p.cancelTerm()
		p.cancelTerm = nil
	}
	if p.term != nil {
		p.term.Exit()
		p.term = nil
	}
	if p.pty != nil {
		_ = p.pty.Close()
		p.pty = nil
	}
	if ep := p.currentEndpoint(); ep != nil {
		_ = ep.Close()
	}
}

func (p *filePane) deleteSelected() { p.controller.confirmDelete(p) }
func (p *filePane) hashSelected()   { p.controller.enqueueHash(p) }

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

type fileRow struct {
	widget.BaseWidget
	pane    *filePane
	index   int
	label   *widget.Label
	bg      *canvas.Rectangle
	dragged float32
}

func newFileRow(pane *filePane) *fileRow {
	row := &fileRow{pane: pane, index: -1, label: widget.NewLabel(""), bg: canvas.NewRectangle(theme.Color(theme.ColorNameBackground))}
	row.label.TextStyle.Monospace = true
	row.ExtendBaseWidget(row)
	return row
}

func (r *fileRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(r.bg, container.NewPadded(r.label)))
}

func (r *fileRow) bind(index int) {
	r.index = index
	r.pane.mu.RLock()
	defer r.pane.mu.RUnlock()
	if index >= len(r.pane.entries) {
		return
	}
	entry := r.pane.entries[index]
	typeMark := " "
	if entry.IsDir() {
		typeMark = "▸"
	} else if entry.Mode&fs.ModeSymlink != 0 {
		typeMark = "↗"
	}
	r.label.SetText(fmt.Sprintf("%s %-10s %10s  %s  %s", typeMark, entry.Mode.String(), formatBytes(entry.Size), entry.Modified.Format("2006-01-02 15:04"), entry.Name))
	if r.pane.dropTarget == entry.Path && entry.IsDir() {
		r.bg.FillColor = theme.Color(theme.ColorNameFocus)
	} else if r.pane.selected == index {
		r.bg.FillColor = theme.Color(theme.ColorNameSelection)
	} else {
		r.bg.FillColor = theme.Color(theme.ColorNameBackground)
	}
	r.bg.Refresh()
}

func (r *fileRow) Tapped(*fyne.PointEvent)       { r.pane.selectEntry(r.index) }
func (r *fileRow) DoubleTapped(*fyne.PointEvent) { r.pane.openEntry(r.index) }
func (r *fileRow) Dragged(event *fyne.DragEvent) {
	r.dragged += event.Dragged.DX
	if math.Abs(float64(r.dragged)) >= 8 {
		r.pane.controller.dragSource = r.pane
	}
}
func (r *fileRow) DragEnd() {
	delta := r.dragged
	r.dragged = 0
	r.pane.selectEntry(r.index)
	targetPane := r.pane.other
	targetDirectory := ""
	if targetPane != nil {
		targetDirectory = targetPane.dropDestination()
		targetPane.setDropTarget("")
	}
	r.pane.controller.dragSource = nil
	if math.Abs(float64(delta)) < 55 || r.pane.other == nil {
		return
	}
	if (r.pane.isLeft && delta > 0) || (!r.pane.isLeft && delta < 0) {
		r.pane.controller.confirmDrop(r.pane, r.pane.other, targetDirectory)
	}
}

func (r *fileRow) MouseIn(*desktop.MouseEvent) {
	if r.pane.controller.dragSource == nil || r.pane.controller.dragSource == r.pane {
		return
	}
	r.pane.mu.RLock()
	if r.index >= 0 && r.index < len(r.pane.entries) && r.pane.entries[r.index].IsDir() {
		target := r.pane.entries[r.index].Path
		r.pane.mu.RUnlock()
		r.pane.setDropTarget(target)
		return
	}
	r.pane.mu.RUnlock()
}

func (r *fileRow) MouseMoved(*desktop.MouseEvent) {}

func (r *fileRow) MouseOut() {
	r.pane.mu.RLock()
	path := ""
	if r.index >= 0 && r.index < len(r.pane.entries) {
		path = r.pane.entries[r.index].Path
	}
	r.pane.mu.RUnlock()
	if path != "" {
		r.pane.mu.RLock()
		active := r.pane.dropTarget == path
		r.pane.mu.RUnlock()
		if active {
			r.pane.setDropTarget("")
		}
	}
}

func filterCWDMarkers(source io.Reader, update func(string)) io.Reader {
	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		prefix := []byte("\x1b]777;dragfm-cwd=")
		buffer := make([]byte, 32*1024)
		pending := make([]byte, 0, 32*1024)
		for {
			count, err := source.Read(buffer)
			if count > 0 {
				pending = append(pending, buffer[:count]...)
				for {
					start := bytes.Index(pending, prefix)
					if start < 0 {
						keep := matchingPrefixSuffix(pending, prefix)
						if len(pending) > keep {
							_, _ = writer.Write(pending[:len(pending)-keep])
							pending = append([]byte(nil), pending[len(pending)-keep:]...)
						}
						break
					}
					end := bytes.IndexByte(pending[start+len(prefix):], 7)
					if end < 0 {
						if start > 0 {
							_, _ = writer.Write(pending[:start])
							pending = append([]byte(nil), pending[start:]...)
						}
						break
					}
					end += start + len(prefix)
					_, _ = writer.Write(pending[:start])
					update(string(pending[start+len(prefix) : end]))
					pending = append([]byte(nil), pending[end+1:]...)
				}
			}
			if err != nil {
				if len(pending) > 0 {
					_, _ = writer.Write(pending)
				}
				if !errors.Is(err, io.EOF) {
					_ = writer.CloseWithError(err)
				}
				return
			}
		}
	}()
	return reader
}

func matchingPrefixSuffix(data, prefix []byte) int {
	maximum := len(prefix) - 1
	if len(data) < maximum {
		maximum = len(data)
	}
	for size := maximum; size > 0; size-- {
		if bytes.Equal(data[len(data)-size:], prefix[:size]) {
			return size
		}
	}
	return 0
}
