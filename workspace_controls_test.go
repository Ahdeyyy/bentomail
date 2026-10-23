package main

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

func TestToolbarSharesTitlebarWithNativeControls(t *testing.T) {
	for _, bar := range []ui.TitleBar{{Height: 36, Right: 138}, {Height: 28, Left: 80}} {
		for _, collapsed := range []bool{false, true} {
			t.Run(fmt.Sprintf("left=%g/right=%g/collapsed=%v", bar.Left, bar.Right, collapsed), func(t *testing.T) {
				a := seededApp(t)
				a.prefs.SidebarCollapsed = collapsed
				tt := ui.NewTester(a.view, 1000, 680)
				tt.SetTitleBar(bar)
				header, ok := tt.Find("Window titlebar")
				if !ok || header.H != max(windowTitlebarHeight, bar.Height) {
					t.Fatalf("unexpected titlebar: %+v", header)
				}
				for _, label := range []string{"Search mail", "Sync mail", "Add account"} {
					r, ok := tt.Find(label)
					if !ok || r.Y < header.Y || r.Y+r.H > header.Y+header.H || r.X < bar.Left || r.X+r.W > 1000-bar.Right {
						t.Fatalf("%s does not fit beside the native controls: %+v", label, r)
					}
				}
				compose, ok := tt.Find("Compose")
				if !ok || compose.Y != header.Y+header.H+8 {
					t.Fatalf("unused toolbar space above sidebar: %+v", compose)
				}
				tt.Key(ui.Ctrl, ui.KeyF)
				tt.Type("timeline")
				if a.query != "timeline" {
					t.Fatal("titlebar search lost keyboard input")
				}
				if err := tt.Click("Add account"); err != nil {
					t.Fatal(err)
				}
				if a.page != "settings" || a.settingsTab != "Accounts" {
					t.Fatal("titlebar action did not open account settings")
				}
				canceled := false
				a.syncing, a.syncCancel = true, func() { canceled = true }
				tt.Frame()
				if err := tt.Click("Cancel synchronization"); err != nil {
					t.Fatal(err)
				}
				if !canceled {
					t.Fatal("titlebar sync action did not receive the click")
				}
			})
		}
	}
}

func TestSidebarKeyboardFocusOutline(t *testing.T) {
	for _, collapsed := range []bool{false, true} {
		for _, scale := range []float32{1, 1.25, 1.5} {
			t.Run(fmt.Sprintf("collapsed=%v/scale=%g", collapsed, scale), func(t *testing.T) {
				a := seededApp(t)
				a.prefs.SidebarCollapsed = collapsed
				a.prefs.Dark = true
				a.theme = workspaceTheme(true, "Indigo")
				tt := ui.NewTester(a.view, 1000, 680)
				tt.SetScale(scale)
				if err := tt.Click("Inbox"); err != nil {
					t.Fatal(err)
				}
				// Keep the pointer over navigation so its scrollbar is visible.
				tt.Frame()
				viewport, ok := tt.Find("Sidebar navigation")
				if !ok {
					t.Fatal("missing sidebar scroll viewport")
				}
				inbox, ok := tt.Find("Inbox")
				if !ok || inbox.W < 48 || viewport.X+viewport.W-inbox.X-inbox.W < 16 {
					t.Fatalf("no clear scrollbar gutter: viewport=%+v button=%+v", viewport, inbox)
				}
				r, ok := tt.Find("Starred")
				if !ok {
					t.Fatal("missing Starred button")
				}
				baseline := tt.Image()
				for i := 0; i < 2; i++ {
					tt.Key(0, ui.KeyTab)
					tt.Frame()
					tt.Frame()
					if !tt.Focused("Starred") {
						t.Fatal("Tab did not focus Starred")
					}
					focused := tt.Image()
					for _, point := range [][2]float32{
						{r.X + 2, r.Y + r.H/2}, {r.X + r.W - 2, r.Y + r.H/2},
						{r.X + r.W/2, r.Y + 2}, {r.X + r.W/2, r.Y + r.H - 2},
					} {
						x, y := int(math.Floor(float64(point[0]*scale))), int(math.Floor(float64(point[1]*scale)))
						if focused.RGBAAt(x, y) == baseline.RGBAAt(x, y) {
							t.Fatalf("keyboard focus ring missing inside edge at %.1f, %.1f", point[0], point[1])
						}
					}
					if dir := os.Getenv("BENTOMAIL_SCREENSHOTS"); dir != "" && i == 0 {
						if err := os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						f, err := os.Create(filepath.Join(dir, fmt.Sprintf("sidebar-focus-%v-%g.png", collapsed, scale)))
						if err != nil {
							t.Fatal(err)
						}
						err = png.Encode(f, focused)
						f.Close()
						if err != nil {
							t.Fatal(err)
						}
					}
					tt.Key(ui.Shift, ui.KeyTab)
					tt.Frame()
					if !tt.Focused("Inbox") {
						t.Fatal("Shift+Tab did not return focus to Inbox")
					}
				}
				// Reach items below the initial scroll viewport and the footer,
				// then traverse back without skipping or activating any control.
				order := []string{"Inbox", "Starred", "Snoozed", "Follow up", "Sent", "Drafts", "Outbox", "Archive", "All mail", "Trash", "Junk", "Manage tags", "Work", "Clients", "Finance", "Settings", "Update app", "Toggle dark mode"}
				for _, label := range order[1:] {
					tt.Key(0, ui.KeyTab)
					tt.Frame()
					if !tt.Focused(label) {
						t.Fatalf("Tab skipped %s", label)
					}
				}
				for i := len(order) - 2; i >= 0; i-- {
					tt.Key(ui.Shift, ui.KeyTab)
					tt.Frame()
					if !tt.Focused(order[i]) {
						t.Fatalf("Shift+Tab skipped %s", order[i])
					}
				}
			})
		}
	}
}

func TestSidebarCollapseAndAccountSwitcher(t *testing.T) {
	a := seededApp(t)
	a.prefs.SidebarWidth = 245
	tt := ui.NewTester(a.view, 1000, 680)
	for i := 0; i < 2; i++ {
		if err := tt.Click("Collapse sidebar"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(300 * time.Millisecond)
		tt.Frame()
		tt.Frame()
		if !a.prefs.SidebarCollapsed || !a.store.Snapshot().Preferences.SidebarCollapsed {
			t.Fatal("collapse was not saved")
		}
		for _, label := range []string{"Compose", "Inbox", "Settings", "Update app", "Expand sidebar"} {
			if _, ok := tt.Find(label); !ok {
				t.Fatalf("collapsed sidebar missing %q", label)
			}
		}
		if tt.HasText("WORKSPACE") || tt.HasText("bentomail") {
			t.Fatal("collapsed sidebar still shows labels")
		}
		avatar, ok := tt.Find("Active account")
		if !ok || avatar.W != 36 || avatar.X+avatar.W > 80 || avatar.Y < 340 {
			t.Fatalf("account trigger is not an avatar in the rail footer: %+v", avatar)
		}
		if _, ok := tt.Find("All accounts"); ok {
			t.Fatal("closed account switcher exposes account labels")
		}
		if err := tt.Click("Inbox"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click("Active account"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		popup, ok := tt.Find("Account choices")
		if !ok || popup.Y+popup.H > avatar.Y || popup.X < 0 || popup.X+popup.W > 1000 {
			t.Fatalf("footer popup is clipped or did not open upward: %+v", popup)
		}
		if _, ok := tt.Find("Account avatar for Alex Morgan"); !ok {
			t.Fatal("account option has no avatar")
		}
		if err := tt.Click("Alex Morgan <alex@example.com>"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Frame()
		if a.accountID != "work" {
			t.Fatal("account selection failed")
		}
		if err := tt.Click("Active account"); err != nil {
			t.Fatal(err)
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if err := tt.Click("Expand sidebar"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(300 * time.Millisecond)
		tt.Frame()
		tt.Frame()
		if a.prefs.SidebarCollapsed || a.prefs.SidebarWidth != 245 {
			t.Fatal("expanding lost sidebar width")
		}
		avatar, ok = tt.Find("Active account")
		if !ok || avatar.W != 36 || avatar.X+avatar.W > 245 || avatar.Y < 340 {
			t.Fatalf("account trigger left the expanded footer: %+v", avatar)
		}
		if tt.HasText("bentomail") {
			t.Fatal("expanded sidebar still shows branding")
		}
	}
}

func TestComposerScheduleWidth(t *testing.T) {
	a := seededApp(t)
	a.newComposer(nil, "new")
	tt := ui.NewTester(a.view, 1000, 680)
	r, ok := tt.Find("Scheduled send time")
	if !ok || r.W < 300 || r.X+r.W > 1000 {
		t.Fatalf("schedule field clipped: %+v", r)
	}
	if err := tt.Click("Scheduled send time"); err != nil {
		t.Fatal(err)
	}
	tt.Type("2026-12-01 09:30")
	tt.Frame()
	if a.activeComposer().schedule != "2026-12-01 09:30" {
		t.Fatal("schedule lost input")
	}
}

func TestUpdaterQuitSavesDraftBeforeClosing(t *testing.T) {
	a := seededApp(t)
	a.prefs.Background = true
	a.newComposer(nil, "new")
	a.activeComposer().doc.SetText(richtext.NewText("Draft saved before update restart"))
	a.prepareQuit(&mygo.QuitEvent{})
	if !a.allowClose {
		t.Fatal("updater restart would be canceled")
	}
	drafts := a.store.Snapshot().Drafts
	if len(drafts) != 1 || drafts[0].Text != "Draft saved before update restart" {
		t.Fatal("open draft was not saved")
	}
}
