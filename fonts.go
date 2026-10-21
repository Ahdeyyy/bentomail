package main

import (
	"embed"
	"fmt"

	"github.com/egoist/mygo/ui"
)

//go:embed resources/fonts/*.ttf
var interfaceFonts embed.FS

// Register before any window or Tester shapes text. Bundling the static faces
// gives the native renderer the same typography on every supported platform.
func init() {
	for _, weight := range []string{"Regular", "Medium", "Bold"} {
		data, err := interfaceFonts.ReadFile("resources/fonts/MartianGrotesk-" + weight + ".ttf")
		if err == nil {
			err = ui.RegisterFont(data, "Martian Grotesk")
		}
		if err != nil {
			panic(fmt.Errorf("register interface font: %w", err))
		}
	}
}
