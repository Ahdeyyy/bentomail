//go:build windows

package main

// MyGo 0.5 uses OS popup menus. Owner drawing keeps their native input,
// accessibility, submenus and command dispatch, but uses our typography and
// palette, including the automatic editing menus of text fields.
import (
	"log"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unsafe"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const menuMaxHeight = 400 // DIPs; longer menus retain native scrolling.

var (
	menuUser             = syscall.NewLazyDLL("user32.dll")
	menuGDI              = syscall.NewLazyDLL("gdi32.dll")
	menuControls         = syscall.NewLazyDLL("comctl32.dll")
	setWindowSubclass    = menuControls.NewProc("SetWindowSubclass")
	removeWindowSubclass = menuControls.NewProc("RemoveWindowSubclass")
	defSubclassProc      = menuControls.NewProc("DefSubclassProc")
	getMenuItemInfo      = menuUser.NewProc("GetMenuItemInfoW")
	setMenuItemInfo      = menuUser.NewProc("SetMenuItemInfoW")
	getMenuItemCount     = menuUser.NewProc("GetMenuItemCount")
	setMenuInfo          = menuUser.NewProc("SetMenuInfo")
	getWindowDPI         = menuUser.NewProc("GetDpiForWindow")
	getDC                = menuUser.NewProc("GetDC")
	releaseDC            = menuUser.NewProc("ReleaseDC")
	fillRect             = menuUser.NewProc("FillRect")
	createRoundRect      = menuGDI.NewProc("CreateRoundRectRgn")
	dwmSetAttribute      = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	drawText             = menuUser.NewProc("DrawTextW")
	createBrush          = menuGDI.NewProc("CreateSolidBrush")
	deleteObject         = menuGDI.NewProc("DeleteObject")
	selectObject         = menuGDI.NewProc("SelectObject")
	createFont           = menuGDI.NewProc("CreateFontW")
	setTextColor         = menuGDI.NewProc("SetTextColor")
	setBGMode            = menuGDI.NewProc("SetBkMode")
	addFont              = menuGDI.NewProc("AddFontMemResourceEx")
	menuFontOnce         sync.Once
	windowMenus          = map[uintptr]*windowMenuTheme{} // UI thread only
	menuSubclass         uintptr
	nextMenuToken        uintptr
)

func init() { menuSubclass = syscall.NewCallback(menuWindowProc) }

type menuRect struct{ Left, Top, Right, Bottom int32 }
type menuItemInfo struct {
	Size, Mask, Type, State, ID       uint32
	Submenu, Checked, Unchecked, Data uintptr
	Text                              *uint16
	TextLen                           uint32
	Bitmap                            uintptr
}
type menuInfo struct {
	Size, Mask, Style, MaxHeight uint32
	Background                   uintptr
	ContextHelp                  uint32
	Data                         uintptr
}
type menuMeasure struct {
	ControlType, ControlID, ID, Width, Height uint32
	Data                                      uintptr
}
type menuDraw struct {
	ControlType, ControlID, ID, Action, State uint32
	Window, DC                                uintptr
	Rect                                      menuRect
	Data                                      uintptr
}
type themedMenuItem struct {
	label   string
	kind    uint32
	submenu bool
}
type windowMenuTheme struct {
	theme            func() *ui.Theme
	items            map[uintptr]themedMenuItem
	font, background uintptr
	dpi              int32
	color            ui.Color
	backdrops        map[uintptr]*menuBackdrop
}

func installWindowTheme(w *mygo.Window, theme func() *ui.Theme) {
	if err := suppressSystemCaption(w.NativeHandle()); err != nil {
		log.Printf("configure hidden title bar: %v", err)
	}
	menuFontOnce.Do(func() {
		data, err := interfaceFonts.ReadFile("resources/fonts/MartianGrotesk-Regular.ttf")
		if err == nil && len(data) > 0 {
			var count uint32
			addFont.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 0, uintptr(unsafe.Pointer(&count)))
			runtime.KeepAlive(data) // GDI copies the private font resource.
		}
	})
	installMenuOwner(w.NativeHandle(), theme)
	// The tray's popup owner is MyGo's application message window. It is on
	// this thread too, but isn't a user-facing Window.
	threadID, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId").Call()
	var after uintptr
	for {
		appWindow, _, _ := menuUser.NewProc("FindWindowExW").Call(0, after, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("MyGoApp"))), 0)
		if appWindow == 0 {
			break
		}
		ownerThread, _, _ := menuUser.NewProc("GetWindowThreadProcessId").Call(appWindow, 0)
		if ownerThread == threadID {
			installMenuOwner(appWindow, theme)
			break
		}
		after = appWindow
	}
}

func installMenuOwner(hwnd uintptr, theme func() *ui.Theme) {
	if hwnd == 0 || windowMenus[hwnd] != nil {
		return
	}
	s := &windowMenuTheme{theme: theme, items: map[uintptr]themedMenuItem{}}
	windowMenus[hwnd] = s
	if ok, _, err := setWindowSubclass.Call(hwnd, menuSubclass, 1, 0); ok == 0 {
		delete(windowMenus, hwnd)
		log.Printf("install menu theme: %v", err)
	}
}

// LPARAM carries either integers or pointers. Keep the callback argument an
// integer so the GC never treats integer messages (e.g. a hit-test value) as
// pointers. Decode it only for messages whose ABI supplies a native structure.
func menuMessage[T any](value uintptr) *T { return *(**T)(unsafe.Pointer(&value)) }

func menuWindowProc(hwnd, msg, wp, lp, id, ref uintptr) uintptr {
	s := windowMenus[hwnd]
	if s != nil {
		switch msg {
		case 0x0117: // WM_INITMENUPOPUP: let the framework update state first.
			r, _, _ := defSubclassProc.Call(hwnd, msg, wp, lp)
			if lp>>16 == 0 {
				s.prepare(hwnd, wp)
				if s.theme().Background.A < 255 {
					if s.backdrops == nil {
						s.backdrops = map[uintptr]*menuBackdrop{}
					}
					if _, captured := s.backdrops[wp]; !captured {
						s.backdrops[wp] = captureMenuBackdrop(max(1, int(s.px(backdropBlurRadius))/4))
					}
				}
			} // Leave the OS system menu alone.
			return r
		case 0x002c: // WM_MEASUREITEM
			m := menuMessage[menuMeasure](lp)
			if m.ControlType == 1 {
				if item, ok := s.items[m.Data]; ok {
					s.measure(hwnd, m, item)
					return 1
				}
			}
		case 0x002b: // WM_DRAWITEM
			d := menuMessage[menuDraw](lp)
			if d.ControlType == 1 {
				if item, ok := s.items[d.Data]; ok {
					s.draw(d, item)
					return 1
				}
			}
		case 0x0120: // WM_MENUCHAR: owner-drawn items need explicit mnemonics.
			if r := s.mnemonic(lp, rune(wp&0xffff)); r != 0 {
				return r
			}
		case 0x0121: // WM_ENTERIDLE: the popup HWND now exists, including submenus.
			if wp == 2 && lp != 0 { // MSGF_MENU
				s.stylePopup(lp)
			}
		case 0x0212: // WM_EXITMENULOOP; native popup handles are ephemeral.
			clear(s.items)
			clear(s.backdrops)
		case 0x0082: // WM_NCDESTROY
			removeWindowSubclass.Call(hwnd, menuSubclass, id)
			deleteObject.Call(s.font)
			deleteObject.Call(s.background)
			delete(windowMenus, hwnd)
		}
	}
	r, _, _ := defSubclassProc.Call(hwnd, msg, wp, lp)
	return r
}

func colorRef(c ui.Color) uintptr { return uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16 }
func menuSurface(t *ui.Theme) ui.Color {
	return overlaySurface(t)
}

func (s *windowMenuTheme) stylePopup(hwnd uintptr) {
	var name [32]uint16
	menuUser.NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	if syscall.UTF16ToString(name[:]) != "#32768" {
		return
	}
	// Preserve the native shadow and hit testing; DWM applies its supported
	// rounded popup policy on Windows 11 and safely declines on older systems.
	corner := uint32(2) // DWMWCP_ROUND
	dwmSetAttribute.Call(hwnd, 33, uintptr(unsafe.Pointer(&corner)), 4)
	dark := uint32(0)
	if s.theme().Dark {
		dark = 1
	}
	dwmSetAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), 4)
	border := uint32(colorRef(s.theme().Border))
	dwmSetAttribute.Call(hwnd, 34, uintptr(unsafe.Pointer(&border)), 4)
}

func (s *windowMenuTheme) px(n int32) int32 { return (n*s.dpi + 48) / 96 }
func (s *windowMenuTheme) prepare(hwnd, menu uintptr) {
	dpi, _, _ := getWindowDPI.Call(hwnd)
	if dpi == 0 {
		dpi = 96
	}
	t := s.theme()
	if s.font == 0 || s.dpi != int32(dpi) {
		deleteObject.Call(s.font)
		s.dpi = int32(dpi)
		height := -s.px(int32(t.FontSize))
		s.font, _, _ = createFont.Call(uintptr(height), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0,
			uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(t.Font))))
	}
	bg := menuSurface(t)
	if s.background == 0 || s.color != bg {
		deleteObject.Call(s.background)
		s.color = bg
		s.background, _, _ = createBrush.Call(colorRef(bg))
	}
	// MIM_MAXHEIGHT enables native scrolling instead of filling the screen.
	mi := menuInfo{Mask: 1 | 2, MaxHeight: uint32(s.px(menuMaxHeight)), Background: s.background}
	mi.Size = uint32(unsafe.Sizeof(mi))
	setMenuInfo.Call(menu, uintptr(unsafe.Pointer(&mi)))
	n, _, _ := getMenuItemCount.Call(menu)
	for i := uintptr(0); i < n; i++ {
		var text [2048]uint16
		it := menuItemInfo{Mask: 0x100 | 0x40 | 0x4 | 0x20, Text: &text[0], TextLen: uint32(len(text))} // FTYPE, STRING, SUBMENU, DATA
		it.Size = uint32(unsafe.Sizeof(it))
		if ok, _, _ := getMenuItemInfo.Call(menu, i, 1, uintptr(unsafe.Pointer(&it))); ok == 0 {
			continue
		}
		if _, ok := s.items[it.Data]; ok {
			continue
		}
		nextMenuToken++
		s.items[nextMenuToken] = themedMenuItem{syscall.UTF16ToString(text[:]), it.Type, it.Submenu != 0}
		it.Mask, it.Type, it.Data = 0x100|0x20, it.Type|0x100, nextMenuToken // MFT_OWNERDRAW; retain the string for accessibility.
		setMenuItemInfo.Call(menu, i, 1, uintptr(unsafe.Pointer(&it)))
	}
}

func menuText(dc uintptr, label string, r *menuRect, flags uintptr) {
	text := syscall.StringToUTF16(label)
	drawText.Call(dc, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)-1), uintptr(unsafe.Pointer(r)), flags)
	runtime.KeepAlive(text)
}
func (s *windowMenuTheme) measure(hwnd uintptr, m *menuMeasure, item themedMenuItem) {
	if item.kind&0x800 != 0 {
		m.Width, m.Height = uint32(s.px(80)), uint32(s.px(9))
		return
	}
	dc, _, _ := getDC.Call(hwnd)
	old, _, _ := selectObject.Call(dc, s.font)
	parts := strings.SplitN(item.label, "\t", 2)
	var r menuRect
	menuText(dc, parts[0], &r, 0x400|0x20) // DT_CALCRECT | DT_SINGLELINE
	width := r.Right + s.px(58)
	if len(parts) == 2 {
		r = menuRect{}
		menuText(dc, parts[1], &r, 0x400|0x20)
		width += r.Right + s.px(28)
	}
	m.Width, m.Height = uint32(max(s.px(160), width)), uint32(s.px(34))
	selectObject.Call(dc, old)
	releaseDC.Call(hwnd, dc)
}
func paintMenuRect(dc uintptr, r menuRect, c ui.Color) {
	brush, _, _ := createBrush.Call(colorRef(c))
	fillRect.Call(dc, uintptr(unsafe.Pointer(&r)), brush)
	deleteObject.Call(brush)
}
func (s *windowMenuTheme) draw(d *menuDraw, item themedMenuItem) {
	t := s.theme()
	bg, fg := menuSurface(t), t.Text
	disabled := d.State&(0x2|0x4) != 0
	selected := d.State&(0x1|0x40) != 0 && !disabled
	if disabled {
		fg = t.TextMuted.Mix(bg, .35)
	}
	backdrop := s.backdrops[d.Window]
	paintMenuBackdrop(d.DC, d.Rect, bg, backdrop)
	r := d.Rect
	if item.kind&0x800 != 0 {
		r.Left += s.px(12)
		r.Right -= s.px(12)
		r.Top = (r.Top + r.Bottom) / 2
		r.Bottom = r.Top + 1
		paintMenuRect(d.DC, r, t.Border)
		return
	}
	if selected {
		amount := float32(.10)
		if t.Dark {
			amount = .18
		}
		r.Left += s.px(4)
		r.Right -= s.px(4)
		r.Top += s.px(2)
		r.Bottom -= s.px(2)
		radius := s.px(int32(t.Radius))
		region, _, _ := createRoundRect.Call(uintptr(r.Left), uintptr(r.Top), uintptr(r.Right+1), uintptr(r.Bottom+1), uintptr(radius*2), uintptr(radius*2))
		saved, _, _ := menuGDI.NewProc("SaveDC").Call(d.DC)
		menuGDI.NewProc("ExtSelectClipRgn").Call(d.DC, region, 1) // RGN_AND: retain the native clip.
		highlight := bg.Mix(t.Accent, amount)
		highlight.A = bg.A
		paintMenuBackdrop(d.DC, r, highlight, backdrop)
		menuGDI.NewProc("RestoreDC").Call(d.DC, saved)
		deleteObject.Call(region)
	}
	r = d.Rect
	old, _, _ := selectObject.Call(d.DC, s.font)
	mode, _, _ := setBGMode.Call(d.DC, 1)
	color, _, _ := setTextColor.Call(d.DC, colorRef(fg))
	flags := uintptr(0x20 | 0x4) // SINGLELINE | VCENTER
	if d.State&0x100 != 0 {
		flags |= 0x100000
	} // HIDE_PREFIX
	r.Left += s.px(32)
	r.Right -= s.px(28)
	parts := strings.SplitN(item.label, "\t", 2)
	menuText(d.DC, parts[0], &r, flags)
	if len(parts) == 2 {
		if !disabled {
			setTextColor.Call(d.DC, colorRef(t.TextMuted))
		}
		menuText(d.DC, parts[1], &r, flags|2)
		setTextColor.Call(d.DC, colorRef(fg))
	}
	if d.State&8 != 0 {
		if !disabled {
			setTextColor.Call(d.DC, colorRef(t.Accent))
		}
		r = d.Rect
		r.Right = r.Left + s.px(28)
		mark := "✓"
		if item.kind&0x200 != 0 {
			mark = "•"
		}
		menuText(d.DC, mark, &r, flags|1)
		setTextColor.Call(d.DC, colorRef(fg))
	}
	if item.submenu {
		r = d.Rect
		r.Left = r.Right - s.px(24)
		menuText(d.DC, "›", &r, flags|1)
	}
	setTextColor.Call(d.DC, color)
	setBGMode.Call(d.DC, mode)
	selectObject.Call(d.DC, old)
}
func (s *windowMenuTheme) mnemonic(menu uintptr, key rune) uintptr {
	n, _, _ := getMenuItemCount.Call(menu)
	for i := uintptr(0); i < n; i++ {
		it := menuItemInfo{Mask: 0x20 | 0x1}
		it.Size = uint32(unsafe.Sizeof(it))
		getMenuItemInfo.Call(menu, i, 1, uintptr(unsafe.Pointer(&it)))
		item, ok := s.items[it.Data]
		if !ok || it.State&3 != 0 {
			continue
		}
		runes := []rune(item.label)
		for j := 0; j+1 < len(runes); j++ {
			if runes[j] != '&' {
				continue
			}
			j++
			if runes[j] == '&' {
				continue
			}
			if unicode.ToLower(runes[j]) == unicode.ToLower(key) {
				return i | 2<<16
			} // MNC_EXECUTE
		}
	}
	return 0
}
