package gui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestNumberedEditorUsesPhysicalLinesWithoutWrapping(t *testing.T) {
	application := test.NewApp()
	defer application.Quit()
	editor := newNumberedEditor("ssh one = user@host\n\n# comment\n")
	if editor.entry.Wrapping != fyne.TextWrapOff || editor.entry.Scroll != fyne.ScrollNone {
		t.Fatalf("editor must use external scrolling with wrapping disabled")
	}
	if !editor.gutter.ShowLineNumbers || len(editor.gutter.Rows) != 4 {
		t.Fatalf("line gutter does not match physical lines: %#v", editor.gutter.Rows)
	}
	editor.entry.CursorRow, editor.entry.CursorColumn = 2, 3
	editor.entry.OnCursorChanged()
	if !strings.Contains(editor.status.Text, "第 3 行，第 4 列") {
		t.Fatalf("cursor status missing: %q", editor.status.Text)
	}
}
