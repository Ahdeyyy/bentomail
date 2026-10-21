package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"mime"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

type composer struct {
	toTokens, ccTokens, bccTokens              []string
	toEntry, ccEntry, bccEntry                 string
	draft                                      mailclient.Draft
	doc                                        *richtext.Document
	accountPick, schedule, link, font, size    string
	dirty, showCC, attachmentWarn, subjectWarn bool
	handle                                     ui.Handle
}

func (a *app) activeComposer() *composer {
	for _, v := range a.composers {
		if v.draft.ID == a.composeID {
			return v
		}
	}
	return nil
}
func (a *app) newComposer(m *mailclient.Message, mode string) {
	ac := a.account(a.accountID)
	if ac.ID == "" {
		ac = a.account(a.prefs.DefaultAccountID)
	}
	if !ac.Enabled {
		ac = mailclient.Account{}
	}
	if m != nil {
		ac = a.account(m.AccountID)
	}
	if ac.ID == "" {
		for _, candidate := range a.state.Accounts {
			if candidate.Enabled {
				ac = candidate
				break
			}
		}
	}
	d := mailclient.Draft{ID: mailclient.NewID(), AccountID: ac.ID, Status: "draft"}
	d.IdentityID = ac.DefaultIdentityID
	if identity, err := mailclient.SendingIdentity(ac, d.IdentityID); err == nil {
		ac = identity
	}
	body := ""
	if ac.Signature != "" {
		body = "\n\n" + ac.Signature
	}
	if m != nil {
		if strings.HasPrefix(mode, "reply") {
			d.Subject = m.Subject
			if !strings.HasPrefix(strings.ToLower(d.Subject), "re:") {
				d.Subject = "Re: " + d.Subject
			}
			_, addr := sender(m.From)
			d.To = addr
			// Reply-To is retained in parsed raw headers when present.
			if raw, err := mail.ReadMessage(strings.NewReader(string(m.Raw))); err == nil && raw.Header.Get("Reply-To") != "" {
				d.To = raw.Header.Get("Reply-To")
			}
			if canonicalFolder(m.Folder) == "sent" {
				d.To = m.To
			}
			d.InReplyTo = m.MessageID
			d.References = append(append([]string{}, m.References...), m.MessageID)
			if mode == "replyall" {
				seen := map[string]bool{strings.ToLower(ac.Email): true}
				base := a.account(d.AccountID)
				seen[strings.ToLower(base.Email)] = true
				for _, identity := range base.Identities {
					seen[strings.ToLower(identity.Email)] = true
				}
				if list, e := mail.ParseAddressList(d.To); e == nil {
					for _, p := range list {
						seen[strings.ToLower(p.Address)] = true
					}
				}
				for _, list := range []string{m.To, m.CC} {
					if ps, e := mail.ParseAddressList(list); e == nil {
						for _, p := range ps {
							key := strings.ToLower(p.Address)
							if !seen[key] {
								if d.CC != "" {
									d.CC += ", "
								}
								d.CC += p.String()
								seen[key] = true
							}
						}
					}
				}
			}
			body += "\n\nOn " + m.Date.Format("02 Jan 2006 at 15:04") + ", " + m.From + " wrote:\n> " + strings.ReplaceAll(m.Text, "\n", "\n> ")
		} else {
			d.Subject = "Fwd: " + m.Subject
			if mode == "forward-attachment" {
				d.Attachments = []mailclient.Attachment{{Name: "forwarded-message.eml", ContentType: "message/rfc822", Data: m.Raw}}
			} else {
				d.Attachments = append([]mailclient.Attachment{}, m.Attachments...)
				body += "\n\n---------- Forwarded message ----------\nFrom: " + m.From + "\nDate: " + m.Date.Format(time.RFC1123) + "\nSubject: " + m.Subject + "\nTo: " + m.To + "\n\n" + m.Text
			}
		}
	}
	co := &composer{draft: d, doc: richtext.NewDocument(richtext.NewText(body)), dirty: true, showCC: d.CC != "", font: "Default", size: "14"}
	co.accountPick = a.identityLabel(d.AccountID, d.IdentityID)
	co.doc.SetSelection(0, 0)
	a.composers = append(a.composers, co)
	co.initRecipients()
	a.composeID = d.ID
	a.page = "compose"
}
func (a *app) openDraft(d mailclient.Draft) {
	for _, co := range a.composers {
		if co.draft.ID == d.ID {
			a.composeID = d.ID
			a.page = "compose"
			return
		}
	}
	ac := a.account(d.AccountID)
	if identity, err := mailclient.SendingIdentity(ac, d.IdentityID); err == nil {
		ac = identity
	}
	text := richtext.NewText(d.Text)
	if d.HTML != "" {
		text = richtext.ParseHTML(d.HTML)
	}
	co := &composer{draft: d, doc: richtext.NewDocument(text), accountPick: a.identityLabel(d.AccountID, d.IdentityID), showCC: d.CC != "" || d.BCC != "", font: "Default", size: "14"}
	a.composers = append(a.composers, co)
	co.initRecipients()
	a.composeID = d.ID
	a.page = "compose"
}
func (co *composer) snapshot() mailclient.Draft {
	d := co.draft
	d.To = joinRecipients(co.toTokens, co.toEntry)
	d.CC = joinRecipients(co.ccTokens, co.ccEntry)
	d.BCC = joinRecipients(co.bccTokens, co.bccEntry)
	d.Text = co.doc.Text().String()
	d.HTML = ""
	if !d.Plain {
		d.HTML = co.doc.Text().HTML()
	}
	d.Updated = time.Now()
	return d
}
func (a *app) saveOpenDrafts() {
	for _, co := range a.composers {
		if !co.dirty {
			continue
		}
		d, s := co.snapshot(), a.store
		co.dirty = false
		a.run("Draft saved", func() error { _, e := s.SaveDraft(d); return e }, func(err error) {
			if err != nil {
				co.dirty = true
			}
		})
	}
}
func (a *app) closeComposer(id string, discard bool) {
	var co *composer
	for _, v := range a.composers {
		if v.draft.ID == id {
			co = v
			break
		}
	}
	if co == nil {
		return
	}
	s := a.store
	if discard {
		a.run("Draft discarded", func() error { return s.DeleteDraft(id) }, func(err error) {
			if err == nil {
				a.removeComposer(id)
			}
		})
		return
	}
	d, version := co.snapshot(), co.doc.Version()
	co.dirty = false
	a.run("Draft saved", func() error { _, err := s.SaveDraft(d); return err }, func(err error) {
		if err != nil {
			co.dirty = true
			return
		}
		if co.doc.Version() != version || co.dirty {
			a.notice("Draft changed while saving; kept open.", false)
			return
		}
		a.removeComposer(id)
	})
}
func (a *app) removeComposer(id string) {
	a.composers = slices.DeleteFunc(a.composers, func(c *composer) bool { return c.draft.ID == id })
	if len(a.composers) > 0 {
		a.composeID = a.composers[len(a.composers)-1].draft.ID
	} else {
		a.composeID = ""
		a.page = "mail"
	}
}
func (a *app) composeView(c *ui.Context) {
	co := a.activeComposer()
	if co == nil {
		ui.Text(c, "Open a draft or compose a new message.").Padding(30)
		return
	}
	t := a.theme
	ui.Column(c).Grow(1).FillWidth().MinHeight(0).Padding(0, 20, 16, 10).Gap(10).Children(func() {
		ui.Row(c).FillWidth().Gap(6).Wrap().Children(func() {
			for _, tab := range a.composers {
				c.Key(tab.draft.ID)
				label := tab.draft.Subject
				if label == "" {
					label = "New message"
				}
				if len([]rune(label)) > 24 {
					label = string([]rune(label)[:24]) + "…"
				}
				b := ui.Button(c, label).FontSize(11).OnClick(func() { a.composeID = tab.draft.ID })
				if tab.draft.ID == co.draft.ID {
					b.Background(t.Surface.Mix(t.Accent, .15)).TextColor(t.Accent)
				}
			}
			iconButton(c, "plus", "New draft", func() { a.newComposer(nil, "new") })
		})
		panel(c, ui.Column(c)).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Row(c).FillWidth().Padding(15, 20).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "New message").FontSize(19).Bold().Grow(1)
				ui.Text(c, "Drafts save automatically").FontSize(10).TextColor(t.TextMuted)
				iconButton(c, "close", "Save and close draft", func() { a.closeComposer(co.draft.ID, false) })
			})
			ui.Column(c).FillWidth().Padding(0, 20, 10, 20).Gap(7).Children(func() {
				opts := []string{}
				for _, ac := range a.state.Accounts {
					if ac.Enabled {
						opts = append(opts, ac.Name+" <"+ac.Email+">")
						for _, identity := range ac.Identities {
							opts = append(opts, identity.Name+" <"+identity.Email+"> · "+ac.Email)
						}
					}
				}
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "From").Width(50).FontSize(11).TextColor(t.TextMuted)
					selectInput(c, &co.accountPick, opts).Label("Sending identity").Grow(1).OnChange(func() {
						for _, ac := range a.state.Accounts {
							if co.accountPick == ac.Name+" <"+ac.Email+">" {
								co.draft.AccountID = ac.ID
								co.draft.IdentityID = ""
								co.dirty = true
							}
							for _, identity := range ac.Identities {
								if co.accountPick == identity.Name+" <"+identity.Email+"> · "+ac.Email {
									co.draft.AccountID = ac.ID
									co.draft.IdentityID = identity.ID
									co.dirty = true
								}
							}
						}
					})
					textButton(c, "Cc / Bcc").FontSize(10).OnClick(func() { co.showCC = !co.showCC })
				})
				a.recipientField(c, co, "To", &co.draft.To)
				if co.showCC {
					a.recipientField(c, co, "Cc", &co.draft.CC)
					a.recipientField(c, co, "Bcc", &co.draft.BCC)
				}
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Subject").Width(50).FontSize(11).TextColor(t.TextMuted)
					ui.TextInput(c, &co.draft.Subject).Label("Subject").Placeholder("Subject").Grow(1).OnChange(func() { co.dirty = true; co.subjectWarn = false })
				})
			})
			ui.Row(c).FillWidth().Padding(9, 20).Gap(5).Wrap().BorderWidth(1, 0, 1, 0).BorderColor(t.Border).Children(func() {
				for _, f := range []struct {
					label string
					fn    func()
				}{{"B", co.doc.ToggleBold}, {"I", co.doc.ToggleItalic}, {"U", co.doc.ToggleUnderline}, {"S", co.doc.ToggleStrikethrough}, {"H1", func() { co.doc.SetHeading(1) }}, {"• List", func() { co.doc.ToggleList(richtext.Bullets) }}, {"1. List", func() { co.doc.ToggleList(richtext.Numbers) }}, {"Quote", co.doc.ToggleQuote}, {"Indent", func() { co.doc.Indent(1) }}, {"Undo", co.doc.Undo}, {"Redo", co.doc.Redo}} {
					c.Key(f.label)
					textButton(c, f.label).FontSize(11).Padding(5, 7).Disabled(co.draft.Plain).OnClick(func() { f.fn(); co.dirty = true; co.handle.Focus(c) })
				}
				menuButton(c, "Style", func(menu *ui.Menu) {
					for label, align := range map[string]ui.Align{"Align left": ui.Start, "Align center": ui.Center, "Align right": ui.End} {
						if menu.Item(label).Chosen() {
							co.doc.SetAlign(align)
							co.dirty = true
						}
					}
					for _, sz := range []float32{12, 14, 16, 18, 24} {
						if menu.Item(fmt.Sprintf("%g px", sz)).Chosen() {
							co.doc.Format(func(at *richtext.Attributes) { at.Size = sz })
							co.dirty = true
						}
					}
					for _, font := range []string{"Martian Grotesk", "Arial", "Georgia", "Consolas"} {
						if menu.Item(font).Chosen() {
							co.doc.Format(func(at *richtext.Attributes) { at.Font = font })
							co.dirty = true
						}
					}
					for _, color := range []string{"#20293e", "#5260d8", "#b91c1c", "#15803d"} {
						if menu.Item(color).Chosen() {
							co.doc.Format(func(at *richtext.Attributes) { at.Color = ui.Hex(color) })
							co.dirty = true
						}
					}
					if menu.Item("Clear formatting").Chosen() {
						co.doc.Format(func(at *richtext.Attributes) { *at = richtext.Attributes{} })
						co.doc.SetHeading(0)
						co.dirty = true
					}
				})
				ui.Checkbox(c, &co.draft.Plain, "Plain text").OnChange(func() { co.dirty = true })
			})
			c.Key("body-" + co.draft.ID)
			ui.Column(c).Grow(1).FillWidth().Padding(18, 24).Children(func() {
				editor := richtext.EditorBase(c, co.doc, richtext.EditorOptions{Label: "Message body", Placeholder: "Write something thoughtful…", NoMarkdown: co.draft.Plain}).Bind(&co.handle).Fill().OnChange(func() { co.dirty = true; co.attachmentWarn = false })
				if files := editor.DroppedFiles(); len(files) > 0 {
					a.attachFiles(co, files, false)
				}
			})
			ui.Row(c).FillWidth().Padding(8, 20).Gap(8).Children(func() {
				ui.TextInput(c, &co.link).Label("Link URL").Placeholder("https://…").Grow(1)
				textButton(c, "Insert link").OnClick(func() {
					if ui.SafeToOpen(co.link) {
						co.doc.SetLink(co.link)
						co.dirty = true
						co.handle.Focus(c)
					}
				})
				menuButton(c, "Templates", func(menu *ui.Menu) {
					for name, body := range a.state.Templates {
						if menu.Item(name).Chosen() {
							co.doc.Edit(func(e *richtext.Edit) {
								anchor, caret := e.Selection()
								e.Replace(min(anchor, caret), max(anchor, caret), richtext.NewText(body))
							})
							co.dirty = true
						}
					}
					if menu.Item("Save as template").Chosen() {
						name := co.draft.Subject
						if name == "" {
							name = "Untitled template"
						}
						body, s := co.doc.Text().String(), a.store
						a.run("Template saved", func() error { return s.SaveTemplate(name, body) })
					}
				})
			})
			if len(co.draft.Attachments) > 0 {
				ui.Row(c).FillWidth().Padding(8, 20).Gap(7).Wrap().Children(func() {
					for i, at := range co.draft.Attachments {
						c.Key(fmt.Sprintf("attachment-%d", i))
						ui.Button(c, at.Name+" · "+fileSize(len(at.Data))+" ×").FontSize(10).OnClick(func() { co.draft.Attachments = slices.Delete(co.draft.Attachments, i, i+1); co.dirty = true })
					}
				})
			}
			if co.attachmentWarn {
				ui.Text(c, "You mentioned an attachment. Attach a file, or press Send again to continue.").TextColor(t.Warning).FontSize(11).Padding(5, 20)
			}
			if co.subjectWarn {
				ui.Text(c, "No subject entered. Press Send again to send without one.").TextColor(t.Warning).FontSize(11).Padding(5, 20)
			}
			ui.Row(c).FillWidth().Padding(14, 20).Gap(8).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
				action(c, "send", "Send", true, func() { a.queueComposer(co) }).Disabled(len(a.state.Accounts) == 0)
				iconButton(c, "attachment", "Attach files", func() { a.pickAttachments(co, false) })
				textButton(c, "Inline image").FontSize(10).OnClick(func() { a.pickAttachments(co, true) })
				ui.TextInput(c, &co.schedule).Label("Scheduled send time").Placeholder("YYYY-MM-DD HH:MM (optional)").Width(300).Shrink(0)
				ui.Box(c).Grow(1)
				iconButton(c, "trash", "Discard draft", func() {
					a.confirm("Discard this draft?", "The message and its attachments will be removed.", func() { a.closeComposer(co.draft.ID, true) })
				})
			})
		})
	})
}
func (a *app) recipientField(c *ui.Context, co *composer, label string, value *string) {
	tokens, entry := &co.toTokens, &co.toEntry
	switch label {
	case "Cc":
		tokens, entry = &co.ccTokens, &co.ccEntry
	case "Bcc":
		tokens, entry = &co.bccTokens, &co.bccEntry
	}
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).Width(50).FontSize(11).TextColor(a.theme.TextMuted)
		ui.Row(c).Grow(1).MinWidth(0).Gap(5).Wrap().Children(func() {
			for i, address := range *tokens {
				c.Key(label + address)
				ui.Button(c, address+" ×").Label("Remove " + label + " recipient " + address).FontSize(10).OnClick(func() { *tokens = slices.Delete(*tokens, i, i+1); co.dirty = true })
			}
			recipientAutocomplete(c, entry, a.state.Contacts).Label(label + " recipients").Placeholder("name@company.com").Grow(1).MinWidth(180).OnChange(func() { co.dirty = true }).OnSubmit(func() { co.commitRecipient(tokens, entry) })
		})
		menuButton(c, "Contacts", func(menu *ui.Menu) {
			for _, addr := range a.state.Contacts {
				if menu.Item(addr).Chosen() {
					*tokens = append(*tokens, addr)
					co.dirty = true
				}
			}
		})
	})
}

func splitRecipients(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parsed, err := mail.ParseAddressList(raw)
	if err != nil {
		return []string{raw}
	}
	out := make([]string, 0, len(parsed))
	for _, addr := range parsed {
		out = append(out, addr.String())
	}
	return out
}

func (a *app) identityLabel(accountID, identityID string) string {
	base := a.account(accountID)
	identity, err := mailclient.SendingIdentity(base, identityID)
	if err != nil {
		return "Choose a sending identity"
	}
	label := identity.Name + " <" + identity.Email + ">"
	if identityID != "" {
		label += " · " + base.Email
	}
	return label
}
func (co *composer) initRecipients() {
	co.toTokens = splitRecipients(co.draft.To)
	co.ccTokens = splitRecipients(co.draft.CC)
	co.bccTokens = splitRecipients(co.draft.BCC)
}
func joinRecipients(tokens []string, entry string) string {
	parts := append([]string{}, tokens...)
	if strings.TrimSpace(entry) != "" {
		parts = append(parts, strings.TrimSpace(entry))
	}
	return strings.Join(parts, ", ")
}
func (co *composer) commitRecipient(tokens *[]string, entry *string) {
	if strings.TrimSpace(*entry) == "" {
		return
	}
	if parsed, err := mail.ParseAddressList(*entry); err == nil {
		for _, addr := range parsed {
			*tokens = append(*tokens, addr.String())
		}
		*entry = ""
		co.dirty = true
	}
}
func (a *app) queueComposer(co *composer) {
	d := co.snapshot()
	ac := a.account(d.AccountID)
	if ac.ID == "" {
		a.notice("Choose a sending account first.", true)
		return
	}
	if _, _, err := mailclient.BuildMIME(ac, d); err != nil {
		a.notice(err.Error(), true)
		return
	}
	if d.Subject == "" && !co.subjectWarn {
		co.subjectWarn = true
		return
	}
	if strings.Contains(strings.ToLower(d.Text), "attach") && len(d.Attachments) == 0 && !co.attachmentWarn {
		co.attachmentWarn = true
		return
	}
	d.Status = "queued"
	d.SendAt = time.Now().Add(time.Duration(max(0, a.prefs.SendDelay)) * time.Second)
	if co.schedule != "" {
		when, err := time.ParseInLocation("2006-01-02 15:04", co.schedule, time.Local)
		if err != nil || when.Before(time.Now()) {
			a.notice("Enter a future send time as YYYY-MM-DD HH:MM.", true)
			return
		}
		d.SendAt = when
	}
	s := a.store
	co.dirty = false
	a.run("Message queued — open Outbox to undo", func() error { _, err := s.SaveDraft(d); return err }, func(err error) {
		if err != nil {
			co.dirty = true
			return
		}
		a.composers = slices.DeleteFunc(a.composers, func(c *composer) bool { return c.draft.ID == d.ID })
		a.navigate("outbox")
	})
}
func (a *app) sendQueued(d mailclient.Draft) {
	a.sending[d.ID] = true
	e, ctx := a.engine, a.ctx
	a.network("Send completed", func() error {
		sendctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return e.Send(sendctx, d)
	}, func(err error) {
		delete(a.sending, d.ID)
		if err == nil {
			a.notify("Message sent", d.Subject)
			a.syncMail()
		}
	})
}
func (a *app) draftsView(c *ui.Context) {
	panel(c, ui.Scroll(c)).Fill().Children(func() {
		ui.Column(c).FillWidth().Padding(16, 24, 16, 16).Gap(10).Children(func() {
			count := 0
			for _, d := range a.state.Drafts {
				if a.accountID != "" && d.AccountID != a.accountID {
					continue
				}
				queued := d.Status != "" && d.Status != "draft"
				if (a.folder == "outbox") != queued || d.Status == "sent" {
					continue
				}
				count++
				c.Key(d.ID)
				frost(ui.Column(c), a.theme.Background).FillWidth().Padding(16).Gap(8).Radius(9).Children(func() {
					subj := d.Subject
					if subj == "" {
						subj = "(No subject)"
					}
					ui.Text(c, subj).FontSize(14).Bold()
					ui.Text(c, "To: "+d.To).FontSize(11).TextColor(a.theme.TextMuted)
					state := d.Status
					if state == "" {
						state = "draft"
					}
					if !d.SendAt.IsZero() {
						state += " · " + d.SendAt.Local().Format("02 Jan 15:04:05")
					}
					ui.Text(c, state).FontSize(10).TextColor(a.theme.Accent)
					if d.Error != "" {
						ui.Text(c, d.Error).FontSize(11).TextColor(a.theme.Danger)
					}
					ui.Row(c).Gap(8).Children(func() {
						textButton(c, "Edit draft").Disabled(a.sending[d.ID]).OnClick(func() {
							if queued {
								s, edit := a.store, d
								edit.Status = "draft"
								edit.SendAt = time.Time{}
								a.run("Send cancelled", func() error { _, e := s.SaveDraft(edit); return e }, func(err error) {
									if err == nil {
										a.openDraft(edit)
									}
								})
							} else {
								a.openDraft(d)
							}
						})
						if queued && !a.sending[d.ID] {
							textButton(c, "Send now").OnClick(func() { a.sendQueued(d) })
						}
						textButton(c, "Discard").Disabled(a.sending[d.ID]).OnClick(func() {
							s, id := a.store, d.ID
							a.confirm("Discard message?", "This removes the local draft.", func() { a.run("Draft discarded", func() error { return s.DeleteDraft(id) }) })
						})
					})
				})
			}
			if count == 0 {
				ui.Text(c, "No messages").TextColor(a.theme.TextMuted).Padding(30)
			}
		})
	})
}
func (a *app) pickAttachments(co *composer, inline bool) {
	dispatch := a.dispatch
	opts := mygo.OpenDialogOptions{Title: "Attach files", Multiple: true, Parent: a.window}
	if inline {
		opts.Filters = []mygo.FileFilter{{Name: "Images", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}}}
	}
	go func() {
		paths, err := mygo.Dialog.Open(opts)
		if dispatch != nil {
			dispatch(func() {
				if err != nil {
					a.notice(err.Error(), true)
				} else if !a.closed {
					a.attachFiles(co, paths, inline)
				}
			})
		}
	}()
}
func (a *app) attachFiles(co *composer, paths []string, inline bool) {
	// Reading files happens on the worker. The immutable result is applied on the UI thread.
	var attachments []mailclient.Attachment
	a.run("Files attached", func() error {
		for _, path := range paths {
			st, err := os.Stat(path)
			if err != nil {
				return err
			}
			if st.Size() > 50<<20 {
				return fmt.Errorf("%s exceeds the 50 MB attachment limit", filepath.Base(path))
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			ct := mime.TypeByExtension(filepath.Ext(path))
			if ct == "" {
				ct = "application/octet-stream"
			}
			attachments = append(attachments, mailclient.Attachment{Name: filepath.Base(path), ContentType: ct, Data: data})
		}
		return nil
	}, func(err error) {
		if err != nil {
			return
		}
		if !slices.Contains(a.composers, co) {
			return
		}
		for _, at := range attachments {
			if inline && strings.HasPrefix(at.ContentType, "image/") {
				markup := `<p><img alt="` + html.EscapeString(at.Name) + `" src="data:` + at.ContentType + `;base64,` + base64.StdEncoding.EncodeToString(at.Data) + `"></p>`
				co.doc.Edit(func(e *richtext.Edit) {
					anchor, caret := e.Selection()
					e.Replace(min(anchor, caret), max(anchor, caret), richtext.ParseHTML(markup))
				})
			} else {
				co.draft.Attachments = append(co.draft.Attachments, at)
			}
		}
		co.dirty = true
	})
}
