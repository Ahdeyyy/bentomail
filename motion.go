package main

import (
	"time"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo/ui"
)

const sidebarDuration = 240 * time.Millisecond
const mailScreenDuration = 300 * time.Millisecond

func (a *app) animationDuration(base time.Duration) time.Duration {
	return time.Duration(float64(base) / mailclient.NormalizeAnimationSpeed(a.prefs.AnimationSpeed))
}

// Keep the split and its children alive in both sidebar modes, so that
// collapsing preserves list scroll, focus, and message content.
type sidebarMotion struct {
	width, rendered float32
	resizable       bool
}

type avatarMotion struct {
	generation uint64
	screen     string
	mounted    bool
	until      time.Time
}

func (a *app) updateAvatarMotion(c *ui.Context) {
	s := &a.avatarMotion
	if a.page != "mail" || a.prefs.Layout != "Hidden" || a.prefs.Density == "Compact" {
		if s.mounted {
			s.generation++
		}
		s.mounted, s.until = false, time.Time{}
		return
	}
	if !s.mounted {
		s.mounted, s.screen = true, a.selectedID
	} else if s.screen != a.selectedID {
		s.screen = a.selectedID
		s.until = c.Now().Add(a.animationDuration(mailScreenDuration))
	}
	if !s.until.IsZero() {
		if remaining := s.until.Sub(c.Now()); remaining > 0 {
			c.After(remaining)
		} else {
			// Retire the morph's channels at its deadline. Resting avatars
			// register a fresh shared identity for the next navigation, with
			// no lingering match that could chase a scrolling destination.
			s.generation++
			s.until = time.Time{}
		}
	}
}

func (a *app) captureSidebarResize() {
	s := &a.sidebarMotion
	// Split writes its bound value after construction. Consume that edit
	// before deriving the next frame's animated width.
	if s.resizable && !a.prefs.SidebarCollapsed && s.width != s.rendered {
		a.prefs.SidebarWidth = s.width
	}
}

func (a *app) animateSidebar(root ui.Element) {
	target := float32(1)
	if a.prefs.SidebarCollapsed {
		target = 0
	}
	progress := root.Animate("sidebar-expanded", target, a.animationDuration(sidebarDuration))
	s := &a.sidebarMotion
	s.width = 80 + (a.prefs.SidebarWidth-80)*progress
	s.rendered = s.width
	s.resizable = !a.prefs.SidebarCollapsed && root.Settled("sidebar-expanded")
}

func (a *app) workspaceSplit(c *ui.Context, content func()) {
	ui.Row(c.Key("workspace-content")).Fill().Children(func() {
		s := &a.sidebarMotion
		paneSplit(c.Key("workspace-split"), &s.width, false, func() { a.sidebar(c) }, content).Fill()
	})
}

func (a *app) mailScreenTransition(x float32) ui.ElementTransition {
	// Keep the screen opaque so the matched avatar remains visible throughout
	// its flight. The framework handles reduced motion for both transitions.
	return ui.ElementTransition{Duration: a.animationDuration(mailScreenDuration), Colors: true, Enter: &ui.Motion{X: x, Opacity: 1}}
}

// Message identity prevents two messages from the same sender matching.
type messageAvatarMatch struct {
	accountID, messageID string
	generation           uint64
}

type messageAvatarKey uint64

func (a *app) messageAvatar(c *ui.Context, m mailclient.Message, reading bool) ui.Element {
	name, _ := sender(m.From)
	background, text := avatarColors(a.theme, m.From)
	size, radius, fontSize := float32(32), float32(11), float32(11)
	if reading {
		size, radius, fontSize = 38, 13, 13
	}
	avatar := ui.Box(c.Key(messageAvatarKey(a.avatarMotion.generation))).Label("Sender avatar "+m.ID).
		Size(size, size).Shrink(0).Radius(radius).Center().Background(background).TextColor(text)
	if a.prefs.Layout == "Hidden" && a.prefs.Density != "Compact" {
		avatar.Transition(ui.ElementTransition{
			Duration: a.animationDuration(mailScreenDuration),
			// Match handles the screen morph; ordinary layout and scrolling
			// must follow the row immediately rather than animate position.
			Colors: true,
			Match:  messageAvatarMatch{m.AccountID, m.ID, a.avatarMotion.generation},
		})
	}
	avatar.Children(func() { ui.Text(c, initials(name)).FontSize(fontSize).Bold() })
	return avatar
}
