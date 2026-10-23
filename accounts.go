package main

import (
	"bentomail/internal/mailclient"
	"github.com/egoist/mygo/ui"
)

func (a *app) accountAvatar(c *ui.Context, ac mailclient.Account) {
	if ac.ID == "" {
		ui.Box(c).Label("All accounts avatar").Size(28, 28).Shrink(0).Radius(14).Center().Background(a.theme.Accent.Alpha(.15)).TextColor(a.theme.Accent).Children(func() {
			ui.Icon(c, icon("mail")).Size(16, 16)
		})
		return
	}
	bg, fg := avatarColors(a.theme, ac.Email)
	name := ac.Name
	if name == "" {
		name = ac.Email
	}
	ui.Box(c).Label("Account avatar for "+name).Size(28, 28).Shrink(0).Radius(14).Center().Background(bg).TextColor(fg).Children(func() {
		ui.Text(c, initials(name)).FontSize(10).Bold()
	})
}

func (a *app) accountChoice(c *ui.Context, ac mailclient.Account) {
	if ac.ID == "" {
		a.accountAvatar(c, ac)
		ui.Text(c, "All accounts").FontSize(12).Grow(1).MaxLines(1)
		return
	}
	a.accountAvatar(c, ac)
	ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
		ui.Text(c, ac.Name).FontSize(12).FontWeight(500).MaxLines(1)
		ui.Text(c, ac.Email).FontSize(10).TextColor(a.theme.TextMuted).MaxLines(1)
	})
}

func (a *app) accountSwitcher(c *ui.Context) {
	t := a.theme
	sel := ui.SelectBase(c.Key("account-switcher"), &a.accountID)
	var active mailclient.Account
	for _, ac := range a.state.Accounts {
		if ac.ID == a.accountID {
			active = ac
			break
		}
	}
	label := "All accounts"
	if active.ID != "" {
		label = active.Name + " <" + active.Email + ">"
	}
	trigger := sidebarFocus(c, sel.Trigger, false).Label("Active account").Size(36, 36).Shrink(0).Center().Radius(18).Description("Switch account: " + label).OnChange(func() {
		a.updateAccountPick()
		a.navigate(a.folder)
	})
	if trigger.Hovered() || sel.Open() {
		trigger.Background(t.SurfaceHover)
	}
	trigger.Children(func() { a.accountAvatar(c, active) })
	sel.Popup(func(p ui.Element) {
		frost(p, overlaySurface(t)).Label("Account choices").Width(300).Padding(6).Margin(8, 0, 0, 0).Radius(panelRadius).Border(1, t.Border).
			Shadow(0, 8, 24, 0, t.Text.Alpha(.12)).Transition(popupTransition())
		ui.Text(c, "Switch account").FontSize(10).TextColor(t.TextMuted).Padding(8, 9)
		ui.Scroll(c).FillWidth().MaxHeight(360).Children(func() {
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				choices := append([]mailclient.Account{{}}, a.state.Accounts...)
				for _, ac := range choices {
					label := "All accounts"
					if ac.ID != "" {
						label = ac.Name + " <" + ac.Email + ">"
					}
					item := sel.Item(ac.ID).Label(label).FillWidth().Padding(7, 9).Gap(8).AlignItems(ui.Center).Radius(6)
					if item.Highlighted() {
						item.Background(t.SurfaceHover)
					}
					item.Transition(controlTransition()).Children(func() {
						a.accountChoice(c, ac)
						if ac.ID == a.accountID {
							ui.Icon(c, icon("check")).Size(14, 14).Shrink(0).TextColor(t.Accent)
						}
					})
				}
			})
		})
	})
}
