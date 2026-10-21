package main

import (
	"encoding/base64"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo"
)

func fileSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.0f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}
func safeName(s string) string {
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, s)
	s = strings.Trim(s, " .")
	if s == "" || s == "." {
		s = "attachment"
	}
	return s
}
func (a *app) confirm(title, message string, fn func()) {
	a.confirmTitle, a.confirmMessage, a.confirmAction = title, message, fn
	a.confirmOpen = true
	if a.window != nil {
		a.window.Invalidate()
	}
}
func (a *app) saveFile(name string, data []byte) {
	dispatch, win := a.dispatch, a.window
	if dispatch == nil {
		return
	}
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: win, Title: "Save file", DefaultPath: safeName(name)})
		dispatch(func() {
			if a.closed {
				return
			}
			if err != nil {
				a.notice(err.Error(), true)
				return
			}
			if path == "" {
				return
			}
			a.run("File saved", func() error { return os.WriteFile(path, data, 0600) })
		})
	}()
}
func (a *app) saveAttachment(at mailclient.Attachment) { a.saveFile(at.Name, at.Data) }
func (a *app) saveAllAttachments(ats []mailclient.Attachment) {
	dispatch, win := a.dispatch, a.window
	if dispatch == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: "Save attachments to folder", Directory: true})
		dispatch(func() {
			if a.closed {
				return
			}
			if err != nil {
				a.notice(err.Error(), true)
				return
			}
			if len(paths) == 0 {
				return
			}
			dir := paths[0]
			a.run("Attachments saved", func() error {
				for _, at := range ats {
					name := safeName(at.Name)
					var f *os.File
					var err error
					for i := 0; i < 1000; i++ {
						candidate := name
						if i > 0 {
							candidate = fmt.Sprintf("%d-%s", i, name)
						}
						f, err = os.OpenFile(filepath.Join(dir, candidate), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
						if os.IsExist(err) {
							continue
						}
						break
					}
					if err != nil {
						return err
					}
					_, err = f.Write(at.Data)
					closeErr := f.Close()
					if err != nil {
						return err
					}
					if closeErr != nil {
						return closeErr
					}
				}
				return nil
			})
		})
	}()
}
func (a *app) exportEML(m mailclient.Message) { a.saveFile(safeName(m.Subject)+".eml", m.Raw) }
func (a *app) backup() {
	dispatch, win, s := a.dispatch, a.window, a.store
	if dispatch == nil {
		return
	}
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: win, Title: "Back up Bentomail", DefaultPath: "bentomail-backup.json"})
		dispatch(func() {
			if a.closed {
				return
			}
			if err != nil {
				a.notice(err.Error(), true)
				return
			}
			if path != "" {
				a.run("Backup saved", func() error { return s.Backup(path) })
			}
		})
	}()
}
func (a *app) restore() {
	dispatch, win, s := a.dispatch, a.window, a.store
	if dispatch == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: "Restore Bentomail backup", Filters: []mygo.FileFilter{{Name: "Backup", Extensions: []string{"json"}}}})
		dispatch(func() {
			if a.closed {
				return
			}
			if err != nil {
				a.notice(err.Error(), true)
				return
			}
			if len(paths) > 0 {
				path := paths[0]
				a.confirm("Restore backup?", "The backup will replace the local mail database. Back up your current mail first.", func() {
					a.run("Backup restored", func() error { return s.Restore(path) }, func(err error) {
						if err == nil {
							a.prefs = a.state.Preferences
							a.prefs.Translucency = normalizedTranslucency(a.prefs.Translucency)
							a.theme = workspaceTheme(a.prefs.Dark, a.prefs.Accent, a.prefs.Translucency)
							a.composers = nil
							a.navigate("inbox")
						}
					})
				})
			}
		})
	}()
}
func (a *app) importEML() {
	dispatch, win, s, id := a.dispatch, a.window, a.store, a.accountID
	if dispatch == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win, Title: "Import email", Multiple: true, Filters: []mygo.FileFilter{{Name: "Email", Extensions: []string{"eml"}}}})
		dispatch(func() {
			if a.closed {
				return
			}
			if err != nil {
				a.notice(err.Error(), true)
				return
			}
			if len(paths) > 0 {
				a.run("Email imported", func() error {
					for _, p := range paths {
						data, err := os.ReadFile(p)
						if err != nil {
							return err
						}
						if err = s.ImportEML(id, data); err != nil {
							return err
						}
					}
					return nil
				})
			}
		})
	}()
}
func (a *app) updatePreview() {
	if a.loadHTML == nil {
		return
	}
	m, ok := a.selected()
	if !ok {
		return
	}
	_, address := sender(m.From)
	remoteImages := a.prefs.RemoteImages || slices.Contains(a.prefs.TrustedSenders, strings.ToLower(address))
	key := fmt.Sprintf("%s:%s:%t:%t:%d:%s", m.ID, a.bodyMode, a.prefs.Dark, remoteImages, a.prefs.ReadingSize, a.prefs.ReadingFont)
	if key == a.previewKey {
		return
	}
	a.previewKey = key
	source := m.HTML
	if source == "" {
		source = "<pre>" + html.EscapeString(m.Text) + "</pre>"
	}
	for _, at := range m.Attachments {
		if at.ContentID != "" && strings.HasPrefix(at.ContentType, "image/") {
			source = strings.ReplaceAll(source, "cid:"+strings.Trim(at.ContentID, "<>"), "data:"+at.ContentType+";base64,"+base64.StdEncoding.EncodeToString(at.Data))
		}
	}
	source = mailclient.SanitizeHTML(source, remoteImages)
	if len(source) > 1_500_000 {
		a.bodyMode = "Plain text"
		a.previewKey = "large:" + m.ID
		a.printPending = false
		a.notice("This HTML message is too large to preview; showing plain text. Attachments remain available.", false)
		return
	}
	images := "data:"
	if remoteImages {
		images += " https: http:"
	}
	bg, fg := "#ffffff", "#20293e"
	if a.prefs.Dark {
		bg = "#1a2030"
		fg = "#e9edf7"
	}
	font := "Arial"
	switch a.prefs.ReadingFont {
	case "Georgia", "Consolas":
		font = a.prefs.ReadingFont
	}
	a.loadHTML(`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src ` + images + `; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-src 'none'; connect-src 'none'"><style>html{color-scheme:` + map[bool]string{true: "dark", false: "light"}[a.prefs.Dark] + `;background:` + bg + `;color:` + fg + `;}body{font:` + fmt.Sprint(a.prefs.ReadingSize) + `px/1.7 ` + font + `,sans-serif;margin:20px 26px;overflow-wrap:anywhere}img{max-width:100%;height:auto}pre{white-space:pre-wrap}blockquote{border-left:3px solid #a3adff;margin-left:0;padding-left:16px;color:#66728a}a{color:#5260d8}table{max-width:100%}</style></head><body>` + source + `</body></html>`)
}
