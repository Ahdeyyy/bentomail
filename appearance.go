package main

import (
	_ "embed"
	"fmt"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

//go:embed resources/logo.svg
var logoSVG []byte

const panelRadius = 12
const backdropBlurRadius = 32

func normalizedTranslucency(value float64) float64 {
	return mailclient.NormalizeTranslucency(value)
}

func workspaceTheme(dark bool, accent string, translucency ...float64) *ui.Theme {
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
	level := float64(mailclient.DefaultTranslucency)
	if len(translucency) > 0 {
		level = normalizedTranslucency(translucency[0])
	}
	frost := float32(level / 100)
	t.Background = t.Background.Alpha(1 - .85*frost)
	// Controls retain at least 94% opacity, independently of the panes.
	t.Surface = t.Surface.Alpha(1 - .06*frost)
	return t
}
func surface(t *ui.Theme) ui.Color {
	frost := (1 - float32(t.Background.A)/255) / .85
	color := ui.Hex("#ffffff")
	if t.Dark {
		color = t.Surface
	}
	return color.Alpha(1 - .55*frost)
}

// Floating surfaces need a stronger tint as well as blur to keep foreground
// text readable over both light and dark content at maximum translucency.
func overlaySurface(t *ui.Theme) ui.Color {
	color := surface(t)
	color.A = max(color.A, 204)
	return color
}

// Blur the backdrop, then tint it; text and controls remain sharp above it.
type frostedSurface struct{ tint ui.Color }

func (s frostedSurface) PaintMaterial(p *ui.Painter, box ui.Rect, radii [4]float32) {
	glass.Blur{Radius: backdropBlurRadius}.PaintMaterial(p, box, radii)
	p.Fill(box, s.tint, radii[0])
}

func frost(e ui.Element, color ui.Color) ui.Element {
	if color.A < 255 {
		return e.Material(frostedSurface{tint: color})
	}
	return e.Background(color)
}
func panel(c *ui.Context, e ui.Element) ui.Element {
	return frost(e, surface(c.Theme())).Radius(panelRadius).Border(1, c.Theme().Border).Clip()
}

func setWindowTheme(c *ui.Context, t *ui.Theme) {
	c.SetTheme(t)
	if !c.Vibrancy() {
		// Unsupported platforms and software renders retain a solid base.
		bg := t.Background
		bg.A = 255
		c.Root().Background(bg)
	}
}

func (a *app) confirmationDialog(c *ui.Context) {
	ui.DialogBase(c.Key("confirmation"), &a.confirmOpen, func(backdrop, dialog ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, .22))
		dialog.Label(a.confirmTitle).Width(440).MaxWidth(560).Padding(24).Gap(18).
			Radius(panelRadius).Border(1, c.Theme().Border).
			Shadow(0, 12, 36, 0, ui.RGBA(0, 0, 0, .2))
		frost(dialog, overlaySurface(c.Theme()))
		ui.Text(c, a.confirmTitle).FontSize(18).Bold().FillWidth()
		ui.Text(c, a.confirmMessage).FontSize(13).LineHeight(1.6).FillWidth().TextColor(c.Theme().TextMuted)
		ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
			textButton(c, "Cancel").AutoFocus().OnClick(func() { a.confirmOpen = false; a.confirmAction = nil })
			ui.PrimaryButton(c, "Continue").OnClick(func() {
				fn := a.confirmAction
				a.confirmOpen, a.confirmAction = false, nil
				if fn != nil {
					fn()
				}
			})
		})
	})
	if !a.confirmOpen {
		a.confirmAction = nil
	}
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

// SelectBase preserves native keyboard/focus behavior while giving the popup
// the same backdrop material as the app's other floating surfaces.
func selectInput(c *ui.Context, selected *string, options []string) ui.Element {
	t := c.Theme()
	sel := ui.SelectBase(c, selected)
	b := sel.Trigger.Padding(t.Space(1.5), t.Space(3.5)).Gap(t.Space(1.5)).Radius(t.Radius).
		MinWidth(t.Space(35)).Justify(ui.SpaceBetween).AlignItems(ui.Center).
		Background(t.Surface).TextColor(t.Text).Border(1, t.Border)
	if b.Pressed() {
		b.Background(t.SurfacePressed)
	} else if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Children(func() {
		ui.Text(c, *selected).SingleLine()
		ui.Icon(c, icon("dropdown")).Size(12, 12).Shrink(0)
	})
	sel.Popup(func(p ui.Element) {
		frost(p, overlaySurface(t)).Margin(t.Space(1), 0, 0, 0).Padding(t.Space(1)).Radius(t.Radius+2).
			Border(1, t.Border).Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, .18))
		for _, option := range options {
			item := sel.Item(option).Padding(t.Space(1.5), t.Space(2.5)).Radius(t.Radius)
			if item.Highlighted() {
				item.Background(t.Accent).TextColor(t.AccentText)
			} else if option == *selected {
				item.Background(t.Surface)
			}
			item.Children(func() { ui.Text(c, option).SingleLine() })
		}
	})
	return b
}

func recipientAutocomplete(c *ui.Context, value *string, suggestions []string) ui.Element {
	// The built-in autocomplete owns its popup. Give it an opaque theme
	// background so suggestions cannot mix with text beneath that popup.
	theme := c.Theme()
	popupTheme := *theme
	popupTheme.Background = overlaySurface(theme)
	popupTheme.Background.A = 255
	c.SetTheme(&popupTheme)
	input := ui.Autocomplete(c, value, suggestions)
	setWindowTheme(c, theme)
	return input
}
func iconButton(c *ui.Context, name, label string, fn func()) ui.Element {
	return iconButtonBase(c, name, label, fn).Tooltip(label)
}

func iconButtonBase(c *ui.Context, name, label string, fn func()) ui.Element {
	b := ui.ButtonBase(c).Label(label).Size(32, 32).Radius(7).Center().OnClick(fn)
	if b.Hovered() {
		b.Background(c.Theme().SurfaceHover)
	}
	b.Children(func() { ui.Icon(c, icon(name)).Size(18, 18) })
	return b
}

// Scroll clips anything outside its viewport, including the default outer
// focus ring. Keep sidebar outlines inside the button, including when the
// keyboard scrolls the first or last visible item into view.
func sidebarFocus(c *ui.Context, b ui.Element, primary bool) ui.Element {
	b.FocusRing(false)
	if b.FocusVisible() {
		color := c.Theme().Focus
		if primary {
			color = c.Theme().AccentText
		}
		b.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Stroke(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, color, 6, 2)
		})
	}
	return b
}

func sidebarTooltip(c *ui.Context, b ui.Element, label string) {
	b.Description(label)
	t := c.Theme()
	ui.TooltipBase(c, b, func(tip ui.Element) {
		frost(tip, overlaySurface(t)).Padding(6, 9).Radius(6).Border(1, t.Border).
			TextColor(t.Text).FontSize(11).MaxWidth(280).
			AttachTo(b, ui.AnchorEnd, ui.AnchorStart).MarginStart(12).
			Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, .2)).
			Children(func() { ui.Text(c, label) })
	})
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
	// Create with a material even at 0% so the setting can change live on Windows.
	opts.Vibrancy = mygo.VibrancyAcrylic
	t := theme()
	opts.BackgroundColor = fmt.Sprintf("#%02x%02x%02x", t.Background.R, t.Background.G, t.Background.B)
	w := mygo.NewWindow(opts)
	installWindowTheme(w, theme)
	return w
}

func windowTitlebar(c *ui.Context) {
	bar := c.TitleBar()
	ui.Row(c).Label("Window titlebar").Height(max(windowTitlebarHeight, bar.Height)).Shrink(0).
		FillWidth().Padding(0, bar.Right, 0, bar.Left).DragWindow()
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
		"settings":   `<path d="m9.17 5.16 1.09-3.01h3.48l1.09 3.01 2.91-1.35 2.45 2.45-1.35 2.91 3.01 1.09v3.48l-3.01 1.09 1.35 2.91-2.45 2.45-2.91-1.35-1.09 3.01h-3.48l-1.09-3.01-2.91 1.35-2.45-2.45 1.35-2.91-3.01-1.09v-3.48l3.01-1.09-1.35-2.91 2.45-2.45z"/><circle cx="12" cy="12" r="3"/>`,
		"search":     `<circle cx="10" cy="10" r="7"/><path d="m15 15 6 6"/>`,
		"sync":       `<path d="M20 8a9 9 0 0 0-16-3L1 8M1 2v6h6M4 16a9 9 0 0 0 16 3l3-3M23 22v-6h-6"/>`,
		"tag":        `<path d="M3 3h9l10 10-9 9L3 12z"/><circle cx="8" cy="8" r="1"/>`,
		"folder":     `<path d="M2 5h8l3 3h9v13H2z"/>`,
		"chevron":    `<path d="m8 4 8 8-8 8"/>`,
		"dropdown":   `<path d="m6 9 6 6 6-6"/>`,
		"sidebar":    `<rect x="3" y="3" width="18" height="18" rx="3"/><path d="M9 3v18"/>`,
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
