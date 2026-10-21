package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo"
	updatenative "github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

func main() {
	mygo.Use(updatenative.Plugin)
	base, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	data := flag.String("data", filepath.Join(base, "Bentomail"), "local mail data directory")
	flag.Parse()
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	store, err := mailclient.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	a := newApp(store)
	mygo.App.OnSecondInstance(func([]string, string) {
		if a.window != nil {
			a.window.Show()
			a.window.Focus()
		}
	})
	mygo.App.WhenReady(func() {
		source := mygo.ThemeLight
		if a.prefs.Dark {
			source = mygo.ThemeDark
		}
		mygo.Theme.SetSource(source)
		win := newAppWindow(mygo.WindowOptions{Title: "Bentomail", Width: 1440, Height: 900, MinWidth: 1000, MinHeight: 680, StateKey: "main", Content: ui.View(a.view)}, func() *ui.Theme { return a.theme })
		a.window = win
		a.dispatch = win.Update
		if mygo.WebViewAvailable() {
			v, e := win.NewWebView(mygo.WebViewOptions{BackgroundColor: "#ffffff", Page: mygo.PageOptions{DevTools: mygo.DevToolsDisabled}})
			if e == nil {
				a.preview = v
				a.loadHTML = func(html string) { v.Page().LoadHTML(html, "") }
				a.print = v.Page().Print
				v.Page().OnWillNavigate(a.previewNavigation)
				v.Page().OnDidFinishLoad(func() {
					if a.printPending {
						a.printPending = false
						v.Page().Print()
					}
				})
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		a.ctx = ctx
		a.startWorker(ctx)
		a.setupTray()
		mygo.App.OnBeforeQuit(a.prepareQuit)
		win.OnClose(a.closeRequested)
		win.OnClosed(func() {
			a.closed = true
			cancel()
			if a.tray != nil {
				a.tray.Destroy()
			}
		})
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					win.Update(func() {
						if !a.closed {
							a.tick()
						}
					})
				}
			}
		}()
		a.syncMail()
		a.refreshWatchers()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
