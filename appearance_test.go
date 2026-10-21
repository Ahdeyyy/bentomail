package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

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
