package main

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func saveLayoutMenuImage(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	if dir := os.Getenv("BENTOMAIL_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPaneWidthsRemainReadable(t *testing.T) {
	for _, width := range []float32{1000, 1440} {
		for _, sidebar := range []float32{20, 2000} {
			for _, list := range []float32{20, 2000} {
				t.Run(fmt.Sprintf("window=%g/sidebar=%g/list=%g", width, sidebar, list), func(t *testing.T) {
					a := seededApp(t)
					a.prefs.SidebarWidth, a.prefs.ListWidth = sidebar, list
					tt := ui.NewTester(a.view, int(width), 680)
					for _, tc := range []struct {
						label string
						min   float32
					}{{"Sidebar navigation", 224}, {"Messages pane", 300}, {"Reading pane", 400}} {
						r, ok := tt.Find(tc.label)
						if !ok || r.W < tc.min || r.X+r.W > width {
							t.Fatalf("%s is cramped or outside the window: %+v", tc.label, r)
						}
					}
				})
			}
		}
	}
}

func TestPaneDividerDragsRespectMinimumWidths(t *testing.T) {
	a := seededApp(t)
	tt := ui.NewTester(a.view, 1000, 680)
	for _, end := range []float32{10, 980} {
		tt.Press(a.prefs.SidebarWidth+.5, 240)
		tt.Move(end, 240)
		tt.Release(end, 240)
		tt.Frame()
		if a.prefs.SidebarWidth < 224 || a.prefs.SidebarWidth > 266 {
			t.Fatalf("sidebar drag escaped its limits: %g", a.prefs.SidebarWidth)
		}
	}
	for _, end := range []float32{10, 980} {
		r, _ := tt.Find("Messages pane")
		x := r.X + r.W + 6.5
		tt.Press(x, 240)
		tt.Move(end, 240)
		tt.Release(end, 240)
		tt.Frame()
		for _, tc := range []struct {
			label string
			min   float32
		}{{"Messages pane", 300}, {"Reading pane", 400}} {
			r, ok := tt.Find(tc.label)
			if !ok || r.W < tc.min || r.X+r.W > 1000 {
				t.Fatalf("divider drag cramped %s: %+v", tc.label, r)
			}
		}
	}
}

func TestMailContextMenuScrollsWithWheel(t *testing.T) {
	a := seededApp(t)
	a.networkJobs = make(chan work, 8)
	for i := 0; i < 24; i++ {
		a.state.Folders["work"] = append(a.state.Folders["work"], fmt.Sprintf("Folder %02d", i))
	}
	tt := ui.NewTester(a.view, 1000, 680)
	m := a.messages()[0]
	if err := tt.RightClick(m.Subject); err != nil {
		t.Fatal(err)
	}
	tt.Advance(200 * time.Millisecond)
	menu, ok := tt.Find("Email context menu")
	if !ok || !a.mailMenu.open || menu.H > 400 || menu.X+menu.W > 1000 || menu.Y+menu.H > 680 {
		t.Fatalf("scrollable context menu did not open within the window: %+v", menu)
	}
	if len(tt.Menu()) != 0 {
		t.Fatal("email still uses the OS menu with scroll arrows")
	}
	replyBefore, _ := tt.Find("Reply")
	viewport, _ := tt.Find("Email menu items")
	saveLayoutMenuImage(t, tt, "email-menu-top")
	tt.Scroll(viewport.X+30, viewport.Y+100, 0, 2000)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	saveLayoutMenuImage(t, tt, "email-menu-scrolled")
	replyAfter, _ := tt.Find("Reply")
	if replyAfter.H > 0 && replyAfter.Y >= replyBefore.Y {
		t.Fatalf("mouse wheel did not scroll: menu=%+v viewport=%+v reply=%+v => %+v", menu, viewport, replyBefore, replyAfter)
	}
	last, ok := tt.Find("Move to Folder 23")
	if !ok || last.Y < viewport.Y || last.Y+last.H > viewport.Y+viewport.H {
		t.Fatalf("last folder is not reachable by wheel scrolling: %+v", last)
	}
	a.prefs.Dark = true
	a.theme = workspaceTheme(true, "Indigo")
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	saveLayoutMenuImage(t, tt, "email-menu-dark-scrolled")
	if err := tt.Click("Move to Folder 23"); err != nil {
		t.Fatal(err)
	}
	updated, _ := a.findMessage(m.ID)
	if a.mailMenu.open || updated.Folder != "Folder 23" {
		t.Fatal("scrolled menu command did not run or close the popup")
	}
}

func TestMailContextMenuKeyboardAndDismissal(t *testing.T) {
	a := seededApp(t)
	tt := ui.NewTester(a.view, 1000, 680)
	if err := tt.Click(a.messages()[0].Subject); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Shift, ui.KeyF10)
	if !a.mailMenu.open || !tt.Focused("Reply") {
		t.Fatal("keyboard did not open and focus the email menu")
	}
	tt.Key(0, ui.KeyEnd)
	if !tt.Focused("Trust this sender's remote images") {
		t.Fatal("End did not reach the last enabled menu item")
	}
	tt.Key(0, ui.KeyEscape)
	if a.mailMenu.open || !tt.Focused("Message list") {
		t.Fatal("Escape did not close the menu and restore focus")
	}
	tt.Key(0, ui.KeyContextMenu)
	if err := tt.Click("Search mail"); err != nil {
		t.Fatal(err)
	}
	if a.mailMenu.open || !tt.Focused("Search mail") {
		t.Fatal("outside click did not dismiss the menu and focus its target")
	}
	if err := tt.Click("More"); err != nil {
		t.Fatal(err)
	}
	menu, ok := tt.Find("Email context menu")
	if !ok || menu.X+menu.W > 1000 || menu.Y+menu.H > 680 {
		t.Fatalf("More popup overflows the window: %+v", menu)
	}
	tt.Key(0, ui.KeyDown)
	if !tt.Focused("Forward as attachment") {
		t.Fatal("arrow key did not move to the next menu command")
	}
	tt.Key(0, ui.KeyEnter)
	if a.mailMenu.open || a.page != "compose" {
		t.Fatal("Enter did not execute the menu command")
	}
}
