package main

import "github.com/egoist/mygo/ui"

const (
	minSidebarWidth = float32(224)
	minMessageWidth = float32(300)
	minReaderWidth  = float32(400)
	// Mail has 20 DIPs of outer padding and 13 between the two panes.
	minWorkspaceWidth = minMessageWidth + minReaderWidth + 33
)

// Constrain the split values before every build, including the rebuild after
// a divider drag or keyboard resize. Both sides keep their readable minimum.
func (a *app) constrainPaneWidths(c *ui.Context) {
	width, _ := c.Size()
	a.prefs.SidebarWidth = max(a.prefs.SidebarWidth, minSidebarWidth)
	sidebarWidth := float32(80)
	if !a.prefs.SidebarCollapsed {
		a.prefs.SidebarWidth = min(a.prefs.SidebarWidth, max(minSidebarWidth, width-minWorkspaceWidth-1))
		sidebarWidth = a.prefs.SidebarWidth + 1
	}
	if a.prefs.Layout == "Right" {
		available := width - sidebarWidth - 20
		a.prefs.ListWidth = max(minMessageWidth+6, min(a.prefs.ListWidth, available-minReaderWidth-7))
	}
}
