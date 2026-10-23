package main

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func motionBounds(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("missing %q", label)
	}
	return r
}

func TestSidebarMotionAndReversal(t *testing.T) {
	a := seededApp(t)
	a.prefs.SidebarWidth = 245
	tt := ui.NewTester(a.view, 1440, 900)
	start := motionBounds(t, tt, "Sidebar navigation")
	search := motionBounds(t, tt, "Search mail")
	if err := tt.Click("Collapse sidebar"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(80 * time.Millisecond)
	middle := motionBounds(t, tt, "Sidebar navigation")
	searchMiddle := motionBounds(t, tt, "Search mail")
	if middle.W <= 80 || middle.W >= start.W || searchMiddle.X >= search.X {
		t.Fatalf("sidebar and titlebar did not move together: sidebar=%+v => %+v search=%+v => %+v", start, middle, search, searchMiddle)
	}
	if math.Abs(float64((searchMiddle.X-search.X)-(middle.W-start.W))) > 1 {
		t.Fatalf("sidebar and titlebar are out of step: sidebar=%g, search=%g", middle.W-start.W, searchMiddle.X-search.X)
	}
	saveLayoutMenuImage(t, tt, "sidebar-collapsing")
	// Reversing in flight should start from the current width.
	if err := tt.Click("Expand sidebar"); err != nil {
		t.Fatal(err)
	}
	reversed := motionBounds(t, tt, "Sidebar navigation")
	if reversed.W <= 80 || reversed.W > middle.W {
		t.Fatalf("reversing jumped from %g to %g", middle.W, reversed.W)
	}
	tt.Advance(300 * time.Millisecond)
	if end := motionBounds(t, tt, "Sidebar navigation"); end.W != 245 || a.prefs.SidebarWidth != 245 {
		t.Fatalf("expanding lost the saved width: %+v, preference=%g", end, a.prefs.SidebarWidth)
	}
	if err := tt.Click("Collapse sidebar"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if end := motionBounds(t, tt, "Sidebar navigation"); end.W != 80 {
		t.Fatalf("collapse did not finish at rail width: %+v", end)
	}
	saveLayoutMenuImage(t, tt, "sidebar-collapsed")
	if a.prefs.SidebarWidth != 245 || !a.store.Snapshot().Preferences.SidebarCollapsed {
		t.Fatal("animation changed the saved sidebar preferences")
	}
}

func TestHiddenReaderAvatarMorphsBothWays(t *testing.T) {
	a := seededApp(t)
	a.prefs.Layout = "Hidden"
	tt := ui.NewTester(a.view, 1000, 680)
	// A lower row makes the motion visible and shares a sender with row zero.
	m := a.messages()[3]
	label := "Sender avatar " + m.ID
	for i := 0; i < 2; i++ {
		from := motionBounds(t, tt, label)
		saveLayoutMenuImage(t, tt, "avatar-list")
		if err := tt.Click(m.Subject); err != nil {
			t.Fatal(err)
		}
		start := motionBounds(t, tt, label)
		if math.Abs(float64(start.W-32)) > .01 || math.Abs(float64(start.Y-from.Y)) > 1 {
			t.Fatalf("reader avatar did not start at its list position: %+v => %+v", from, start)
		}
		tt.Advance(120 * time.Millisecond)
		middle := motionBounds(t, tt, label)
		if middle.W <= 32 || middle.W >= 38 || middle.Y == start.Y {
			t.Fatalf("avatar did not move and grow between views: %+v => %+v", start, middle)
		}
		saveLayoutMenuImage(t, tt, "avatar-opening")
		tt.Advance(300 * time.Millisecond)
		end := motionBounds(t, tt, label)
		if end.W != 38 || !tt.HasText(m.Text) {
			t.Fatalf("reader did not settle: %+v", end)
		}
		saveLayoutMenuImage(t, tt, "avatar-reader")
		if err := tt.Click("Back to messages"); err != nil {
			t.Fatal(err)
		}
		back := motionBounds(t, tt, label)
		if math.Abs(float64(back.W-38)) > .01 || math.Abs(float64(back.Y-end.Y)) > 1 {
			t.Fatalf("returning avatar did not start in the reader: %+v => %+v", end, back)
		}
		tt.Advance(120 * time.Millisecond)
		saveLayoutMenuImage(t, tt, "avatar-returning")
		tt.Advance(300 * time.Millisecond)
		back = motionBounds(t, tt, label)
		if back.W != 32 || !tt.Focused("Message list") {
			t.Fatalf("Back did not settle and restore list focus: %+v", back)
		}
	}
}

func TestWorkspaceMotionRespectsReducedMotion(t *testing.T) {
	a := seededApp(t)
	a.prefs.Layout = "Hidden"
	a.prefs.AnimationSpeed = 0.25
	tt := ui.NewTester(a.view, 1000, 680)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if err := tt.Click("Collapse sidebar"); err != nil {
		t.Fatal(err)
	}
	if r := motionBounds(t, tt, "Sidebar navigation"); r.W != 80 {
		t.Fatalf("sidebar animated with reduced motion: %+v", r)
	}
	m := a.messages()[0]
	if err := tt.Click(m.Subject); err != nil {
		t.Fatal(err)
	}
	if r := motionBounds(t, tt, "Sender avatar "+m.ID); r.W != 38 {
		t.Fatalf("avatar animated with reduced motion: %+v", r)
	}
}

func TestAnimationSpeedChangesSidebarAndAvatarTiming(t *testing.T) {
	for _, speed := range []float64{0.25, 2} {
		t.Run(fmt.Sprintf("speed=%g", speed), func(t *testing.T) {
			a := seededApp(t)
			a.prefs.Layout = "Hidden"
			a.prefs.AnimationSpeed = speed
			tt := ui.NewTester(a.view, 1000, 680)
			if err := tt.Click("Collapse sidebar"); err != nil {
				t.Fatal(err)
			}
			tt.Advance(200 * time.Millisecond)
			sidebar := motionBounds(t, tt, "Sidebar navigation")
			if speed == 2 && sidebar.W != 80 || speed == 0.25 && sidebar.W <= 80 {
				t.Fatalf("sidebar ignored speed %g: %+v", speed, sidebar)
			}
			tt.Advance(time.Second)
			m := a.messages()[3]
			if err := tt.Click(m.Subject); err != nil {
				t.Fatal(err)
			}
			tt.Advance(200 * time.Millisecond)
			avatar := motionBounds(t, tt, "Sender avatar "+m.ID)
			if speed == 2 && math.Abs(float64(avatar.W-38)) > .01 || speed == 0.25 && (avatar.W <= 32 || avatar.W >= 38) {
				t.Fatalf("avatar ignored speed %g: %+v", speed, avatar)
			}
		})
	}
}

func TestReturnedAvatarStaysWithRowWhileScrolling(t *testing.T) {
	for _, speed := range []float64{0.25, 1, 2} {
		t.Run(fmt.Sprintf("speed=%g", speed), func(t *testing.T) {
			a := seededApp(t)
			a.prefs.Layout, a.prefs.AnimationSpeed = "Hidden", speed
			tt := ui.NewTester(a.view, 1000, 680)
			m := a.messages()[3]
			if err := tt.Click(m.Subject); err != nil {
				t.Fatal(err)
			}
			tt.Advance(a.animationDuration(mailScreenDuration))
			if err := tt.Click("Back to messages"); err != nil {
				t.Fatal(err)
			}
			tt.Advance(a.animationDuration(mailScreenDuration))
			avatarLabel := "Sender avatar " + m.ID
			avatar := motionBounds(t, tt, avatarLabel)
			subject := motionBounds(t, tt, m.Subject)
			offset := avatar.Y - subject.Y
			viewport := motionBounds(t, tt, "Message list")
			for _, delta := range []float32{80, 80, -80, -80} {
				tt.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, delta)
				saveLayoutMenuImage(t, tt, "avatar-scrolling")
				for _, elapsed := range []time.Duration{0, 16 * time.Millisecond, 100 * time.Millisecond, time.Second} {
					tt.Advance(elapsed)
					avatar = motionBounds(t, tt, avatarLabel)
					subject = motionBounds(t, tt, m.Subject)
					if math.Abs(float64(avatar.Y-subject.Y-offset)) > 1 {
						t.Fatalf("avatar detached from its row after scroll %g at %v: avatar=%+v subject=%+v original offset=%g", delta, elapsed, avatar, subject, offset)
					}
				}
			}
			saveLayoutMenuImage(t, tt, "avatar-after-scrolling")
		})
	}
}
