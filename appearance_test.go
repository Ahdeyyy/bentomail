package main

import (
	"encoding/json"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo/ui"
)

func TestTranslucencyPreferenceMigration(t *testing.T) {
	for _, tc := range []struct {
		data string
		want float64
	}{{`{"Dark":true}`, 65}, {`{"Translucency":0}`, 0}, {`{"Translucency":100}`, 100}} {
		var prefs mailclient.Preferences
		if err := json.Unmarshal([]byte(tc.data), &prefs); err != nil {
			t.Fatal(err)
		}
		if prefs.Translucency != tc.want {
			t.Fatalf("%s: got %v, want %v", tc.data, prefs.Translucency, tc.want)
		}
	}
}

func TestFrostedSurfaceBlurAndButtonOpacity(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, level := range []float64{0, 65, 100} {
			theme := workspaceTheme(dark, "Indigo", level)
			if theme.Surface.A < 239 || theme.Accent.A != 255 || theme.SurfaceHover.A != 255 || theme.SurfacePressed.A != 255 {
				t.Fatalf("buttons are too translucent at %.0f%%: %+v", level, theme.Surface)
			}
		}
	}
	view := func(blur bool) func(*ui.Context) {
		return func(c *ui.Context) {
			c.Root().Background(ui.Hex("#000000"))
			ui.Box(c).Absolute().Left(80).Top(0).Width(80).Height(100).Background(ui.Hex("#ffffff"))
			e := ui.Box(c).Absolute().Left(10).Top(10).Width(140).Height(80).Radius(12)
			if blur {
				frost(e, ui.Hex("#ffffff").Alpha(.6))
			} else {
				e.Background(ui.Hex("#ffffff").Alpha(.6))
			}
		}
	}
	plain := ui.NewTester(view(false), 160, 100).Image()
	blurred := ui.NewTester(view(true), 160, 100).Image()
	if blurred.RGBAAt(78, 50).R <= plain.RGBAAt(78, 50).R || blurred.RGBAAt(82, 50).R >= plain.RGBAAt(82, 50).R {
		t.Fatal("translucent surface does not blur the backdrop")
	}
}

func TestTranslucencySurvivesStoreReopen(t *testing.T) {
	for _, level := range []float64{0, 65, 100} {
		dir := t.TempDir()
		store, err := mailclient.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		prefs := store.Snapshot().Preferences
		prefs.Translucency = level
		err = store.SavePreferences(prefs)
		store.Close()
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := mailclient.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		got := reopened.Snapshot().Preferences.Translucency
		reopened.Close()
		if got != level {
			t.Fatalf("reopened translucency = %v, want %v", got, level)
		}
	}
}

func TestTranslucencyControlPersistsAndRestores(t *testing.T) {
	a := testApp(t)
	a.page, a.settingsTab = "settings", "Appearance"
	tt := ui.NewTester(a.view, 1000, 680)
	tt.SetVibrancy(true)
	if err := tt.Click("Translucency level"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyHome)
	tt.Frame()
	if a.prefs.Translucency != 0 || a.theme.Background.A != 255 || surface(a.theme).A != 255 {
		t.Fatalf("opaque endpoint not applied: %v, %+v", a.prefs.Translucency, a.theme)
	}
	tt.Key(0, ui.KeyEnd)
	tt.Frame()
	if a.prefs.Translucency != 100 || a.theme.Background.A >= 100 || surface(a.theme).A >= 150 {
		t.Fatalf("frosted endpoint not applied: %v, %+v", a.prefs.Translucency, a.theme)
	}
	if got := newApp(a.store).prefs.Translucency; got != 100 {
		t.Fatalf("preference not restored: %v", got)
	}
	// The root must regain an opaque background when no OS material exists.
	tt.SetVibrancy(false)
	if _, _, _, alpha := tt.Image().At(2, 300).RGBA(); alpha != 65535 {
		t.Fatal("unsupported platform has a transparent window background")
	}
}

func TestConfirmationDialogActionsAndFocus(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1000, 680)
	called := 0
	open := func() {
		a.confirm("Remove account?", "Downloaded mail will be retained as a local archive. Credentials will be removed.", func() { called++ })
		tt.Frame()
		tt.Frame()
	}
	open()
	tt.Key(0, ui.KeyEnter) // Cancel is the safe default.
	tt.Frame()
	if called != 0 || a.confirmOpen {
		t.Fatal("default action did not cancel")
	}
	open()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if called != 0 || a.confirmOpen || a.confirmAction != nil {
		t.Fatal("escape did not discard confirmation")
	}
	open()
	if err := tt.Click("Continue"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if called != 1 || a.confirmOpen || a.confirmAction != nil {
		t.Fatal("confirmation must execute once and close")
	}
}

func TestRenderConfirmationDialog(t *testing.T) {
	dir := os.Getenv("BENTOMAIL_SCREENSHOTS")
	if dir == "" {
		t.Skip("set BENTOMAIL_SCREENSHOTS to export native UI renders")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := seededApp(t)
	tt := ui.NewTester(a.view, 1000, 680)
	for _, dark := range []bool{false, true} {
		a.prefs.Dark = dark
		a.savePrefs()
		a.confirm("Remove account?", "Downloaded mail will be retained as a local archive. Credentials will be removed.", func() {})
		tt.Frame()
		name := "confirmation-light.png"
		if dark {
			name = "confirmation-dark.png"
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSettingsControlWidths(t *testing.T) {
	for _, width := range []int{1000, 1440} {
		a := seededApp(t)
		a.page, a.settingsTab = "settings", "Storage"
		tt := ui.NewTester(a.view, width, 900)
		for _, label := range []string{"Back up mail", "Restore backup", "Import .eml files", "Rebuild search index"} {
			r, ok := tt.Find(label)
			if !ok || r.W > 260 || r.H > 40 {
				t.Fatalf("%s is stretched or wrapped at width %d: %+v", label, width, r)
			}
		}
		a.settingsTab = "Accounts"
		tt.Frame()
		for _, label := range []string{"Display name", "Email address", "IMAP server"} {
			r, ok := tt.Find(label)
			if !ok || r.W > 480 {
				t.Fatalf("%s is too wide: %+v", label, r)
			}
		}
	}
}

func TestAccentForegroundContrast(t *testing.T) {
	luminance := func(c ui.Color) float64 {
		linear := func(v uint8) float64 {
			s := float64(v) / 255
			if s <= .04045 {
				return s / 12.92
			}
			return math.Pow((s+.055)/1.055, 2.4)
		}
		return .2126*linear(c.R) + .7152*linear(c.G) + .0722*linear(c.B)
	}
	for _, dark := range []bool{false, true} {
		for _, accent := range []string{"Indigo", "Teal", "Rose", "Amber", "Violet"} {
			theme := workspaceTheme(dark, accent)
			a, b := luminance(theme.Accent), luminance(theme.AccentText)
			ratio := (max(a, b) + .05) / (min(a, b) + .05)
			if ratio < 4.5 {
				t.Errorf("%s dark=%v: accent text contrast %.2f", accent, dark, ratio)
			}
		}
	}
}
