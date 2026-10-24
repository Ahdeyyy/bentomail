//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var captionUser = syscall.NewLazyDLL("user32.dll")
var captionSubclass uintptr

func init() { captionSubclass = syscall.NewCallback(captionWindowProc) }

type captionPoint struct{ X, Y int32 }
type captionRect struct{ Left, Top, Right, Bottom int32 }
type captionMonitorInfo struct {
	Size          uint32
	Monitor, Work captionRect
	Flags         uint32
}
type captionMinMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize captionPoint
}
type captionWindowPos struct {
	Window, InsertAfter uintptr
	X, Y, Width, Height int32
	Flags               uint32
}

func captionWorkArea(hwnd uintptr) (captionMonitorInfo, bool) {
	monitor, _, _ := captionUser.NewProc("MonitorFromWindow").Call(hwnd, 2) // nearest monitor
	return captionMonitorWorkArea(monitor)
}

func captionMonitorWorkArea(monitor uintptr) (captionMonitorInfo, bool) {
	info := captionMonitorInfo{Size: uint32(unsafe.Sizeof(captionMonitorInfo{}))}
	ok, _, _ := captionUser.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info)))
	return info, ok != 0
}

// Removing WS_CAPTION also removes Windows' normal maximized-frame bounds.
// MyGo fits the *client* to the work area, but the outer window can still cover
// the monitor, causing Explorer to treat it as full screen after activation.
// Keep the outer window in the work area on maximize and later frame changes.
func captionWindowProc(hwnd, msg, wp, lp, id, ref uintptr) uintptr {
	r, _, _ := defSubclassProc.Call(hwnd, msg, wp, lp)
	switch msg {
	case 0x0024: // WM_GETMINMAXINFO; preserve framework tracking constraints.
		if info, ok := captionWorkArea(hwnd); ok {
			limits := menuMessage[captionMinMaxInfo](lp)
			limits.MaxPosition = captionPoint{info.Work.Left - info.Monitor.Left, info.Work.Top - info.Monitor.Top}
			limits.MaxSize = captionPoint{info.Work.Right - info.Work.Left, info.Work.Bottom - info.Work.Top}
		}
	case 0x0046: // WM_WINDOWPOSCHANGING: includes later SWP_FRAMECHANGED updates.
		zoomed, _, _ := captionUser.NewProc("IsZoomed").Call(hwnd)
		iconic, _, _ := captionUser.NewProc("IsIconic").Call(hwnd)
		if zoomed != 0 && iconic == 0 {
			pos := menuMessage[captionWindowPos](lp)
			info, ok := captionWorkArea(hwnd)
			if pos.Flags&0x3 == 0 { // A move/resize may target another monitor.
				proposed := captionRect{pos.X, pos.Y, pos.X + pos.Width, pos.Y + pos.Height}
				monitor, _, _ := captionUser.NewProc("MonitorFromRect").Call(uintptr(unsafe.Pointer(&proposed)), 2)
				info, ok = captionMonitorWorkArea(monitor)
			}
			if ok {
				if pos.Flags&0x2 == 0 { // SWP_NOMOVE
					pos.X, pos.Y = info.Work.Left, info.Work.Top
				}
				if pos.Flags&0x1 == 0 { // SWP_NOSIZE
					pos.Width, pos.Height = info.Work.Right-info.Work.Left, info.Work.Bottom-info.Work.Top
				}
			}
		}
	case 0x0082: // WM_NCDESTROY
		removeWindowSubclass.Call(hwnd, captionSubclass, id)
	}
	return r
}

// MyGo's hidden title bar supplies its own caption-button child window. With
// an Acrylic frame, DWM can also draw the system caption underneath it when
// maximized. Remove only that caption; retain resizing, the system menu, and
// minimize/maximize styles used by MyGo's controls and Windows snap layouts.
func suppressSystemCaption(hwnd uintptr) error {
	if hwnd == 0 {
		return nil
	}
	if ok, _, err := setWindowSubclass.Call(hwnd, captionSubclass, 2, 0); ok == 0 {
		return fmt.Errorf("install maximized window bounds: %w", err)
	}
	index := int32(-16) // GWL_STYLE
	style, _, err := captionUser.NewProc("GetWindowLongPtrW").Call(hwnd, uintptr(index))
	if style == 0 {
		return fmt.Errorf("read window style: %w", err)
	}
	const caption = 0x00c00000 // WS_CAPTION
	if style&caption != 0 {
		if old, _, err := captionUser.NewProc("SetWindowLongPtrW").Call(hwnd, uintptr(index), style&^caption); old == 0 {
			return fmt.Errorf("remove system caption: %w", err)
		}
	}
	// SWP_FRAMECHANGED | SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER |
	// SWP_NOACTIVATE: refresh the frame without moving or focusing the window.
	var x, y, width, height int32
	flags := uintptr(0x37)
	if zoomed, _, _ := captionUser.NewProc("IsZoomed").Call(hwnd); zoomed != 0 {
		if info, ok := captionWorkArea(hwnd); ok {
			x, y, width, height = info.Work.Left, info.Work.Top, info.Work.Right-info.Work.Left, info.Work.Bottom-info.Work.Top
			flags = 0x34 // Also fit windows restored maximized before installation.
		}
	}
	if ok, _, err := captionUser.NewProc("SetWindowPos").Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(width), uintptr(height), flags); ok == 0 {
		return fmt.Errorf("refresh window frame: %w", err)
	}
	return nil
}
