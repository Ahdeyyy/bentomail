package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestClickMailRowOpensLocalBody(t *testing.T) {
	a := seededApp(t)
	tt := ui.NewTester(a.view, 1440, 900)
	m := a.messages()[0]
	if err := tt.Click(m.Subject); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.selectedID != m.ID {
		t.Fatalf("click did not open mail: selected=%q row=%d", a.selectedID, a.rowIndex)
	}
	if !tt.HasText(m.Text) {
		t.Fatal("selected message body is not shown")
	}
}

func TestMailSelectionAndDragHandle(t *testing.T) {
	for _, density := range []string{"Comfortable", "Compact"} {
		t.Run(density, func(t *testing.T) {
			a := seededApp(t)
			a.prefs.Density = density
			tt := ui.NewTester(a.view, 1440, 900)
			ms := a.messages()
			if err := tt.Click(ms[0].Subject); err != nil {
				t.Fatal(err)
			}
			tt.Key(0, ui.KeyDown)
			if a.selectedID != ms[1].ID {
				t.Fatal("keyboard did not open the next message")
			}
			if err := tt.ClickWith(ui.Ctrl, ms[0].Subject); err != nil {
				t.Fatal(err)
			}
			if !a.selection.Has(ms[0].ID) || !a.selection.Has(ms[1].ID) {
				t.Fatal("Ctrl-click lost the multiple selection")
			}
			if err := tt.ClickWith(ui.Shift, ms[2].Subject); err != nil {
				t.Fatal(err)
			}
			if a.selection.Len() != 3 {
				t.Fatal("Shift-click did not select the range")
			}
			src, ok := tt.Find("Drag message " + ms[0].Subject)
			if !ok {
				t.Fatal("missing drag handle")
			}
			dest, ok := tt.Find("Clients")
			if !ok {
				t.Fatal("missing tag target")
			}
			x, y := src.X+src.W/2, src.Y+src.H/2
			tt.Press(x, y)
			tt.Move(x+20, y+10)
			tt.Move(dest.X+dest.W/2, dest.Y+dest.H/2)
			tt.Release(dest.X+dest.W/2, dest.Y+dest.H/2)
			m, _ := a.findMessage(ms[0].ID)
			if !strings.Contains(strings.Join(m.Tags, ","), "Clients") {
				t.Fatal("dragging the handle did not apply the tag")
			}
		})
	}
}

func TestOpeningMailDoesNotWaitForServer(t *testing.T) {
	a := seededApp(t)
	ms := a.messages()
	for i := 0; i < 2; i++ {
		m := ms[i]
		m.HTML = fmt.Sprintf("<p>Cached body %d</p>", i)
		m.UID = uint32(i + 1)
		if err := a.store.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	a.state = a.store.Snapshot()
	// Leave network work pending, as when the provider is unreachable.
	a.networkJobs = make(chan work, 8)
	var loaded string
	a.loadHTML = func(s string) { loaded = s }
	tt := ui.NewTester(a.view, 1000, 680)
	for i := 0; i < 2; i++ {
		if err := tt.Click(ms[i].Subject); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.selectedID != ms[i].ID || !strings.Contains(loaded, fmt.Sprintf("Cached body %d", i)) {
			t.Fatal("the cached message was not loaded immediately")
		}
	}
	if len(a.networkJobs) != 2 {
		t.Fatal("read-state changes should remain queued independently of reading")
	}
}

func TestAppearanceReadingControls(t *testing.T) {
	a := seededApp(t)
	a.page, a.settingsTab = "settings", "Appearance"
	tt := ui.NewTester(a.view, 1440, 900)
	beforeSize, beforeLines := a.prefs.ReadingSize, a.prefs.PreviewLines
	for _, label := range []string{"Smaller", "Larger", "Fewer", "More"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if label == "Smaller" && a.prefs.ReadingSize != beforeSize-1 {
			t.Fatal("font size was not decreased")
		}
		if label == "Fewer" && a.prefs.PreviewLines != beforeLines-1 {
			t.Fatal("preview lines were not decreased")
		}
	}
	if a.prefs.ReadingSize != beforeSize || a.prefs.PreviewLines != beforeLines {
		t.Fatal("reading controls did not restore the original settings")
	}
	if got := a.store.Snapshot().Preferences; got.ReadingSize != beforeSize || got.PreviewLines != beforeLines {
		t.Fatal("reading settings did not persist")
	}
}

func TestReadingPaneMenu(t *testing.T) {
	a := seededApp(t)
	tt := ui.NewTester(a.view, 1440, 900)
	if err := tt.Click("View"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Bottom reading pane"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prefs.Layout != "Bottom" {
		t.Fatal("reading pane menu did not change the layout")
	}
	if err := tt.Click(a.messages()[0].Subject); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.selectedID == "" {
		t.Fatal("bottom reading pane did not open the message")
	}
}
