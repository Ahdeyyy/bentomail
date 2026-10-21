//go:build windows

package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

func TestNativeMenuRoundedHighlight(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	fontData, err := interfaceFonts.ReadFile("resources/fonts/MartianGrotesk-Regular.ttf")
	if err != nil {
		t.Fatal(err)
	}
	var count uint32
	fontResource, _, _ := addFont.Call(uintptr(unsafe.Pointer(&fontData[0])), uintptr(len(fontData)), 0, uintptr(unsafe.Pointer(&count)))
	runtime.KeepAlive(fontData)
	if fontResource == 0 || count == 0 {
		t.Fatal("cannot load bundled menu font")
	}
	defer menuGDI.NewProc("RemoveFontMemResourceEx").Call(fontResource)
	const width, height = 280, 34 * 5
	dc, _, _ := menuGDI.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		t.Fatal("cannot create menu drawing context")
	}
	defer menuGDI.NewProc("DeleteDC").Call(dc)
	header := struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		ClrUsed, ClrImportant  uint32
	}{Size: 40, Width: width, Height: -height, Planes: 1, BitCount: 32}
	var bits uintptr
	bitmap, _, _ := menuGDI.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 {
		t.Fatal("cannot create menu bitmap")
	}
	defer deleteObject.Call(bitmap)
	old, _, _ := selectObject.Call(dc, bitmap)
	defer selectObject.Call(dc, old)
	for _, dark := range []bool{false, true} {
		theme := workspaceTheme(dark, "Indigo")
		backdrop := &menuBackdrop{width: width, height: height, screenWidth: width, screenHeight: height, pixels: make([]byte, width*height*4)}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				color := ui.Hex("#7295c5")
				if x > width/2 {
					color = ui.Hex("#d5a48b")
				}
				i := (y*width + x) * 4
				copy(backdrop.pixels[i:i+4], []byte{color.B, color.G, color.R, 255})
			}
		}
		blurMenuPixels(backdrop.pixels, width, height, 6)
		s := &windowMenuTheme{theme: func() *ui.Theme { return theme }, dpi: 96, backdrops: map[uintptr]*menuBackdrop{0: backdrop}}
		s.font, _, _ = createFont.Call(^uintptr(12), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(theme.Font))))
		pixels := unsafe.Slice(menuMessage[byte](bits), width*height*4)
		pixel := func(x, y int) ui.Color {
			i := (y*width + x) * 4
			return ui.Color{R: pixels[i+2], G: pixels[i+1], B: pixels[i], A: 255}
		}
		paintMenuBackdrop(dc, menuRect{0, 0, width, height}, menuSurface(theme), backdrop)
		menuGDI.NewProc("GdiFlush").Call()
		corner, selection, disabled := pixel(4, 2), pixel(200, 3), pixel(200, 3+34*3)
		items := []struct {
			label   string
			state   uint32
			submenu bool
		}{
			{"Reply\tCtrl+R", 1, false},
			{"Mark as unread", 8, false},
			{"Move to folder", 0, true},
			{"Unavailable", 3, false},
			{"Delete local copy", 0, false},
		}
		for i, item := range items {
			s.draw(&menuDraw{DC: dc, State: item.state, Rect: menuRect{0, int32(i * 34), width, int32((i + 1) * 34)}}, themedMenuItem{label: item.label, submenu: item.submenu})
		}
		menuGDI.NewProc("GdiFlush").Call()
		bg := menuSurface(theme)
		bg.A = 255
		if pixel(4, 2) != corner || pixel(200, 3) == selection || pixel(200, 3+34*3) != disabled {
			t.Fatal("menu highlight is square, absent, or applied to a disabled item")
		}
		if pixel(4, 2) == bg {
			t.Fatal("menu lost its translucent backdrop")
		}
		if dir := os.Getenv("BENTOMAIL_SCREENSHOTS"); dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			img := image.NewRGBA(image.Rect(0, 0, width, height))
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					c := pixel(x, y)
					i := y*img.Stride + x*4
					copy(img.Pix[i:i+4], []byte{c.R, c.G, c.B, 255})
				}
			}
			name := "menu-light.png"
			if dark {
				name = "menu-dark.png"
			}
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, img)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		deleteObject.Call(s.font)
		// At 0% translucency even a captured backdrop must remain invisible.
		theme = workspaceTheme(dark, "Indigo", 0)
		paintMenuBackdrop(dc, menuRect{0, 0, width, height}, menuSurface(theme), backdrop)
		menuGDI.NewProc("GdiFlush").Call()
		if pixel(4, 2) != menuSurface(theme) {
			t.Fatal("opaque menu setting still shows the backdrop")
		}
	}
}

func TestNativeMenuBackdropBlur(t *testing.T) {
	const width, height = 40, 5
	pixels := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := width / 2; x < width; x++ {
			for channel := 0; channel < 3; channel++ {
				pixels[(y*width+x)*4+channel] = 255
			}
		}
	}
	blurMenuPixels(pixels, width, height, 2)
	left, right := pixels[(width/2-1)*4], pixels[(width/2)*4]
	if left == 0 || right == 255 || left >= right || pixels[0] != 0 || pixels[(width-1)*4] != 255 {
		t.Fatalf("backdrop blur does not soften edges: %d / %d", left, right)
	}
}

func TestNativeMenuBackdropCapture(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b := captureMenuBackdrop(2)
	if b == nil {
		t.Skip("desktop capture unavailable; menus fall back to solid surfaces")
	}
	if b.width < 1 || b.height < 1 || len(b.pixels) != b.width*b.height*4 {
		t.Fatal("invalid native backdrop capture")
	}
}

func TestNativeMenuThemePreservesCommands(t *testing.T) {
	menu, _, err := menuUser.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		t.Fatal(err)
	}
	defer menuUser.NewProc("DestroyMenu").Call(menu)
	appendItem := menuUser.NewProc("AppendMenuW")
	appendItem.Call(menu, 8, 101, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("&Reply\tCtrl+R"))))
	appendItem.Call(menu, 0x800, 0, 0)
	appendItem.Call(menu, 1, 102, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("&Unavailable"))))
	theme := workspaceTheme(true, "Indigo")
	s := &windowMenuTheme{theme: func() *ui.Theme { return theme }, items: map[uintptr]themedMenuItem{}}
	defer func() { deleteObject.Call(s.font); deleteObject.Call(s.background) }()
	s.prepare(0, menu)
	for i, want := range []string{"&Reply\tCtrl+R", "", "&Unavailable"} {
		it := menuItemInfo{Mask: 0x100 | 0x20 | 0x1 | 0x2}
		it.Size = uint32(unsafe.Sizeof(it))
		if ok, _, _ := getMenuItemInfo.Call(menu, uintptr(i), 1, uintptr(unsafe.Pointer(&it))); ok == 0 {
			t.Fatal("cannot inspect themed item")
		}
		item, ok := s.items[it.Data]
		if !ok || item.label != want || it.Type&0x100 == 0 {
			t.Fatalf("item lost text or owner drawing: %+v", item)
		}
		if i == 0 && (it.ID != 101 || it.State&8 == 0) {
			t.Fatal("reply lost its ID or checkmark")
		}
		if i == 2 && (it.ID != 102 || it.State&3 == 0) {
			t.Fatal("disabled command was enabled or changed ID")
		}
		m := menuMeasure{}
		s.measure(0, &m, item)
		if m.Width == 0 || m.Height == 0 {
			t.Fatal("menu item has no size")
		}
	}
	if got := s.mnemonic(menu, 'r'); got != 2<<16 {
		t.Fatalf("reply mnemonic: %#x", got)
	}
	if got := s.mnemonic(menu, 'u'); got != 0 {
		t.Fatal("disabled mnemonic was activated")
	}
	// Reopening a popup and changing appearance must preserve IDs and labels.
	theme = workspaceTheme(false, "Teal")
	s.prepare(0, menu)
	if len(s.items) != 3 || s.color != menuSurface(theme) {
		t.Fatal("reopening duplicated items or retained stale colors")
	}
}
