package main

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

//go:embed resources/icon-32.png
var appIcon []byte

func (a *app) setupTray() {
	if !a.prefs.Background || a.tray != nil {
		return
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{{Label: "Open Bentomail", Click: func(*mygo.MenuItem, *mygo.Window) { a.window.Show(); a.window.Focus() }}, {Label: "Compose", Click: func(*mygo.MenuItem, *mygo.Window) { a.newComposer(nil, "new"); a.window.Show(); a.window.Focus() }}, {Label: "Sync mail", Click: func(*mygo.MenuItem, *mygo.Window) { a.syncMail() }}, mygo.Separator(), {Label: "Quit Bentomail", Click: func(*mygo.MenuItem, *mygo.Window) { a.quitRequested = true; a.window.Close() }}})
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: appIcon, ToolTip: "Bentomail", Menu: menu})
	if err == nil {
		a.tray = tray
		tray.OnClick(func() { a.window.Show(); a.window.Focus() })
	}
}
func (a *app) closeRequested(event *mygo.CloseEvent) {
	if a.allowClose {
		return
	}
	event.PreventDefault()
	if a.prefs.Background && !a.quitRequested && a.tray != nil {
		a.saveOpenDrafts()
		a.window.Hide()
		return
	}
	if a.closing {
		return
	}
	a.closing = true
	drafts := []mailclient.Draft{}
	for _, co := range a.composers {
		drafts = append(drafts, co.snapshot())
	}
	s, p := a.store, a.prefs
	a.run("Mail saved", func() error {
		for _, d := range drafts {
			if _, err := s.SaveDraft(d); err != nil {
				return err
			}
		}
		return s.SavePreferences(p)
	}, func(err error) {
		a.closing = false
		if err != nil {
			return
		}
		a.allowClose = true
		a.window.Close()
	})
}

// Relaunch uses a synchronous quit event: canceling it to save asynchronously
// also cancels the relaunch. Flush local drafts before windows close so the
// updater can restart, including when closing a window would normally hide it.
func (a *app) prepareQuit(event *mygo.QuitEvent) {
	windows := []*app{}
	var collect func(*app)
	collect = func(w *app) {
		if !w.closed {
			windows = append(windows, w)
		}
		for _, child := range w.childWindows {
			collect(child)
		}
	}
	collect(a)
	for _, w := range windows {
		for _, co := range w.composers {
			if _, err := a.store.SaveDraft(co.snapshot()); err != nil {
				event.PreventDefault()
				w.notice("Could not save draft before quitting: "+err.Error(), true)
				return
			}
		}
	}
	if err := a.store.SavePreferences(a.prefs); err != nil {
		event.PreventDefault()
		a.notice("Could not save preferences before quitting: "+err.Error(), true)
		return
	}
	for _, w := range windows {
		w.allowClose = true
	}
}
func (a *app) updateBadge() {
	if a.window == nil {
		return
	}
	unread := 0
	for _, m := range a.state.Messages {
		if canonicalFolder(m.Folder) == "inbox" && !m.Read {
			unread++
		}
	}
	mygo.App.SetBadgeCount(unread)
	a.window.SetTitle("Bentomail" + map[bool]string{true: fmt.Sprintf(" · %d unread", unread), false: ""}[unread > 0])
	if a.tray != nil {
		a.tray.SetToolTip(fmt.Sprintf("Bentomail · %d unread", unread))
	}
}
func (a *app) openMessageWindow(m mailclient.Message) {
	b := newApp(a.store)
	a.childWindows = append(a.childWindows, b)
	b.accountID = m.AccountID
	b.selectedID = m.ID
	b.prefs.SidebarWidth = 200
	b.prefs.ListWidth = 300
	win := newAppWindow(mygo.WindowOptions{Title: m.Subject + " · Bentomail", Width: 1160, Height: 800, MinWidth: 1000, MinHeight: 680, Content: ui.View(b.view)}, func() *ui.Theme { return b.theme })
	b.window = win
	b.dispatch = win.Update
	ctx, cancel := context.WithCancel(context.Background())
	b.ctx = ctx
	b.startWorker(ctx)
	if mygo.WebViewAvailable() {
		v, err := win.NewWebView(mygo.WebViewOptions{BackgroundColor: "#ffffff", Page: mygo.PageOptions{DevTools: mygo.DevToolsDisabled}})
		if err == nil {
			b.preview = v
			b.loadHTML = func(markup string) { v.Page().LoadHTML(markup, "") }
			b.print = v.Page().Print
			v.Page().OnWillNavigate(b.previewNavigation)
			v.Page().OnDidFinishLoad(func() {
				if b.printPending {
					b.printPending = false
					v.Page().Print()
				}
			})
			b.updatePreview()
		}
	}
	win.OnClose(b.closeRequested)
	win.OnClosed(func() { b.closed = true; cancel() })
}
func (a *app) askOpenURL(raw string) {
	if !ui.SafeToOpen(raw) {
		a.notice("This link cannot be opened safely.", true)
		return
	}
	a.confirm("Open link in your browser?", raw, func() {
		if err := mygo.Shell.OpenExternal(raw); err != nil {
			a.notice(err.Error(), true)
		}
	})
}
func (a *app) previewNavigation(event *mygo.NavigateEvent) {
	event.PreventDefault()
	if event.UserInitiated && strings.TrimSpace(event.URL) != "" {
		a.askOpenURL(event.URL)
	}
}
