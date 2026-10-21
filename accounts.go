package main

import (
	"bentomail/internal/mailclient"
	"github.com/egoist/mygo/ui"
)

func (a *app) accountAvatar(c *ui.Context, ac mailclient.Account) {
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
		ui.Box(c).Size(28, 28).Shrink(0).Radius(14).Center().Background(a.theme.Accent.Alpha(.12)).TextColor(a.theme.Accent).Children(func() {
			ui.Icon(c, icon("mail")).Size(16, 16)
		})
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
	sel.Trigger.Label("Active account").Width(245).Height(44).Padding(5, 9).Gap(8).AlignItems(ui.Center).Radius(8).Background(t.Surface).Border(1, t.Border).OnChange(func() {
		a.updateAccountPick()
		a.navigate(a.folder)
	}).Children(func() {
		var ac mailclient.Account
		for _, account := range a.state.Accounts {
			if account.ID == a.accountID {
				ac = account
				break
			}
		}
		a.accountChoice(c, ac)
		ui.Icon(c, icon("dropdown")).Size(12, 12).Shrink(0)
	})
	sel.Popup(func(p ui.Element) {
		frost(p, overlaySurface(t)).Width(300).Padding(4).Radius(9).Border(1, t.Border)
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
					item.Children(func() { a.accountChoice(c, ac) })
				}
			})
		})
	})
}
