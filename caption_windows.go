//go:build windows

package main

import (
	"fmt"
	"syscall"
)

var captionUser = syscall.NewLazyDLL("user32.dll")

// MyGo's hidden title bar supplies its own caption-button child window. With
// an Acrylic frame, DWM can also draw the system caption underneath it when
// maximized. Remove only that caption; retain resizing, the system menu, and
// minimize/maximize styles used by MyGo's controls and Windows snap layouts.
func suppressSystemCaption(hwnd uintptr) error {
	if hwnd == 0 {
		return nil
	}
	index := int32(-16) // GWL_STYLE
	style, _, err := captionUser.NewProc("GetWindowLongPtrW").Call(hwnd, uintptr(index))
	if style == 0 {
		return fmt.Errorf("read window style: %w", err)
	}
	const caption = 0x00c00000 // WS_CAPTION
	if style&caption == 0 {
		return nil
	}
	if old, _, err := captionUser.NewProc("SetWindowLongPtrW").Call(hwnd, uintptr(index), style&^caption); old == 0 {
		return fmt.Errorf("remove system caption: %w", err)
	}
	// SWP_FRAMECHANGED | SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER |
	// SWP_NOACTIVATE: refresh the frame without moving or focusing the window.
	if ok, _, err := captionUser.NewProc("SetWindowPos").Call(hwnd, 0, 0, 0, 0, 0, 0x37); ok == 0 {
		return fmt.Errorf("refresh window frame: %w", err)
	}
	return nil
}
