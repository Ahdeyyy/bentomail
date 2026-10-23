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
