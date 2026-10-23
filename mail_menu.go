package main

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo/ui"
)

type mailMenuAction struct {
	label    string // Empty labels separate groups.
	disabled bool
	run      func()
}

type mailMenuState struct {
	open       bool
	messageID  string
	x, y       float32
	generation int
	actions    []mailMenuAction
	handles    []ui.Handle
	reveal     int
	origin     ui.Handle
}

// Use a real scroll view rather than the OS menu's scroll-arrow controls.
func (a *app) openMailMenu(c *ui.Context, m mailclient.Message, x, y float32, origin ui.Handle) {
	s := &a.mailMenu
	s.generation++
	s.open, s.messageID, s.x, s.y = true, m.ID, x, y
	s.actions = a.messageMenuActions(m)
	s.handles = make([]ui.Handle, len(s.actions))
	s.reveal, s.origin = -1, origin
	s.handles[0].Focus(c)
}

func (a *app) mailContextTarget(c *ui.Context, row ui.Element, m mailclient.Message) {
	h := ui.Local(row, "mail-menu-anchor", func() ui.Handle { return ui.Handle{} })
	row.Bind(h).HandleInput(func(ev ui.InputEvent) bool {
		secondary := ev.Button == 1 || runtime.GOOS == "darwin" && ev.Button == 0 && ev.Mods == ui.Ctrl
		if ev.Kind == ui.InputPointerDown && secondary {
			return true
		}
		if ev.Kind == ui.InputPointerUp && secondary {
			r := h.Bounds(c)
			a.openMailMenu(c, m, r.X+ev.X, r.Y+ev.Y, a.list.Handle)
			return true
		}
		return false
	})
}

func (a *app) openSelectedMailMenu(c *ui.Context) {
	ms := a.messages()
	if len(ms) == 0 {
		return
	}
	i := max(0, min(a.rowIndex, len(ms)-1))
	r := a.list.Handle.Bounds(c)
	a.openMailMenu(c, ms[i], r.X+20, r.Y+16, a.list.Handle)
}

func (a *app) mailMoreButton(c *ui.Context, m mailclient.Message) {
	open := func() {
		r := a.mailMenuTrigger.Bounds(c)
		a.openMailMenu(c, m, r.X, r.Y+r.H+4, a.mailMenuTrigger)
	}
	ui.Button(c, "").Label("More").Bind(&a.mailMenuTrigger).Width(100).OnClick(open).
		OnShortcut(0, ui.KeyDown, open).Children(func() {
		ui.Text(c, "More").NoWrap().Shrink(0)
		ui.Icon(c, icon("dropdown")).Size(12, 12).Shrink(0)
	})
}

func (a *app) moveMailMenuFocus(c *ui.Context, start, step int) {
	s := &a.mailMenu
	for i, checked := start, 0; checked < len(s.actions); i, checked = (i+step+len(s.actions))%len(s.actions), checked+1 {
		i = (i + len(s.actions)) % len(s.actions)
		if action := s.actions[i]; action.label != "" && !action.disabled {
			s.reveal = i
			s.handles[i].Focus(c)
			return
		}
	}
}

func (a *app) mailContextMenu(c *ui.Context) {
	s := &a.mailMenu
	if !s.open {
		return
	}
	if _, ok := a.findMessage(s.messageID); !ok || a.page != "mail" {
		s.open = false
		return
	}
	width, height := c.Size()
	menuHeight := float32(12)
	for _, action := range s.actions {
		if action.label == "" {
			menuHeight += 9
		} else {
			menuHeight += 30
		}
	}
	ui.Overlay(c.Key("email-menu-overlay"), func() {
		anchor := ui.Box(c).Absolute().Left(s.x).Top(s.y).Size(1, 1)
		ui.PopoverBase(c.Key(fmt.Sprintf("email-menu-%d", s.generation)), anchor, &s.open, func(p ui.Element) {
			t := a.theme
			frost(p, overlaySurface(t)).Label("Email context menu").Role(ui.RoleMenu).
				Width(min(328, width-16)).Height(min(menuHeight, 400, height-16)).Padding(6).Radius(panelRadius).
				Border(1, t.Border).Shadow(0, 8, 24, 0, t.Text.Alpha(.15)).Clip().Transition(popupTransition())
			ui.Scroll(c).Label("Email menu items").Fill().Padding(0, 10, 0, 0).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for i, action := range s.actions {
						if action.label == "" {
							ui.Box(c.Key(i)).FillWidth().Height(1).Margin(4, 6).Background(t.Border)
							continue
						}
						b := ui.ButtonBase(c.Key(i)).Bind(&s.handles[i]).Label(action.label).Role(ui.RoleMenuItem).
							FillWidth().Height(30).Shrink(0).Padding(0, 10).Radius(6).AlignItems(ui.Center).Justify(ui.Start).
							Disabled(action.disabled).TextColor(t.Text).Transition(controlTransition())
						if action.disabled {
							b.TextColor(t.TextMuted)
						} else if b.Hovered() || b.FocusVisible() {
							b.Background(t.SurfaceHover)
						}
						b.Children(func() { ui.Text(c, action.label).FontSize(12).MaxLines(1) })
						b.OnClick(func() { s.open = false; action.run() }).
							OnShortcut(0, ui.KeyDown, func() { a.moveMailMenuFocus(c, i+1, 1) }).
							OnShortcut(0, ui.KeyUp, func() { a.moveMailMenuFocus(c, i-1, -1) }).
							OnShortcut(0, ui.KeyHome, func() { a.moveMailMenuFocus(c, 0, 1) }).
							OnShortcut(0, ui.KeyEnd, func() { a.moveMailMenuFocus(c, len(s.actions)-1, -1) }).
							OnShortcut(0, ui.KeyEscape, func() { s.open = false; s.origin.Focus(c) }).
							OnShortcut(0, ui.KeyTab, func() { s.open = false; s.origin.Focus(c) }).
							OnShortcut(ui.Shift, ui.KeyTab, func() { s.open = false; s.origin.Focus(c) })
						if s.reveal == i {
							b.ScrollIntoView()
							s.reveal = -1
						}
					}
				})
			})
		})
	})
}

func (a *app) messageMenuActions(m mailclient.Message) []mailMenuAction {
	var actions []mailMenuAction
	add := func(label string, run func()) { actions = append(actions, mailMenuAction{label: label, run: run}) }
	separator := func() { actions = append(actions, mailMenuAction{}) }
	add("Reply", func() { a.newComposer(&m, "reply") })
	add("Forward as attachment", func() { a.newComposer(&m, "forward-attachment") })
	separator()
	add("Toggle read / unread", func() { m.Read = !m.Read; a.mutate(m, true) })
	add("Toggle importance", func() { m.Important = !m.Important; a.mutate(m, false) })
	add("Follow up tomorrow", func() { m.FollowUp = time.Now().Add(24 * time.Hour); a.mutate(m, false) })
	add("Clear follow-up", func() { m.FollowUp = time.Time{}; a.mutate(m, false) })
	categories := append([]string{"Work", "Finance", "Clients", "Personal"}, a.state.Categories...)
	categories = append(categories, "")
	seen := map[string]bool{}
	for _, cat := range categories {
		if seen[cat] {
			continue
		}
		seen[cat] = true
		label := cat
		if label == "" {
			label = "Clear category"
		}
		add("Category: "+label, func() { m.Category = cat; a.mutate(m, false) })
	}
	for _, folder := range a.state.Folders[m.AccountID] {
		add("Move to "+folder, func() { m.Folder = folder; a.mutate(m, true) })
	}
	separator()
	add("Save as .eml", func() { a.exportEML(m) })
	add("Open in a new window", func() { a.openMessageWindow(m) })
	add("Trust this sender's remote images", func() {
		_, address := sender(m.From)
		address = strings.ToLower(address)
		if !slices.Contains(a.prefs.TrustedSenders, address) {
			a.prefs.TrustedSenders = append(a.prefs.TrustedSenders, address)
			a.savePrefs()
		}
	})
	if m.UID == 0 && m.Folder == "Local archive" {
		add("Delete this local archived copy", func() {
			s, id := a.store, m.ID
			a.confirm("Delete local copy permanently?", "This removes the downloaded message and its attachments from Bentomail.", func() {
				a.run("Local copy deleted", func() error { return s.DeleteMessage(id) }, func(err error) {
					if err == nil {
						a.selectedID = ""
					}
				})
			})
		})
	}
	actions = append(actions, mailMenuAction{label: "Print", disabled: a.print == nil, run: func() {
		a.printPending = true
		a.previewKey = ""
		a.selectMessage(m.ID)
	}})
	return actions
}
