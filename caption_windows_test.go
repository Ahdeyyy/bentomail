//go:build windows

package main

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestSuppressSystemCaptionPreservesWindowControls(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const original = 0x00cf0000 // WS_OVERLAPPEDWINDOW: caption, thick frame, system menu, minimize/maximize.
	class := syscall.StringToUTF16Ptr("STATIC")
	hwnd, _, err := captionUser.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, original, 0, 0, 400, 240, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal(err)
	}
	defer captionUser.NewProc("DestroyWindow").Call(hwnd)
	index := int32(-16)
	before, _, _ := captionUser.NewProc("GetWindowLongPtrW").Call(hwnd, uintptr(index))
	for i := 0; i < 2; i++ {
		if err := suppressSystemCaption(hwnd); err != nil {
			t.Fatal(err)
		}
		style, _, _ := captionUser.NewProc("GetWindowLongPtrW").Call(hwnd, uintptr(index))
		if want := before &^ 0x00c00000; style != want {
			t.Fatalf("caption was retained or another window capability changed: style=%#x want=%#x", style, want)
		}
		menu, _, _ := captionUser.NewProc("GetSystemMenu").Call(hwnd, 0)
		if menu == 0 {
			t.Fatal("hidden caption removed the Windows system menu")
		}
		for _, command := range []uintptr{0xf020, 0xf030, 0xf060} { // SC_MINIMIZE, SC_MAXIMIZE, SC_CLOSE
			state, _, _ := captionUser.NewProc("GetMenuState").Call(menu, command, 0)
			if uint32(state) == 0xffffffff || state&3 != 0 {
				t.Fatalf("window command %#x disappeared or became disabled: %#x", command, state)
			}
		}
	}
}

func TestCaptionlessMaximizedWindowStaysInWorkArea(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class := syscall.StringToUTF16Ptr("STATIC")
	// A hidden maximized window exercises native positioning without stealing
	// focus or changing the user's desktop during the regression test.
	hwnd, _, err := captionUser.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0x01cf0000, 0, 0, 400, 240, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatal(err)
	}
	defer captionUser.NewProc("DestroyWindow").Call(hwnd)
	if zoomed, _, _ := captionUser.NewProc("IsZoomed").Call(hwnd); zoomed == 0 {
		t.Fatal("test window is not maximized")
	}
	info, ok := captionWorkArea(hwnd)
	if !ok {
		t.Fatal("cannot query monitor work area")
	}
	for i := 0; i < 3; i++ {
		if err := suppressSystemCaption(hwnd); err != nil {
			t.Fatal(err)
		}
		var limits captionMinMaxInfo
		captionUser.NewProc("SendMessageW").Call(hwnd, 0x0024, 0, uintptr(unsafe.Pointer(&limits)))
		wantPosition := captionPoint{info.Work.Left - info.Monitor.Left, info.Work.Top - info.Monitor.Top}
		wantSize := captionPoint{info.Work.Right - info.Work.Left, info.Work.Bottom - info.Work.Top}
		if limits.MaxPosition != wantPosition || limits.MaxSize != wantSize {
			t.Fatalf("maximize covers taskbar: position=%+v size=%+v; want %+v %+v", limits.MaxPosition, limits.MaxSize, wantPosition, wantSize)
		}
		// Simulate a later DPI/material/frame refresh proposing monitor bounds.
		monitor := info.Monitor
		if ok, _, err := captionUser.NewProc("SetWindowPos").Call(hwnd, 0, uintptr(monitor.Left), uintptr(monitor.Top), uintptr(monitor.Right-monitor.Left), uintptr(monitor.Bottom-monitor.Top), 0x34); ok == 0 {
			t.Fatal(err)
		}
		var bounds captionRect
		captionUser.NewProc("GetWindowRect").Call(hwnd, uintptr(unsafe.Pointer(&bounds)))
		if bounds != info.Work {
			t.Fatalf("frame update covered the taskbar: bounds=%+v work=%+v", bounds, info.Work)
		}
		visible, _, _ := captionUser.NewProc("IsWindowVisible").Call(hwnd)
		if visible != 0 {
			t.Fatal("frame update unexpectedly showed the hidden window")
		}
	}
}
