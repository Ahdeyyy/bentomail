package main

import (
	"github.com/egoist/mygo/ui"
	"hash/fnv"
)

func (a *app) tagColor(tag string) ui.Color {
	if value := a.state.TagColors[tag]; value != "" {
		color := ui.Hex(value)
		if a.prefs.Dark {
			color = color.Mix(ui.Hex("#ffffff"), .3)
		}
		return color
	}
	h := fnv.New32a()
	h.Write([]byte(tag))
	l := float32(.48)
	if a.prefs.Dark {
		l = .74
	}
	return ui.Oklch(l, .12, float32(h.Sum32()%360))
}
