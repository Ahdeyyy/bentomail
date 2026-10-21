package main

import (
	"hash/fnv"
	"strings"

	"github.com/egoist/mygo/ui"
)

func avatarColors(t *ui.Theme, from string) (background, text ui.Color) {
	// Hash the address so colors stay consistent across views and launches,
	// even when a sender changes their display name.
	_, address := sender(from)
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.ToLower(strings.TrimSpace(address))))
	palette := [...]string{
		"#5260d8", "#147d75", "#be456d", "#986314",
		"#8258cf", "#2874a6", "#39783b", "#b45336",
	}
	color := ui.Hex(palette[hash.Sum32()%uint32(len(palette))])
	if t.Dark {
		return color.Mix(ui.Hex("#111521"), .65), color.Mix(ui.Hex("#ffffff"), .55)
	}
	return color.Mix(ui.Hex("#ffffff"), .88), color
}
