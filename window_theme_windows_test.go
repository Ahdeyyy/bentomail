//go:build windows

package main

import (
	"syscall"
	"testing"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

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
	if len(s.items) != 3 || s.color != theme.Background {
		t.Fatal("reopening duplicated items or retained stale colors")
	}
}
