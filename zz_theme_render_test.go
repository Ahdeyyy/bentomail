package main

// Scratch renderer for the theming cleanup: renders with the window material
// on and composites the frames over a simulated acrylic desktop. Delete once
// the review is done.

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func acrylicDesktop(w, h int, dark bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	type blob struct {
		x, y, r float64
		c       [3]float64
	}
	blobs := []blob{{.15, .2, .45, [3]float64{255, 140, 80}}, {.8, .3, .5, [3]float64{60, 120, 255}}, {.5, .9, .5, [3]float64{40, 200, 150}}, {.95, .95, .3, [3]float64{230, 60, 160}}}
	tint, amount := [3]float64{243, 243, 243}, .55
	if dark {
		tint, amount = [3]float64{32, 32, 32}, .7
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float64(x)/float64(w), float64(y)/float64(h)
			c := [3]float64{120, 120, 130}
			for _, b := range blobs {
				d := math.Hypot(fx-b.x, fy-b.y) / b.r
				k := math.Exp(-d * d * 2)
				for i := range c {
					c[i] = c[i]*(1-k) + b.c[i]*k
				}
			}
			for i := range c {
				c[i] = c[i]*(1-amount) + tint[i]*amount
			}
			img.Set(x, y, color.RGBA{uint8(c[0]), uint8(c[1]), uint8(c[2]), 255})
		}
	}
	return img
}

func compositeOver(dst, src *image.RGBA) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	for i := 0; i < len(src.Pix); i += 4 {
		a := int(src.Pix[i+3])
		for c := 0; c < 3; c++ {
			// MyGo frames are premultiplied.
			out.Pix[i+c] = uint8(min(255, int(src.Pix[i+c])+int(dst.Pix[i+c])*(255-a)/255))
		}
		out.Pix[i+3] = 255
	}
	return out
}

func TestZZRenderThemes(t *testing.T) {
	dir := os.Getenv("BENTOMAIL_THEME_RENDERS")
	if dir == "" {
		t.Skip("set BENTOMAIL_THEME_RENDERS")
	}
	os.MkdirAll(dir, 0700)
	type dataset struct {
		name string
		make func(*testing.T) *app
	}
	full := os.Getenv("BENTOMAIL_FULL") != ""
	for _, ds := range []dataset{{"demo", seededApp}, {"worst", worstCaseApp}, {"empty", emptyApp}, {"one", oneMessageApp}} {
		for _, dark := range []bool{false, true} {
			a := ds.make(t)
			if len(a.state.Messages) > 0 {
				a.selectedID = a.state.Messages[0].ID
				for _, m := range a.state.Messages {
					if m.ID == "w1" {
						a.selectedID = m.ID
					}
				}
			}
			a.prefs.Dark = dark
			a.theme = workspaceTheme(dark, "Indigo", a.prefs.Translucency)
			w, h := 1440, 900
			tt := ui.NewTester(a.view, w, h)
			tt.SetVibrancy(true)
			mode := "light"
			if dark {
				mode = "dark"
			}
			shot := func(name string) {
				tt.Frame()
				tt.Frame()
				tt.Advance(400 * time.Millisecond)
				img := tt.Image()
				desk := acrylicDesktop(img.Bounds().Dx(), img.Bounds().Dy(), dark)
				f, err := os.Create(filepath.Join(dir, ds.name+"-"+mode+"-"+name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				png.Encode(f, compositeOver(desk, img))
				f.Close()
			}
			shot("workspace")
			tt.SetSize(1000, 680)
			shot("narrow")
			tt.SetSize(w, h)
			if !full || ds.name != "demo" && ds.name != "worst" {
				continue
			}
			if len(a.state.Messages) > 1 {
				if err := tt.RightClick(a.state.Messages[1].Subject); err == nil {
					shot("menu")
					tt.Key(0, ui.KeyEscape)
				}
			}
			a.newComposer(nil, "new")
			shot("composer")
			a.page, a.settingsTab = "settings", "Appearance"
			shot("settings")
			if err := tt.Click("Indigo"); err == nil {
				shot("select")
				tt.Key(0, ui.KeyEscape)
			}
			a.page = "mail"
			a.prefs.SidebarCollapsed = true
			shot("collapsed")
		}
	}
}

func TestZZRenderHiddenScrolled(t *testing.T) {
	dir := os.Getenv("BENTOMAIL_THEME_RENDERS")
	if dir == "" {
		t.Skip("set BENTOMAIL_THEME_RENDERS")
	}
	a := worstCaseApp(t)
	a.prefs.Layout = "Hidden"
	tt := ui.NewTester(a.view, 1000, 680)
	tt.SetVibrancy(true)
	tt.Frame()
	a.selectMessage("w1")
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	msg, _ := tt.Find("Message")
	tt.Scroll(msg.X+msg.W/2, msg.Y+40, 0, 120)
	tt.Move(5, 5) // Keep the pointer off tooltips.
	tt.Frame()
	tt.Advance(time.Second)
	f, err := os.Create(filepath.Join(dir, "worst-light-hidden-scrolled.png"))
	if err != nil {
		t.Fatal(err)
	}
	img := tt.Image()
	png.Encode(f, compositeOver(acrylicDesktop(img.Bounds().Dx(), img.Bounds().Dy(), false), img))
	f.Close()
}
