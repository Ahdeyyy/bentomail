// Command brand-assets renders the vector mark through the same MyGo renderer as the app.
package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

func main() {
	var sizes = []int{16, 24, 32, 48, 64, 128, 256, 512}
	var pngs [][]byte
	for _, size := range sizes {
		path := "resources/logo.svg"
		if size <= 32 {
			path = "resources/logo-micro.svg"
		}
		data, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		logo := ui.MustParseSVG(data)
		theme := ui.LightTheme()
		theme.Background = ui.Transparent
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(theme)
			ui.Box(c).Fill().TextColor(ui.Hex("#5260d8")).Center().Children(func() { ui.Icon(c, logo).Size(float32(size)*.95, float32(size)*.95) })
		}, size, size)
		tt.Frame()
		var out bytes.Buffer
		if err := png.Encode(&out, tt.Image()); err != nil {
			log.Fatal(err)
		}
		pngs = append(pngs, out.Bytes())
		if err = os.WriteFile(filepath.Join("resources", "icon-"+itoa(size)+".png"), out.Bytes(), 0600); err != nil {
			log.Fatal(err)
		}
		if size == 512 {
			if err = os.WriteFile("resources/icon.png", out.Bytes(), 0600); err != nil {
				log.Fatal(err)
			}
		}
	}
	// Windows ICO can embed PNG entries for all standard sizes.
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, uint16(0))
	binary.Write(&ico, binary.LittleEndian, uint16(1))
	binary.Write(&ico, binary.LittleEndian, uint16(len(sizes)-1))
	offset := uint32(6 + 16*(len(sizes)-1))
	for i, size := range sizes[:len(sizes)-1] {
		dimension := byte(size)
		ico.Write([]byte{dimension, dimension, 0, 0})
		binary.Write(&ico, binary.LittleEndian, uint16(1))
		binary.Write(&ico, binary.LittleEndian, uint16(32))
		binary.Write(&ico, binary.LittleEndian, uint32(len(pngs[i])))
		binary.Write(&ico, binary.LittleEndian, offset)
		offset += uint32(len(pngs[i]))
	}
	for _, data := range pngs[:len(pngs)-1] {
		ico.Write(data)
	}
	if err := os.WriteFile("resources/icon.ico", ico.Bytes(), 0600); err != nil {
		log.Fatal(err)
	}
}
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}
