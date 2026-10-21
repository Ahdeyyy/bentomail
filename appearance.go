package main

import (
	_ "embed"
	"fmt"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

//go:embed resources/logo.svg
var logoSVG []byte

func workspaceTheme(dark bool, accent string) *ui.Theme {
	t := ui.LightTheme()
	t.Font = "Martian Grotesk"
	t.FontSize = 13
	t.Radius = 8
	t.Spacing = 4
	t.Background = ui.Hex("#f1f3f8")
	t.Surface = ui.Hex("#e9edf5")
	t.SurfaceHover = ui.Hex("#e1e7f2")
	t.SurfacePressed = ui.Hex("#dce3f5")
	t.Border = ui.Hex("#dce2ed")
	t.Text = ui.Hex("#20293e")
	t.TextMuted = ui.Hex("#66728a")
	t.Accent = ui.Hex("#5260d8")
	t.AccentText = ui.Hex("#ffffff")
	if dark {
		t.Dark = true
		t.Background = ui.Hex("#111521")
		t.Surface = ui.Hex("#1a2030")
		t.SurfaceHover = ui.Hex("#252e43")
		t.SurfacePressed = ui.Hex("#303c59")
		t.Border = ui.Hex("#2b354b")
		t.Text = ui.Hex("#e9edf7")
		t.TextMuted = ui.Hex("#9ba9c3")
		t.Accent = ui.Hex("#a3adff")
		t.AccentText = ui.Hex("#171d35")
	}
	switch accent {
	case "Teal":
		t.Accent = ui.Hex("#147d75")
	case "Rose":
		t.Accent = ui.Hex("#be456d")
	case "Amber":
		t.Accent = ui.Hex("#986314")
	case "Violet":
		t.Accent = ui.Hex("#8258cf")
	}
	if dark && accent != "" && accent != "Indigo" {
		t.Accent = t.Accent.Mix(ui.Hex("#ffffff"), 0.32)
	}
	t.AccentHover = t.Accent.Mix(t.Text, 0.12)
	t.AccentPressed = t.Accent.Mix(t.Text, 0.22)
	t.Focus = t.Accent.Alpha(.7)
	t.Selection = t.Accent.Alpha(.2)
	t.Scrollbar = t.TextMuted.Alpha(.35)
	t.Danger = ui.Hex("#b91c1c")
	t.Warning = ui.Hex("#b45309")
	t.Success = ui.Hex("#15803d")
	if dark {
		t.Danger = ui.Hex("#f87171")
		t.Warning = ui.Hex("#fbbf24")
		t.Success = ui.Hex("#4ade80")
	}
	return t
}
func surface(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Surface
	}
	return ui.Hex("#ffffff")
}
func panel(c *ui.Context, e ui.Element) ui.Element {
	return e.Background(surface(c.Theme())).Radius(12).Border(1, c.Theme().Border).Clip()
}

// Short control labels must keep their intrinsic width. MyGo's SingleLine
// labels can ellipsize at fractional widths even inside a content-sized button.
func textButton(c *ui.Context, label string) ui.Element {
	b := ui.Button(c, "").Label(label)
	b.Children(func() { ui.Text(c, label).NoWrap().Shrink(0) })
	return b
}
func menuButton(c *ui.Context, label string, build func(*ui.Menu)) ui.Element {
	b := ui.Button(c, "").Label(label).Menu(build)
	b.Children(func() {
		ui.Text(c, label).NoWrap().Shrink(0)
		ui.Icon(c, icon("dropdown")).Size(12, 12).Shrink(0)
	})
	return b
}
func iconButton(c *ui.Context, name, label string, fn func()) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(label).Size(32, 32).Radius(7).Center().OnClick(fn)
	if b.Hovered() {
		b.Background(c.Theme().SurfaceHover)
	}
	b.Children(func() { ui.Icon(c, icon(name)).Size(18, 18) })
	return b
}
func action(c *ui.Context, name, label string, primary bool, fn func()) ui.Element {
	b := ui.ButtonBase(c).Label(label).Padding(9, 12).Gap(7).Radius(8).AlignSelf(ui.Start).Shrink(0).OnClick(fn)
	if primary {
		b.Background(c.Theme().Accent).TextColor(c.Theme().AccentText)
	} else {
		b.Background(c.Theme().Surface).TextColor(c.Theme().Text)
	}
	b.Children(func() {
		ui.Icon(c, icon(name)).Size(16, 16).Shrink(0)
		ui.Text(c, label).FontSize(12).FontWeight(500).NoWrap().Shrink(0)
	})
	return b
}

const windowTitlebarHeight = 36

// Keep the OS title for task switching and accessibility, while the client
// draws the sidebar header and a titlebar beneath the native window controls.
func newAppWindow(opts mygo.WindowOptions, theme func() *ui.Theme) *mygo.Window {
	opts.TitleBarStyle = mygo.TitleBarHidden
	opts.TitleBarHeight = windowTitlebarHeight
	t := theme()
	opts.BackgroundColor = fmt.Sprintf("#%02x%02x%02x", t.Background.R, t.Background.G, t.Background.B)
	w := mygo.NewWindow(opts)
	installWindowTheme(w, theme)
	return w
}

func windowTitlebar(c *ui.Context) {
	bar := c.TitleBar()
	ui.Row(c).Label("Window titlebar").Height(max(windowTitlebarHeight, bar.Height)).Shrink(0).
		FillWidth().Padding(0, bar.Right, 0, bar.Left).DragWindow().
		Background(c.Theme().Background)
}

var iconCache = map[string]*ui.SVG{}

func icon(name string) *ui.SVG {
	if v := iconCache[name]; v != nil {
		return v
	}
	if name == "logo" {
		v := ui.MustParseSVG(logoSVG)
		iconCache[name] = v
		return v
	}
	paths := map[string]string{
		"logo":       `<rect x="3" y="5" width="18" height="14" rx="3"/><path d="m4 7 8 5 8-5M12 12v7M3 13h5M16 13h5"/>`,
		"inbox":      `<path d="M4 4h16l2 10v6H2v-6zM2 14h6l2 3h4l2-3h6"/>`,
		"mail":       `<rect x="3" y="5" width="18" height="14" rx="3"/><path d="m3 7 9 6 9-6"/>`,
		"compose":    `<path d="M13 5H5v15h15v-8M10 14l1-4L20 1l3 3-9 9z"/>`,
		"send":       `<path d="m3 3 19 9-19 9 4-9zM7 12h15"/>`,
		"star":       `<path d="m12 2 3 6 7 1-5 5 1 8-6-4-6 4 1-8-5-5 7-1z"/>`,
		"clock":      `<circle cx="12" cy="12" r="9"/><path d="M12 6v6l4 2"/>`,
		"archive":    `<path d="M4 8v13h16V8M2 3h20v5H2zM9 12h6"/>`,
		"trash":      `<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7"/>`,
		"settings":   `<circle cx="12" cy="12" r="4"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M19 5l-2 2M7 17l-2 2"/>`,
		"search":     `<circle cx="10" cy="10" r="7"/><path d="m15 15 6 6"/>`,
		"sync":       `<path d="M20 8a9 9 0 0 0-16-3L1 8M1 2v6h6M4 16a9 9 0 0 0 16 3l3-3M23 22v-6h-6"/>`,
		"tag":        `<path d="M3 3h9l10 10-9 9L3 12z"/><circle cx="8" cy="8" r="1"/>`,
		"folder":     `<path d="M2 5h8l3 3h9v13H2z"/>`,
		"chevron":    `<path d="m8 4 8 8-8 8"/>`,
		"dropdown":   `<path d="m6 9 6 6 6-6"/>`,
		"grip":       `<path d="M9 5v.01M15 5v.01M9 12v.01M15 12v.01M9 19v.01M15 19v.01"/>`,
		"close":      `<path d="m6 6 12 12M18 6 6 18"/>`,
		"plus":       `<path d="M12 4v16M4 12h16"/>`,
		"reply":      `<path d="m9 4-7 7 7 7M2 11h12c5 0 8 4 8 9"/>`,
		"forward":    `<path d="m15 4 7 7-7 7M22 11H10c-5 0-8 4-8 9"/>`,
		"attachment": `<path d="m8 13 7-7a3 3 0 0 1 4 4L9 20a5 5 0 0 1-7-7L13 2a2 2 0 0 1 3 0M6 15l8-8"/>`,
		"download":   `<path d="M12 2v14m-5-5 5 5 5-5M3 17v5h18v-5"/>`,
		"more":       `<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>`,
		"check":      `<path d="m4 12 5 5L21 5"/>`,
		"sun":        `<circle cx="12" cy="12" r="4"/><path d="M12 1v2M12 21v2M1 12h2M21 12h2M4 4l2 2M18 18l2 2M20 4l-2 2M6 18l-2 2"/>`,
		"shield":     `<path d="m12 2 9 4v6c0 5-9 10-9 10S3 17 3 12V6zM8 12l3 3 5-6"/>`,
	}
	p := paths[name]
	if p == "" {
		p = paths["mail"]
	}
	v := ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">` + p + `</svg>`))
	iconCache[name] = v
	return v
}
