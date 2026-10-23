package main

import (
	"encoding/json"
	"testing"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo/ui"
)

func TestAnimationSpeedPreferenceMigration(t *testing.T) {
	for _, tc := range []struct {
		data string
		want float64
	}{
		{`{"Dark":true}`, 1},
		{`{"AnimationSpeed":0}`, 1},
		{`{"AnimationSpeed":0.25}`, 0.25},
		{`{"AnimationSpeed":2}`, 2},
		{`{"AnimationSpeed":-10}`, 0.25},
		{`{"AnimationSpeed":100}`, 2},
	} {
		var prefs mailclient.Preferences
		if err := json.Unmarshal([]byte(tc.data), &prefs); err != nil {
			t.Fatal(err)
		}
		if prefs.AnimationSpeed != tc.want {
			t.Fatalf("%s: speed=%g, want %g", tc.data, prefs.AnimationSpeed, tc.want)
		}
	}
}

func TestAnimationSpeedSliderPersists(t *testing.T) {
	dir := t.TempDir()
	s, err := mailclient.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	a := newApp(s)
	a.page, a.settingsTab = "settings", "Appearance"
	tt := ui.NewTester(a.view, 1000, 680)
	if a.prefs.AnimationSpeed != 1 {
		t.Fatal("new preferences do not use normal animation speed")
	}
	if err := tt.Click("Animation speed slider"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyHome)
	if a.prefs.AnimationSpeed != 0.25 || s.Snapshot().Preferences.AnimationSpeed != 0.25 {
		t.Fatal("slow endpoint was not applied and saved")
	}
	tt.Key(0, ui.KeyEnd)
	if a.prefs.AnimationSpeed != 2 || !tt.HasText("2.00×") {
		t.Fatal("fast endpoint or current speed readout did not update")
	}
	tt.Key(0, ui.KeyLeft)
	if a.prefs.AnimationSpeed != 1.75 {
		t.Fatal("slider does not adjust in quarter-speed steps")
	}
	saveLayoutMenuImage(t, tt, "animation-speed-settings")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := mailclient.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := newApp(reopened).prefs.AnimationSpeed; got != 1.75 {
		t.Fatalf("saved speed did not survive reopening: %g", got)
	}
	// Verify reset using an app restored from the reopened store.
	a = newApp(reopened)
	a.page, a.settingsTab = "settings", "Appearance"
	tt = ui.NewTester(a.view, 1000, 680)
	if err := tt.Click("Normal speed"); err != nil {
		t.Fatal(err)
	}
	if a.prefs.AnimationSpeed != 1 || reopened.Snapshot().Preferences.AnimationSpeed != 1 {
		t.Fatal("normal speed reset was not applied and saved")
	}
}
