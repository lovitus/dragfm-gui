package gui

import (
	"io/fs"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/lovitus/dragfm-gui/internal/endpoint"
)

func TestDragHoverChoosesConcreteDirectory(t *testing.T) {
	application := test.NewApp()
	defer application.Quit()
	controller := &controller{}
	source := &filePane{controller: controller}
	pathEntry := widget.NewEntry()
	pathEntry.SetText("/open")
	target := &filePane{
		controller: controller,
		path:       pathEntry,
		entries: []endpoint.Entry{
			{Name: "folder", Path: "/open/folder", Mode: fs.ModeDir},
			{Name: "file", Path: "/open/file"},
		},
	}
	target.list = widget.NewList(func() int { return len(target.entries) }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(widget.ListItemID, fyne.CanvasObject) {})
	controller.dragSource = source
	row := newFileRow(target)
	row.index = 0
	row.MouseIn(nil)
	if got := target.dropDestination(); got != "/open/folder" {
		t.Fatalf("directory row was not selected as drop target: %q", got)
	}
	row.MouseOut()
	if got := target.dropDestination(); got != "/open" {
		t.Fatalf("leaving row should fall back to opened directory: %q", got)
	}
	row.index = 1
	row.MouseIn(nil)
	if got := target.dropDestination(); got != "/open" {
		t.Fatalf("regular file must not become a drop directory: %q", got)
	}
}
