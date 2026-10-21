package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

func (a *app) view(c *ui.Context) {
	setWindowTheme(c, a.theme)
	c.OnShortcut(ui.Ctrl, ui.KeyN, func() { a.newComposer(nil, "new") })
	c.OnShortcut(ui.Ctrl, ui.KeyF, func() { a.searchHandle.Focus(c) })
	c.OnShortcut(ui.Ctrl, ui.KeyZ, func() {
		if a.page == "mail" {
			a.undoAction()
		}
	})
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Grow(1).FillWidth().Children(func() {
			ui.Split(c, &a.prefs.SidebarWidth, func() { a.sidebar(c) }, func() {
				ui.Column(c).Fill().Children(func() {
					windowTitlebar(c)
					a.topbar(c)
					if a.statusError {
						ui.Row(c).FillWidth().Padding(10, 20).Gap(10).Background(a.theme.Danger.Alpha(.08)).Children(func() {
							ui.Text(c, a.status).FontSize(11).TextColor(a.theme.Danger).Grow(1).Selectable()
							iconButton(c, "close", "Dismiss error", func() { a.statusError = false })
						})
					}
					switch a.page {
					case "settings":
						a.settingsView(c)
					case "compose":
						a.composeView(c)
					default:
						a.mailView(c)
					}
				})
			}).Fill()
		})
		frost(ui.Row(c), surface(a.theme)).Height(28).Shrink(0).FillWidth().Padding(0, 16).Gap(8).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(a.theme.Border).Children(func() {
			color := a.theme.Success
			if a.statusError {
				color = a.theme.Danger
			}
			ui.Box(c).Size(5, 5).Radius(3).Background(color)
			ui.Text(c, a.status).FontSize(10).TextColor(a.theme.TextMuted).MaxLines(1).Grow(1).Tooltip(a.status)
			if len(a.undo) > 0 {
				textButton(c, "Undo").FontSize(10).OnClick(a.undoAction)
			}
			ui.Text(c, "LOCAL STORAGE").FontSize(9).TextColor(a.theme.TextMuted)
		})
	})
	a.confirmationDialog(c)
}
func (a *app) sidebar(c *ui.Context) {
	t := a.theme
	ui.Column(c).Fill().Padding(0, 14, 12, 14).Gap(7).Children(func() {
		ui.Row(c).Label("Sidebar header").Height(52).Shrink(0).Gap(9).AlignItems(ui.Center).Padding(0, 6).DragWindow().Children(func() {
			ui.Box(c).Size(31, 31).TextColor(t.Accent).Center().Children(func() { ui.Icon(c, icon("logo")).Size(31, 31) })
			ui.Text(c, "bentomail").FontSize(18).FontWeight(700).Grow(1)
		})
		action(c, "compose", "Compose", true, func() { a.newComposer(nil, "new") }).FillWidth().Height(40)
		ui.Scroll(c).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Padding(0, 20, 0, 0).Gap(3).Children(func() {
				ui.Text(c, "WORKSPACE").FontSize(9).FontWeight(700).TextColor(t.TextMuted).Padding(22, 10, 8, 10)
				for _, v := range []struct{ id, label, icon string }{{"inbox", "Inbox", "inbox"}, {"starred", "Starred", "star"}, {"snoozed", "Snoozed", "clock"}, {"followup", "Follow up", "clock"}, {"sent", "Sent", "send"}, {"drafts", "Drafts", "compose"}, {"outbox", "Outbox", "send"}, {"archive", "Archive", "archive"}, {"all", "All mail", "mail"}, {"trash", "Trash", "trash"}, {"spam", "Junk", "shield"}} {
					n := 0
					for _, m := range a.state.Messages {
						if (a.accountID == "" || m.AccountID == a.accountID) && !m.Read && canonicalFolder(m.Folder) == v.id {
							n++
						}
					}
					if v.id == "drafts" {
						for _, d := range a.state.Drafts {
							if d.Status == "" || d.Status == "draft" {
								n++
							}
						}
					}
					if v.id == "outbox" {
						for _, d := range a.state.Drafts {
							if d.Status != "" && d.Status != "draft" && d.Status != "sent" {
								n++
							}
						}
					}
					a.nav(c, v.icon, v.label, v.id, n)
				}
				ui.Row(c).FillWidth().Padding(18, 10, 6, 10).Children(func() {
					ui.Text(c, "TAGS").FontSize(9).Bold().TextColor(t.TextMuted).Grow(1)
					iconButton(c, "plus", "Manage tags", func() { a.page = "settings"; a.settingsTab = "Organization" }).Size(20, 20)
				})
				for _, tag := range a.state.Tags {
					a.nav(c, "tag", tag, "tag:"+tag, 0)
				}
				if len(a.state.Tags) == 0 {
					ui.Text(c, "No tags").FontSize(11).TextColor(t.TextMuted).Padding(7, 10)
				}
				if len(a.state.SavedSearches) > 0 {
					ui.Text(c, "SAVED SEARCHES").FontSize(9).Bold().TextColor(t.TextMuted).Padding(18, 10, 6, 10)
					for name, q := range a.state.SavedSearches {
						ui.ButtonBase(c).Label("Search "+name).FillWidth().Padding(9, 10).Radius(7).OnClick(func() { a.query = q; a.navigate("all") }).Children(func() { ui.Icon(c, icon("search")).Size(15, 15); ui.Text(c, name).FontSize(11) })
					}
				}
				folders := a.state.Folders[a.accountID]
				if len(folders) > 0 {
					ui.Text(c, "FOLDERS").FontSize(9).Bold().TextColor(t.TextMuted).Padding(18, 10, 6, 10)
					for _, f := range folders {
						canon := canonicalFolder(f)
						if slices.Contains([]string{"inbox", "sent", "drafts", "archive", "trash", "spam"}, canon) {
							continue
						}
						a.nav(c, "folder", f, f, 0)
					}
				}
			})
		})
		ui.Column(c).FillWidth().Gap(3).Padding(10, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				action(c, "settings", "Settings", false, func() { a.page = "settings" }).Grow(1)
				iconButton(c, "sun", "Toggle dark mode", func() { a.prefs.Dark = !a.prefs.Dark; a.savePrefs() })
			})
			ui.Textf(c, "%d accounts · Stored on this PC", len(a.state.Accounts)).FontSize(9).TextColor(t.TextMuted).Padding(6, 7)
		})
	})
}
func (a *app) nav(c *ui.Context, ic, label, id string, count int) {
	c.Key("nav-" + id)
	sel := a.page == "mail" && a.folder == id
	t := a.theme
	b := ui.ButtonBase(c).Label(label).FillWidth().Padding(9, 10).Gap(10).Radius(7).OnClick(func() { a.navigate(id) })
	if sel {
		b.Background(t.Surface.Mix(t.Accent, .12)).TextColor(t.Accent)
	} else if b.Hovered() {
		b.Background(t.SurfaceHover)
	} else {
		b.TextColor(t.TextMuted)
	}
	b.Children(func() {
		ui.Icon(c, icon(ic)).Size(17, 17)
		ui.Text(c, label).FontSize(12).FontWeight(500).Grow(1).MaxLines(1)
		if count > 0 {
			ui.Textf(c, "%d", count).FontSize(10).Padding(1, 5).Radius(4).Background(t.Accent.Alpha(.1))
		}
	})
	if id != "drafts" && id != "outbox" {
		if mid, ok := ui.Drop[string](b); ok {
			if m, exists := a.findMessage(mid); exists {
				if strings.HasPrefix(id, "tag:") {
					tag := strings.TrimPrefix(id, "tag:")
					if !slices.Contains(m.Tags, tag) {
						m.Tags = append(m.Tags, tag)
					}
					a.mutate(m, false)
				} else if !slices.Contains([]string{"all", "starred", "snoozed", "followup"}, id) {
					m.Folder = id
					a.mutate(m, true)
				}
			}
		}
	}
}
func (a *app) findMessage(id string) (mailclient.Message, bool) {
	for _, m := range a.state.Messages {
		if m.ID == id {
			return m, true
		}
	}
	return mailclient.Message{}, false
}
func (a *app) topbar(c *ui.Context) {
	t := a.theme
	ui.Row(c).Height(68).Shrink(0).FillWidth().Padding(12, 20).Gap(12).AlignItems(ui.Center).Children(func() {
		opts := []string{"All accounts"}
		for _, ac := range a.state.Accounts {
			opts = append(opts, ac.Name+" <"+ac.Email+">")
		}
		ui.Select(c, &a.accountPick, opts).Label("Active account").Width(245).OnChange(func() {
			a.accountID = ""
			for _, ac := range a.state.Accounts {
				if a.accountPick == ac.Name+" <"+ac.Email+">" {
					a.accountID = ac.ID
					break
				}
			}
			a.navigate(a.folder)
		})
		ui.Row(c).Grow(1).Height(38).Padding(0, 11).Gap(8).AlignItems(ui.Center).Background(t.Surface).Radius(8).Border(1, t.Border).Children(func() {
			ui.Icon(c, icon("search")).Size(17, 17).TextColor(t.TextMuted)
			ui.TextInput(c, &a.query).Bind(&a.searchHandle).Label("Search mail").Placeholder("Search your mail…").Grow(1).Border(0, ui.Transparent).Background(ui.Transparent).OnChange(func() { a.page = "mail"; a.selectedID = ""; a.rowIndex = -1; a.searchMail() }).OnShortcut(0, ui.KeyEscape, func() { a.query = "" })
			if a.query != "" {
				iconButton(c, "close", "Clear search", func() { a.query = "" })
			} else {
				ui.Text(c, "Ctrl F").FontSize(10).TextColor(t.TextMuted)
			}
		})
		label := "Sync mail"
		if a.syncing {
			label = "Cancel synchronization"
		}
		iconButton(c, "sync", label, func() {
			if a.syncing && a.syncCancel != nil {
				a.syncCancel()
			} else {
				a.syncMail()
			}
		})
		iconButton(c, "plus", "Add account", func() { a.resetAccountForm(); a.page = "settings"; a.settingsTab = "Accounts" })
	})
}
func title(s string) string {
	if strings.HasPrefix(s, "tag:") {
		return strings.TrimPrefix(s, "tag:")
	}
	if s == "all" {
		return "All mail"
	}
	if s == "followup" {
		return "Follow up"
	}
	if s == "outbox" {
		return "Outbox"
	}
	if len(s) == 0 {
		return "Inbox"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
func (a *app) mailView(c *ui.Context) {
	ui.Column(c).Grow(1).FillWidth().MinHeight(0).Padding(0, 20, 16, 10).Gap(12).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Text(c, title(a.folder)).FontSize(26).Bold().Grow(1)
			if a.query != "" {
				textButton(c, "Save search").OnClick(func() { a.page = "settings"; a.settingsTab = "Organization"; a.searchName = a.query })
			}
			menuButton(c, "View", func(m *ui.Menu) {
				for _, v := range []string{"Right", "Bottom", "Hidden"} {
					if m.Item(v + " reading pane").Chosen() {
						a.prefs.Layout = v
						a.savePrefs()
					}
				}
				m.Separator()
				if m.Item("Toggle conversations").Chosen() {
					a.threaded = !a.threaded
				}
				if m.Item("Reverse sort order").Chosen() {
					a.sortOldest = !a.sortOldest
				}
			})
		})
		if a.folder == "drafts" || a.folder == "outbox" {
			a.draftsView(c)
			return
		}
		if a.prefs.Layout == "Hidden" {
			if a.selectedID != "" {
				panel(c, ui.Column(c)).Fill().Children(func() { action(c, "reply", "Back to messages", false, func() { a.selectedID = "" }); a.reader(c) })
			} else {
				a.messageList(c)
			}
			return
		}
		if a.prefs.Layout == "Bottom" {
			ui.SplitVertical(c, &a.prefs.ListWidth, func() {
				ui.Column(c).Fill().Padding(0, 0, 6, 0).Children(func() { a.messageList(c) })
			}, func() {
				ui.Column(c).Fill().Padding(6, 0, 0, 0).Children(func() { a.reader(c) })
			}).Grow(1).FillWidth().MinHeight(0)
		} else {
			ui.Split(c, &a.prefs.ListWidth, func() {
				ui.Column(c).Fill().Padding(0, 6, 0, 0).Children(func() { a.messageList(c) })
			}, func() {
				ui.Column(c).Fill().Padding(0, 0, 0, 6).Children(func() { a.reader(c) })
			}).Grow(1).FillWidth().MinHeight(0)
		}
	})
}
func (a *app) messageList(c *ui.Context) {
	ms, t := a.messages(), a.theme
	panel(c, ui.Column(c)).Fill().Children(func() {
		ui.Column(c).FillWidth().Gap(9).Padding(12).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Textf(c, "%d conversations", len(ms)).FontSize(11).TextColor(t.TextMuted).Grow(1)
				iconButton(c, "more", "Selected message actions", func() {}).Menu(a.bulkMenu)
			})
			ui.Row(c).Gap(5).FillWidth().Children(func() {
				for _, f := range []string{"All", "Unread", "Attachments"} {
					c.Key("filter" + f)
					b := ui.ButtonBase(c).Label(f+" filter").Padding(6, 9).Radius(6).OnClick(func() {
						a.filter = f
						if f == "All" {
							a.filter = ""
						}
					})
					if a.filter == f || (a.filter == "" && f == "All") {
						b.Background(t.Surface.Mix(t.Accent, .12)).TextColor(t.Accent)
					} else {
						b.TextColor(t.TextMuted)
					}
					b.Children(func() { ui.Text(c, f).FontSize(10) })
				}
			})
		})
		if len(ms) == 0 {
			ui.Column(c).Fill().Center().Padding(24).Gap(12).Children(func() {
				ui.Icon(c, icon("inbox")).Size(32, 32).TextColor(t.TextMuted)
				ui.Text(c, "No messages").FontSize(15).Bold()
				if len(a.state.Accounts) == 0 {
					action(c, "plus", "Add account", true, func() { a.page = "settings"; a.settingsTab = "Accounts" })
				} else if a.query != "" {
					textButton(c, "Clear search").OnClick(func() { a.query = "" })
				}
			})
			return
		}
		a.list.Label = func(i int) string { return ms[i].Subject }
		a.list.Selection = &a.selection
		ui.List(c, &a.list, len(ms)).Label("Message list").Grow(1).FillWidth().Padding(5, 20, 5, 5).Gap(3).ItemKey(func(i int) any { return ms[i].ID }).Rows(func(row ui.ListRow) {
			m, rc := ms[row.Index], row.Context
			name, _ := sender(m.From)
			compact := a.prefs.Density == "Compact"
			pad := float32(13)
			if compact {
				pad = 8
			}
			selected := m.ID == a.selectedID || row.Selected()
			text, muted := t.Text, t.TextMuted
			if selected {
				text, muted = t.AccentText, t.AccentText
			}
			r := ui.Row(rc).FillWidth().Padding(pad, 10).Gap(10).Radius(8).TextColor(text)
			if selected {
				r.Background(t.Accent)
			} else if r.Hovered() {
				r.Background(t.SurfaceHover)
			}
			r.ContextMenu(func(menu *ui.Menu) { a.messageMenu(menu, m) })
			r.Children(func() {
				if !compact {
					avatarBG, avatarText := avatarColors(t, m.From)
					ui.Box(rc).Size(32, 32).Shrink(0).Radius(11).Center().Background(avatarBG).TextColor(avatarText).Children(func() { ui.Text(rc, initials(name)).FontSize(11).Bold() })
				}
				ui.Column(rc).Grow(1).MinWidth(0).Gap(5).Children(func() {
					ui.Row(rc).FillWidth().Gap(5).Children(func() {
						label := name
						if !m.Read {
							label = "• " + label
						}
						ui.Text(rc, label).FontSize(11).FontWeight(500).MaxLines(1).Grow(1).MinWidth(0)
						ui.Text(rc, mailDate(m.Date)).FontSize(9).TextColor(muted)
					})
					subject := m.Subject
					if subject == "" {
						subject = "(No subject)"
					}
					s := ui.Text(rc, subject).FontSize(12).MaxLines(1)
					if !m.Read {
						s.Bold()
					}
					if !compact && !a.prefs.HideAvatars {
						ui.Text(rc, strings.Join(strings.Fields(m.Text), " ")).FontSize(10).TextColor(muted).MaxLines(max(1, a.prefs.PreviewLines))
					}
					if len(m.Tags) > 0 || len(m.Attachments) > 0 || m.Starred || a.accountID == "" {
						ui.Row(rc).Gap(4).FillWidth().Wrap().Children(func() {
							if a.accountID == "" {
								account := a.account(m.AccountID)
								label := account.Name
								if label == "" {
									label = "Local"
								}
								color := t.Accent
								if account.Color != "" {
									color = ui.Hex(account.Color)
								}
								if selected {
									color = text
								}
								ui.Text(rc, label).FontSize(8).Padding(2, 4).Radius(3).TextColor(color).Background(color.Alpha(.08))
							}
							if m.Starred {
								color := t.Warning
								if selected {
									color = text
								}
								ui.Icon(rc, icon("star")).Size(12, 12).TextColor(color)
							}
							if len(m.Attachments) > 0 {
								ui.Icon(rc, icon("attachment")).Size(12, 12).TextColor(muted)
							}
							for _, tag := range m.Tags {
								color := a.tagColor(tag)
								if selected {
									color = text
								}
								ui.Text(rc, tag).FontSize(8).Padding(2, 4).Radius(3).Background(color.Alpha(.09)).TextColor(color)
							}
						})
					}
				})
				// A drag source consumes pointer input. Keep it off the row so
				// the native List receives clicks and modifier-key selection.
				ui.Box(rc).Size(12, 24).Shrink(0).Center().Drag(m.ID).Label("Drag message " + m.Subject).Tooltip("Drag to a folder or tag").TextColor(muted).Children(func() {
					ui.Icon(rc, icon("grip")).Size(12, 20)
				})
			})
		}).OnChange(func() {
			if a.rowIndex >= 0 && a.rowIndex < len(ms) {
				a.selectMessage(ms[a.rowIndex].ID)
			}
		})
		ui.Text(c, "↑ ↓ Navigate   ·   Ctrl click to select").FontSize(9).TextColor(t.TextMuted).Padding(10, 12)
	})
}
func mailDate(t time.Time) string {
	now := time.Now()
	if t.Format("2006-01-02") == now.Format("2006-01-02") {
		return t.Format("15:04")
	}
	return t.Format("02 Jan")
}
func (a *app) reader(c *ui.Context) {
	t := a.theme
	m, ok := a.selected()
	panel(c, ui.Column(c)).Fill().Children(func() {
		if !ok {
			ui.Column(c).Fill().Center().Gap(14).Padding(36).Children(func() {
				ui.Box(c).Size(72, 72).Center().Children(func() { ui.Icon(c, icon("logo")).Size(56, 56).TextColor(t.Accent) })
				ui.Text(c, "Select a message").FontSize(22).Bold()
				ui.Row(c).Gap(16).Padding(18, 0).Children(func() {
					ui.Text(c, "Ctrl N  Compose").FontSize(10).TextColor(t.TextMuted)
					ui.Text(c, "Ctrl F  Find mail").FontSize(10).TextColor(t.TextMuted)
				})
			})
			return
		}
		ui.Row(c).FillWidth().Height(48).Shrink(0).Padding(6, 15).Gap(5).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			iconButton(c, "archive", "Archive message", func() { a.bulk(func(m *mailclient.Message) { m.Folder = "Archive" }, true) })
			iconButton(c, "trash", "Move to trash", func() { a.bulk(func(m *mailclient.Message) { m.Folder = "Trash" }, true) })
			iconButton(c, "star", "Toggle star", func() { m.Starred = !m.Starred; a.mutate(m, true) })
			menuButton(c, "Snooze", func(menu *ui.Menu) {
				for _, v := range []struct {
					label string
					d     time.Duration
				}{{"For one hour", time.Hour}, {"Until tomorrow", 24 * time.Hour}, {"For one week", 7 * 24 * time.Hour}} {
					if menu.Item(v.label).Chosen() {
						m.SnoozedUntil = time.Now().Add(v.d)
						a.mutate(m, false)
					}
				}
				if menu.Item("Unsnooze").Chosen() {
					m.SnoozedUntil = time.Time{}
					a.mutate(m, false)
				}
			}).Width(110)
			ui.Box(c).Grow(1)
			menuButton(c, "More", func(menu *ui.Menu) { a.messageMenu(menu, m) }).Width(100)
		})
		ui.Column(c).FillWidth().Padding(20, 24, 14, 24).Gap(14).Children(func() {
			subj := m.Subject
			if subj == "" {
				subj = "(No subject)"
			}
			ui.Text(c, subj).FontSize(23).Bold().MaxLines(3)
			name, address := sender(m.From)
			ui.Row(c).FillWidth().Gap(11).AlignItems(ui.Center).Children(func() {
				avatarBG, avatarText := avatarColors(t, m.From)
				ui.Box(c).Size(38, 38).Radius(13).Background(avatarBG).TextColor(avatarText).Center().Children(func() { ui.Text(c, initials(name)).FontSize(13).Bold() })
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Text(c, name).FontSize(12).Bold()
					ui.Text(c, address).FontSize(10).TextColor(t.TextMuted).Selectable()
				})
				ui.Text(c, m.Date.Local().Format("02 Jan 2006 · 15:04")).FontSize(10).TextColor(t.TextMuted)
			})
			ui.Text(c, "To: "+m.To).FontSize(10).TextColor(t.TextMuted).Selectable().MaxLines(2)
			if m.CC != "" {
				ui.Text(c, "Cc: "+m.CC).FontSize(10).TextColor(t.TextMuted).Selectable()
			}
			ui.Row(c).Gap(5).Wrap().Children(func() {
				if m.Category != "" {
					ui.Text(c, m.Category).FontSize(10).Padding(4, 7).Radius(4).Background(t.Surface)
				}
				for _, tag := range m.Tags {
					color := a.tagColor(tag)
					ui.Text(c, tag).FontSize(10).Padding(4, 7).Radius(4).TextColor(color).Background(color.Alpha(.1))
				}
				menuButton(c, "+ Tag", func(menu *ui.Menu) {
					for _, tag := range a.state.Tags {
						if menu.Item(tag).Chosen() {
							if slices.Contains(m.Tags, tag) {
								m.Tags = slices.DeleteFunc(m.Tags, func(v string) bool { return v == tag })
							} else {
								m.Tags = append(m.Tags, tag)
							}
							a.mutate(m, false)
						}
					}
				})
			})
		})
		ui.Row(c).FillWidth().Padding(0, 24, 8, 24).Gap(8).Children(func() {
			ui.Select(c, &a.bodyMode, []string{"Formatted", "Plain text", "Source", "Conversation"}).Label("Message format").Width(160).OnChange(func() { a.previewKey = ""; a.updatePreview() })
			if m.HTML != "" {
				label := "Remote images blocked"
				if a.prefs.RemoteImages {
					label = "Remote images allowed"
				}
				ui.Text(c, label).FontSize(9).TextColor(t.TextMuted)
			}
		})
		if a.bodyMode == "Formatted" && a.preview != nil && m.HTML != "" {
			ui.WebView(c, a.preview).Grow(1).FillWidth()
		} else {
			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				ui.Column(c).FillWidth().Padding(14, 32, 14, 24).Gap(20).Children(func() {
					if a.bodyMode == "Conversation" {
						for _, item := range a.conversation(m) {
							name, _ := sender(item.From)
							ui.Text(c, name+" · "+item.Date.Format("02 Jan 15:04")).FontSize(11).Bold()
							ui.Text(c, item.Text).FontSize(float32(a.prefs.ReadingSize)).LineHeight(1.6).Selectable().FillWidth()
						}
					} else if a.bodyMode == "Source" {
						ui.Text(c, string(m.Raw)).Font("Consolas").FontSize(11).Selectable().FillWidth()
					} else if m.Text != "" {
						ui.Text(c, m.Text).Font(a.prefs.ReadingFont).FontSize(float32(a.prefs.ReadingSize)).LineHeight(1.7).Selectable().FillWidth()
					} else {
						doc := richtext.NewDocument(richtext.ParseHTML(m.HTML))
						richtext.EditorBase(c, doc, richtext.EditorOptions{ReadOnly: true, Label: "Message body"}).FillWidth().MinHeight(300)
					}
				})
			})
		}
		if len(m.Attachments) > 0 {
			ui.Row(c).FillWidth().Padding(12, 24).Gap(7).Wrap().BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
				for _, at := range m.Attachments {
					action(c, "attachment", fmt.Sprintf("%s · %s", at.Name, fileSize(len(at.Data))), false, func() { a.previewAttachment(at) }).FontSize(10).ContextMenu(func(menu *ui.Menu) {
						if menu.Item("Save attachment").Chosen() {
							a.saveAttachment(at)
						}
						if strings.HasPrefix(at.ContentType, "image/") {
							if menu.Item("Preview image").Chosen() {
								a.previewAttachment(at)
							}
						}
					})
				}
				textButton(c, "Save all").OnClick(func() { a.saveAllAttachments(m.Attachments) })
			})
		}
		ui.Row(c).FillWidth().Shrink(0).Padding(14, 24).Gap(8).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			action(c, "reply", "Reply", true, func() { a.newComposer(&m, "reply") })
			action(c, "reply", "Reply all", false, func() { a.newComposer(&m, "replyall") })
			action(c, "forward", "Forward", false, func() { a.newComposer(&m, "forward") })
		})
	})
}
func (a *app) conversation(m mailclient.Message) []mailclient.Message {
	var ms []mailclient.Message
	k := threadKey(m)
	for _, v := range a.state.Messages {
		if threadKey(v) == k {
			ms = append(ms, v)
		}
	}
	slices.SortFunc(ms, func(x, y mailclient.Message) int { return x.Date.Compare(y.Date) })
	return ms
}
func (a *app) bulkMenu(menu *ui.Menu) {
	if menu.Item("Mark read").Chosen() {
		a.bulk(func(m *mailclient.Message) { m.Read = true }, true)
	}
	if menu.Item("Mark unread").Chosen() {
		a.bulk(func(m *mailclient.Message) { m.Read = false }, true)
	}
	if menu.Item("Star selected").Chosen() {
		a.bulk(func(m *mailclient.Message) { m.Starred = true }, true)
	}
	if menu.Item("Archive selected").Chosen() {
		a.bulk(func(m *mailclient.Message) { m.Folder = "Archive" }, true)
	}
	if menu.Item("Trash selected").Chosen() {
		a.bulk(func(m *mailclient.Message) { m.Folder = "Trash" }, true)
	}
}
func (a *app) messageMenu(menu *ui.Menu, m mailclient.Message) {
	if menu.Item("Reply").Chosen() {
		a.newComposer(&m, "reply")
	}
	if menu.Item("Forward as attachment").Chosen() {
		a.newComposer(&m, "forward-attachment")
	}
	menu.Separator()
	if menu.Item("Toggle read / unread").Chosen() {
		m.Read = !m.Read
		a.mutate(m, true)
	}
	if menu.Item("Toggle importance").Chosen() {
		m.Important = !m.Important
		a.mutate(m, false)
	}
	if menu.Item("Follow up tomorrow").Chosen() {
		m.FollowUp = time.Now().Add(24 * time.Hour)
		a.mutate(m, false)
	}
	if menu.Item("Clear follow-up").Chosen() {
		m.FollowUp = time.Time{}
		a.mutate(m, false)
	}
	categories := append([]string{"Work", "Finance", "Clients", "Personal"}, a.state.Categories...)
	categories = append(categories, "")
	for _, cat := range categories {
		label := cat
		if label == "" {
			label = "Clear category"
		}
		if menu.Item("Category: " + label).Chosen() {
			m.Category = cat
			a.mutate(m, false)
		}
	}
	for _, f := range a.state.Folders[m.AccountID] {
		if menu.Item("Move to " + f).Chosen() {
			m.Folder = f
			a.mutate(m, true)
		}
	}
	menu.Separator()
	if menu.Item("Save as .eml").Chosen() {
		a.exportEML(m)
	}
	if menu.Item("Open in a new window").Chosen() {
		a.openMessageWindow(m)
	}
	if menu.Item("Trust this sender's remote images").Chosen() {
		_, address := sender(m.From)
		address = strings.ToLower(address)
		if !slices.Contains(a.prefs.TrustedSenders, address) {
			a.prefs.TrustedSenders = append(a.prefs.TrustedSenders, address)
			a.savePrefs()
		}
	}
	if m.UID == 0 && m.Folder == "Local archive" {
		if menu.Item("Delete this local archived copy").Chosen() {
			s, id := a.store, m.ID
			a.confirm("Delete local copy permanently?", "This removes the downloaded message and its attachments from Bentomail.", func() {
				a.run("Local copy deleted", func() error { return s.DeleteMessage(id) }, func(err error) {
					if err == nil {
						a.selectedID = ""
					}
				})
			})
		}
	}
	if menu.Item("Print").Disabled(a.print == nil).Chosen() {
		a.printPending = true
		a.previewKey = ""
		a.selectMessage(m.ID)
	}
}
