//go:build !windows

package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func installWindowTheme(*mygo.Window, func() *ui.Theme) {}
