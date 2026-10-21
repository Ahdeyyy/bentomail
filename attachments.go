package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) previewAttachment(at mailclient.Attachment) {
	if !strings.HasPrefix(at.ContentType, "image/") {
		a.saveAttachment(at)
		return
	}
	var bitmap *ui.Bitmap
	a.run("Image preview ready", func() error {
		config, _, err := image.DecodeConfig(bytes.NewReader(at.Data))
		if err != nil {
			return err
		}
		if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 40_000_000 {
			return fmt.Errorf("image is too large to preview; save it to open externally")
		}
		bitmap, err = ui.DecodeBitmap(at.Data)
		return err
	}, func(err error) {
		if err != nil {
			return
		}
		newAppWindow(mygo.WindowOptions{Title: at.Name + " · Bentomail", Width: 900, Height: 680, MinWidth: 400, MinHeight: 300, Content: ui.View(func(c *ui.Context) {
			theme := a.theme
			c.SetTheme(theme)
			ui.Column(c).Fill().Background(theme.Background).Children(func() {
				windowTitlebar(c)
				ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
					ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Text(c, at.Name).FontSize(17).Bold().Grow(1)
						action(c, "download", "Save image", false, func() { a.saveAttachment(at) })
					})
					ui.Image(c, bitmap).Grow(1).FillWidth()
					ui.Text(c, fileSize(len(at.Data))).FontSize(11).TextColor(theme.TextMuted)
				})
			})
		})}, func() *ui.Theme { return a.theme })
	})
}
