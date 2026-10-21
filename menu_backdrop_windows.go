//go:build windows

package main

import (
	"runtime"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

type menuBitmapHeader struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	ClrUsed, ClrImportant  uint32
}

func bitmapHeader(width, height int) menuBitmapHeader {
	return menuBitmapHeader{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
}

// GDI menus cannot paint alpha over a DWM material. Capture the backdrop
// before each popup opens, blur it at quarter resolution, then composite the
// surface and item highlights over it. Text is drawn separately at full
// opacity. Captures live only for the menu loop and are never written to disk.
type menuBackdrop struct {
	left, top, screenWidth, screenHeight int
	width, height                        int
	pixels                               []byte // top-down BGRA
}

func captureMenuBackdrop(radius int) *menuBackdrop {
	metric := func(index uintptr) int {
		value, _, _ := menuUser.NewProc("GetSystemMetrics").Call(index)
		return int(int32(value))
	}
	b := &menuBackdrop{left: metric(76), top: metric(77), screenWidth: metric(78), screenHeight: metric(79)}
	b.width, b.height = (b.screenWidth+3)/4, (b.screenHeight+3)/4
	if b.width <= 0 || b.height <= 0 || int64(b.width)*int64(b.height) > 4_000_000 {
		return nil
	}
	screen, _, _ := getDC.Call(0)
	if screen == 0 {
		return nil
	}
	defer releaseDC.Call(0, screen)
	dc, _, _ := menuGDI.NewProc("CreateCompatibleDC").Call(screen)
	if dc == 0 {
		return nil
	}
	defer menuGDI.NewProc("DeleteDC").Call(dc)
	header := bitmapHeader(b.width, b.height)
	var bits uintptr
	bitmap, _, _ := menuGDI.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == 0 {
		return nil
	}
	defer deleteObject.Call(bitmap)
	old, _, _ := selectObject.Call(dc, bitmap)
	defer selectObject.Call(dc, old)
	menuGDI.NewProc("SetStretchBltMode").Call(dc, 4) // HALFTONE
	ok, _, _ := menuGDI.NewProc("StretchBlt").Call(dc, 0, 0, uintptr(b.width), uintptr(b.height), screen,
		uintptr(b.left), uintptr(b.top), uintptr(b.screenWidth), uintptr(b.screenHeight), 0x00cc0020) // SRCCOPY
	if ok == 0 {
		return nil
	}
	menuGDI.NewProc("GdiFlush").Call()
	b.pixels = append([]byte(nil), unsafe.Slice(menuMessage[byte](bits), b.width*b.height*4)...)
	blurMenuPixels(b.pixels, b.width, b.height, radius)
	return b
}

// Three separable box passes approximate a soft Gaussian blur in linear time.
func blurMenuPixels(pixels []byte, width, height, radius int) {
	tmp := make([]byte, len(pixels))
	span := radius*2 + 1
	for pass := 0; pass < 3; pass++ {
		for y := 0; y < height; y++ {
			for channel := 0; channel < 3; channel++ {
				sum := 0
				for dx := -radius; dx <= radius; dx++ {
					sum += int(pixels[(y*width+max(0, min(width-1, dx)))*4+channel])
				}
				for x := 0; x < width; x++ {
					tmp[(y*width+x)*4+channel] = byte(sum / span)
					sum -= int(pixels[(y*width+max(0, x-radius))*4+channel])
					sum += int(pixels[(y*width+min(width-1, x+radius+1))*4+channel])
				}
			}
		}
		for x := 0; x < width; x++ {
			for channel := 0; channel < 3; channel++ {
				sum := 0
				for dy := -radius; dy <= radius; dy++ {
					sum += int(tmp[(max(0, min(height-1, dy))*width+x)*4+channel])
				}
				for y := 0; y < height; y++ {
					pixels[(y*width+x)*4+channel] = byte(sum / span)
					sum -= int(tmp[(max(0, y-radius)*width+x)*4+channel])
					sum += int(tmp[(min(height-1, y+radius+1)*width+x)*4+channel])
				}
			}
		}
	}
}

func paintMenuBackdrop(dc uintptr, r menuRect, tint ui.Color, b *menuBackdrop) {
	if b == nil || tint.A == 255 {
		paintMenuRect(dc, r, tint)
		return
	}
	var origin struct{ X, Y int32 }
	if ok, _, _ := menuGDI.NewProc("GetDCOrgEx").Call(dc, uintptr(unsafe.Pointer(&origin))); ok == 0 {
		paintMenuRect(dc, r, tint)
		return
	}
	width, height := int(r.Right-r.Left), int(r.Bottom-r.Top)
	if width <= 0 || height <= 0 {
		return
	}
	pixels := make([]byte, width*height*4)
	alpha := int(tint.A)
	colors := [3]byte{tint.B, tint.G, tint.R}
	for y := 0; y < height; y++ {
		sy := max(0, min(b.height-1, (int(origin.Y+r.Top)+y-b.top)*b.height/b.screenHeight))
		for x := 0; x < width; x++ {
			sx := max(0, min(b.width-1, (int(origin.X+r.Left)+x-b.left)*b.width/b.screenWidth))
			for channel, color := range colors {
				base := int(b.pixels[(sy*b.width+sx)*4+channel])
				pixels[(y*width+x)*4+channel] = byte((base*(255-alpha) + int(color)*alpha + 127) / 255)
			}
			pixels[(y*width+x)*4+3] = 255
		}
	}
	header := bitmapHeader(width, height)
	menuGDI.NewProc("StretchDIBits").Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(width), uintptr(height),
		0, 0, uintptr(width), uintptr(height), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&header)), 0, 0x00cc0020)
	runtime.KeepAlive(pixels)
}
