package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type refinedTheme struct{ base fyne.Theme }

func (t refinedTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	return t.base.Color(name, variant)
}
func (t refinedTheme) Font(style fyne.TextStyle) fyne.Resource { return t.base.Font(style) }
func (t refinedTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}
func (t refinedTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 14
	case theme.SizeNameCaptionText:
		return 12
	case theme.SizeNamePadding:
		return 7
	case theme.SizeNameInputRadius:
		return 7
	case theme.SizeNameScrollBar:
		return 12
	}
	return t.base.Size(name)
}

func (c *controller) applyTheme(name string) {
	var base fyne.Theme
	switch name {
	case "light":
		base = theme.LightTheme()
	case "dark":
		base = theme.DarkTheme()
	default:
		base = theme.DefaultTheme()
		name = "system"
	}
	c.app.Settings().SetTheme(refinedTheme{base: base})
	c.document.UI.Theme = name
}

func (c *controller) cycleTheme() {
	next := map[string]string{"system": "light", "light": "dark", "dark": "system"}[c.document.UI.Theme]
	if next == "" {
		next = "system"
	}
	c.applyTheme(next)
	_ = c.save()
	if c.jobView != nil {
		c.jobView.appendOutput("主题切换为：" + map[string]string{"system": "默认颜色", "light": "亮色", "dark": "暗色"}[next])
	}
}
