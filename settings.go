package main

import (
	"context"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (a *app) resetAccountForm() {
	a.accountForm = mailclient.Account{IMAPPort: 993, SMTPPort: 587, IMAPTLS: "TLS", SMTPTLS: "STARTTLS", AuthMode: "password", Enabled: true}
	a.imapPort = "993"
	a.smtpPort = "587"
	a.password = ""
	a.accountError = ""
}
func (a *app) settingsView(c *ui.Context) {
	t := a.theme
	ui.Column(c).Grow(1).FillWidth().MinHeight(0).Padding(0, 12, 12, 10).Gap(10).Children(func() {
		ui.Text(c, "Settings").FontSize(25).Bold()
		ui.Row(c).Gap(5).Wrap().Children(func() {
			for _, tab := range []string{"Accounts", "Appearance", "Organization", "Contacts", "Notifications", "Storage", "Shortcuts"} {
				b := textButton(c, tab).FontSize(11).OnClick(func() { a.settingsTab = tab })
				if a.settingsTab == tab {
					b.Background(t.Accent.Alpha(.12)).TextColor(t.Accent)
				}
			}
		})
		panel(c, ui.Scroll(c)).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Start).Padding(12, 24, 12, 12).Gap(12).Children(func() {
				switch a.settingsTab {
				case "Contacts":
					a.contactsSettings(c)
				case "Accounts":
					a.accountsSettings(c)
				case "Appearance":
					a.appearanceSettings(c)
				case "Organization":
					a.organizationSettings(c)
				case "Notifications":
					a.notificationSettings(c)
				case "Storage":
					a.storageSettings(c)
				default:
					a.shortcutsSettings(c)
				}
			})
		})
	})
}
func field(c *ui.Context, label string, value *string, placeholder string) {
	fieldWidth(c, label, value, placeholder, 480)
}
func fieldWidth(c *ui.Context, label string, value *string, placeholder string, width float32) {
	ui.Column(c).FillWidth().MaxWidth(width).Gap(5).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(500)
		ui.TextInput(c, value).Label(label).Placeholder(placeholder).FillWidth()
	})
}
func note(c *ui.Context, s string) {
	ui.Text(c, s).FontSize(11).TextColor(c.Theme().TextMuted).LineHeight(1.6).FillWidth()
}
func section(c *ui.Context, label string) { ui.Text(c, label).FontSize(17).Bold().Padding(10, 0, 2, 0) }
func (a *app) accountsSettings(c *ui.Context) {
	section(c, "Connected accounts")
	for _, ac := range a.state.Accounts {
		c.Key(ac.ID)
		ui.Row(c).FillWidth().Padding(12).Gap(10).Radius(9).Background(a.theme.Background).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon("mail")).Size(23, 23).TextColor(a.theme.Accent)
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				ui.Text(c, ac.Name).FontSize(12).Bold()
				ui.Text(c, ac.Email).FontSize(10).TextColor(a.theme.TextMuted)
			})
			textButton(c, "Edit").OnClick(func() {
				a.accountForm = ac
				a.imapPort = strconv.Itoa(ac.IMAPPort)
				a.smtpPort = strconv.Itoa(ac.SMTPPort)
				a.password = ""
			})
			textButton(c, "Use by default").OnClick(func() { a.prefs.DefaultAccountID = ac.ID; a.savePrefs() })
			textButton(c, "Remove").OnClick(func() {
				s, id := a.store, ac.ID
				a.confirm("Remove account?", "Downloaded mail will be retained as a local archive. Credentials will be removed.", func() { a.run("Account removed", func() error { return s.DeleteAccount(id, true) }) })
			})
		})
	}
	action(c, "plus", "New account", false, a.resetAccountForm)
	section(c, "Identity")
	ui.Row(c).Gap(8).Children(func() {
		textButton(c, "Google preset").OnClick(func() {
			a.accountForm.IMAPHost = "imap.gmail.com"
			a.accountForm.SMTPHost = "smtp.gmail.com"
			a.accountForm.IMAPTLS = "TLS"
			a.accountForm.SMTPTLS = "STARTTLS"
			a.imapPort = "993"
			a.smtpPort = "587"
			a.accountForm.AuthMode = "oauth2"
			a.oauthProvider = "Google"
		})
		textButton(c, "Microsoft preset").OnClick(func() {
			a.accountForm.IMAPHost = "outlook.office365.com"
			a.accountForm.SMTPHost = "smtp.office365.com"
			a.accountForm.IMAPTLS = "TLS"
			a.accountForm.SMTPTLS = "STARTTLS"
			a.imapPort = "993"
			a.smtpPort = "587"
			a.accountForm.AuthMode = "oauth2"
			a.oauthProvider = "Microsoft"
		})
	})
	note(c, "The name recipients see on outgoing mail.")
	field(c, "Display name", &a.accountForm.Name, "Alex Morgan")
	menuButton(c, "Account color", func(menu *ui.Menu) {
		for name, color := range map[string]string{"Indigo": "#5260d8", "Teal": "#188980", "Rose": "#be456d", "Amber": "#ac7217", "Violet": "#8258cf"} {
			if menu.Item(name).Chosen() {
				a.accountForm.Color = color
			}
		}
	}).Width(160)
	field(c, "Email address", &a.accountForm.Email, "alex@company.com")
	field(c, "Reply-To (optional)", &a.accountForm.ReplyTo, "Replies go here when different from your email")
	ui.Text(c, "Signature").FontSize(11).Bold()
	ui.TextArea(c, &a.accountForm.Signature).Label("Signature").Height(85).FillWidth().MaxWidth(600)
	ui.Checkbox(c, &a.accountForm.Enabled, "Enable this account")
	menuButton(c, "Default sending identity", func(menu *ui.Menu) {
		if menu.Item(a.accountForm.Name + " (primary)").Chosen() {
			a.accountForm.DefaultIdentityID = ""
		}
		for _, identity := range a.accountForm.Identities {
			if menu.Item(identity.Name + " <" + identity.Email + ">").Chosen() {
				a.accountForm.DefaultIdentityID = identity.ID
			}
		}
	}).Width(240)
	section(c, "Incoming mail · IMAP")
	field(c, "IMAP server", &a.accountForm.IMAPHost, "imap.company.com")
	ui.Row(c).FillWidth().MaxWidth(480).Gap(16).Children(func() {
		ui.Column(c).Width(120).Children(func() { field(c, "IMAP port", &a.imapPort, "993") })
		ui.Column(c).Width(200).Gap(5).Children(func() {
			ui.Text(c, "IMAP security").FontSize(11)
			ui.Select(c, &a.accountForm.IMAPTLS, []string{"TLS", "STARTTLS", "None"}).Label("IMAP security").FillWidth()
		})
	})
	field(c, "IMAP username", &a.accountForm.Username, "Usually your email address")
	section(c, "Outgoing mail · SMTP")
	field(c, "SMTP server", &a.accountForm.SMTPHost, "smtp.company.com")
	ui.Row(c).FillWidth().MaxWidth(480).Gap(16).Children(func() {
		ui.Column(c).Width(120).Children(func() { field(c, "SMTP port", &a.smtpPort, "587") })
		ui.Column(c).Width(200).Gap(5).Children(func() {
			ui.Text(c, "SMTP security").FontSize(11)
			ui.Select(c, &a.accountForm.SMTPTLS, []string{"TLS", "STARTTLS", "None"}).Label("SMTP security").FillWidth()
		})
	})
	field(c, "SMTP username", &a.accountForm.SMTPUsername, "Leave blank to use the IMAP username")
	ui.Select(c, &a.accountForm.AuthMode, []string{"password", "oauth2"}).Label("Authentication method").Width(200)
	label := "Password or app password"
	if a.accountForm.AuthMode == "oauth2" {
		label = "OAuth access token"
	}
	ui.Text(c, label).FontSize(11).Bold()
	ui.TextInput(c, &a.password).Password().Label(label).Placeholder("Leave blank to keep the saved credential").FillWidth().MaxWidth(480)
	note(c, "Credentials are protected for your Windows user. Unencrypted connections are only allowed to loopback test servers. OAuth access tokens are supported; interactive provider setup is configured separately.")
	if a.accountError != "" {
		ui.Text(c, a.accountError).TextColor(a.theme.Danger).FontSize(12)
	}
	ui.Row(c).Gap(8).Children(func() {
		action(c, "check", "Save account", true, func() {
			ac, ok := a.validAccount()
			if !ok {
				return
			}
			pw, s := a.password, a.store
			a.run("Account saved", func() error { return s.SaveAccount(ac, pw) }, func(err error) {
				if err == nil {
					a.password = ""
					a.oauthProvider = ac.OAuthProvider
					a.oauthClientID = ac.OAuthClientID
					a.accountForm = ac
					a.accountID = ac.ID
					a.updateAccountPick()
				}
			})
		})
		action(c, "sync", "Test connection", false, func() {
			ac, ok := a.validAccount()
			if !ok {
				return
			}
			pw, e, ctx := a.password, a.engine, a.ctx
			a.run("Connection test successful", func() error { return e.TestAccount(ctx, ac, pw) })
		})
	})
	if a.accountForm.AuthMode == "oauth2" {
		section(c, "Sign in with your provider")
		if a.oauthProvider == "" {
			a.oauthProvider = "Microsoft"
		}
		ui.Select(c, &a.oauthProvider, []string{"Microsoft", "Google"}).Label("OAuth provider").Width(240)
		field(c, "OAuth desktop client ID", &a.oauthClientID, "Application ID from your provider registration")
		ui.Text(c, "Desktop client secret (Google, if issued)").FontSize(11)
		ui.TextInput(c, &a.oauthClientSecret).Password().Label("OAuth desktop client secret").FillWidth().MaxWidth(480)
		note(c, "Use a registered desktop/public OAuth application. Sign-in opens your browser and uses a temporary loopback redirect with PKCE. Provider consent and workplace policies still apply.")
		action(c, "shield", "Sign in securely", true, func() {
			ac, ok := a.validAccount()
			if !ok {
				return
			}
			config := mailclient.OAuthConfig{Provider: a.oauthProvider, ClientID: a.oauthClientID, ClientSecret: a.oauthClientSecret}
			s, ctx := a.store, a.ctx
			a.run("Provider sign-in completed", func() error {
				ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
				defer cancel()
				return s.Authorize(ctx, ac, config, mygo.Shell.OpenExternal)
			}, func(err error) {
				a.oauthClientSecret = ""
				if err == nil {
					a.accountForm = a.account(ac.ID)
					a.password = ""
				}
			})
		})
	}
	section(c, "Additional sending identities")
	note(c, "Add aliases authorized by your mail provider. Each can have its own display name, reply address, and signature.")
	for _, identity := range a.accountForm.Identities {
		ui.Text(c, identity.Name+" <"+identity.Email+">").FontSize(11)
	}
	field(c, "Alias display name", &a.aliasForm.Name, "Alex · Acme Studio")
	field(c, "Alias email", &a.aliasForm.Email, "hello@company.com")
	field(c, "Alias Reply-To", &a.aliasForm.ReplyTo, "")
	field(c, "Alias signature", &a.aliasForm.Signature, "")
	textButton(c, "Add identity to account").OnClick(func() {
		if _, err := mail.ParseAddress(a.aliasForm.Email); err != nil || strings.TrimSpace(a.aliasForm.Name) == "" {
			a.notice("Enter a valid alias address and display name.", true)
			return
		}
		a.aliasForm.ID = mailclient.NewID()
		a.accountForm.Identities = append(a.accountForm.Identities, a.aliasForm)
		a.aliasForm = mailclient.Identity{}
		a.notice("Identity added. Save the account to keep it.", false)
	})
}
func (a *app) validAccount() (mailclient.Account, bool) {
	ac := a.accountForm
	ac.Name = strings.TrimSpace(ac.Name)
	ac.Email = strings.TrimSpace(ac.Email)
	ac.IMAPHost = strings.TrimSpace(ac.IMAPHost)
	ac.SMTPHost = strings.TrimSpace(ac.SMTPHost)
	fail := func(s string) (mailclient.Account, bool) { a.accountError = s; return ac, false }
	if ac.Name == "" {
		return fail("Enter a display name.")
	}
	p, err := mail.ParseAddress(ac.Email)
	if err != nil || p.Address != ac.Email {
		return fail("Enter a valid email address without a display name.")
	}
	if ac.ReplyTo != "" {
		if _, e := mail.ParseAddress(ac.ReplyTo); e != nil {
			return fail("Enter a valid Reply-To address.")
		}
	}
	if ac.IMAPHost == "" || ac.SMTPHost == "" {
		return fail("Enter both mail server hostnames.")
	}
	ac.IMAPPort, err = strconv.Atoi(a.imapPort)
	if err != nil || ac.IMAPPort < 1 || ac.IMAPPort > 65535 {
		return fail("IMAP port must be between 1 and 65535.")
	}
	ac.SMTPPort, err = strconv.Atoi(a.smtpPort)
	if err != nil || ac.SMTPPort < 1 || ac.SMTPPort > 65535 {
		return fail("SMTP port must be between 1 and 65535.")
	}
	if ac.Username == "" {
		ac.Username = ac.Email
	}
	if ac.ID == "" {
		ac.ID = mailclient.NewID()
	}
	a.accountError = ""
	return ac, true
}
func (a *app) appearanceSettings(c *ui.Context) {
	section(c, "Appearance")
	ui.Checkbox(c, &a.prefs.Dark, "Dark appearance").FillWidth().OnChange(a.savePrefs)
	ui.Text(c, "Accent color").FontSize(11)
	ui.Select(c, &a.prefs.Accent, []string{"Indigo", "Teal", "Rose", "Amber", "Violet"}).Label("Accent color").Width(240).OnChange(a.savePrefs)
	ui.Text(c, "Message density").FontSize(11)
	ui.Select(c, &a.prefs.Density, []string{"Comfortable", "Compact"}).Label("Message density").Width(240).OnChange(a.savePrefs)
	ui.Text(c, "Reading pane").FontSize(11)
	ui.Select(c, &a.prefs.Layout, []string{"Right", "Bottom", "Hidden"}).Label("Reading pane").Width(240).OnChange(a.savePrefs)
	section(c, "Reading")
	ui.Text(c, "Reading font").FontSize(11)
	ui.Select(c, &a.prefs.ReadingFont, []string{"Martian Grotesk", "Arial", "Georgia", "Consolas"}).Label("Reading font").Width(240).OnChange(a.savePrefs)
	ui.Text(c, "Mark messages read").FontSize(11)
	ui.Select(c, &a.prefs.ReadMode, []string{"Immediately", "After 3 seconds", "Manually"}).Label("Mark messages read").Width(240).OnChange(a.savePrefs)
	ui.Checkbox(c, &a.prefs.HideAvatars, "Hide sender avatars").OnChange(a.savePrefs)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Textf(c, "Reading font size: %d", a.prefs.ReadingSize).Width(200)
		textButton(c, "Smaller").OnClick(func() { a.prefs.ReadingSize = max(11, a.prefs.ReadingSize-1); a.savePrefs() })
		textButton(c, "Larger").OnClick(func() { a.prefs.ReadingSize = min(28, a.prefs.ReadingSize+1); a.savePrefs() })
	})
	ui.Row(c).Gap(8).Children(func() {
		ui.Textf(c, "Preview lines: %d", a.prefs.PreviewLines).Width(200)
		textButton(c, "Fewer").OnClick(func() { a.prefs.PreviewLines = max(0, a.prefs.PreviewLines-1); a.savePrefs() })
		textButton(c, "More").OnClick(func() { a.prefs.PreviewLines = min(3, a.prefs.PreviewLines+1); a.savePrefs() })
	})
	note(c, "Drag the dividers to resize the sidebar and message list. Display scaling, reduced motion, and high contrast follow system settings.")
	action(c, "check", "Save pane sizes", false, a.savePrefs)
}
func (a *app) organizationSettings(c *ui.Context) {
	section(c, "Tags")
	field(c, "Tag name", &a.tagInput, "Project, client, or topic")
	action(c, "plus", "Create tag", false, func() {
		tag := strings.TrimSpace(a.tagInput)
		if tag == "" {
			return
		}
		tags := append([]string{}, a.state.Tags...)
		for _, v := range tags {
			if strings.EqualFold(v, tag) {
				return
			}
		}
		tags = append(tags, tag)
		s := a.store
		a.run("Tag created", func() error { return s.SaveTags(tags) })
		a.tagInput = ""
	})
	for _, tag := range a.state.Tags {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "• "+tag).FontSize(11).TextColor(a.tagColor(tag)).Grow(1)
			menuButton(c, "Color", func(menu *ui.Menu) {
				for name, color := range map[string]string{"Indigo": "#5260d8", "Teal": "#188980", "Rose": "#be456d", "Amber": "#ac7217", "Violet": "#8258cf"} {
					if menu.Item(name).Chosen() {
						s := a.store
						a.run("Tag color saved", func() error { return s.SaveTagColor(tag, color) })
					}
				}
			})
			textButton(c, "Remove tag").OnClick(func() {
				s := a.store
				a.confirm("Remove tag?", "This removes the tag from downloaded messages.", func() { a.run("Tag removed", func() error { return s.DeleteTag(tag) }) })
			})
		})
	}
	section(c, "Categories")
	field(c, "Category name", &a.categoryInput, "Work")
	textButton(c, "Create category").OnClick(func() {
		name, s := strings.TrimSpace(a.categoryInput), a.store
		if name != "" {
			a.run("Category created", func() error { return s.SaveCategory(name) })
			a.categoryInput = ""
		}
	})
	section(c, "Saved searches")
	note(c, `Combine words, "quoted phrases", from:, to:, subject:, tag:, category:, folder:, before:2026-10-01, after:, is:unread, is:starred, and has:attachment. Prefix a term with - to exclude it.`)
	field(c, "Search name", &a.searchName, "Unread client mail")
	note(c, "Current query: "+a.query)
	action(c, "search", "Save current search", false, func() {
		name, q, s := strings.TrimSpace(a.searchName), a.query, a.store
		if name != "" && q != "" {
			a.run("Search saved", func() error { return s.SaveSearch(name, q) })
		}
	})
	section(c, "Server folders")
	field(c, "Folder name", &a.folderInput, "Clients/Acme")
	action(c, "folder", "Create folder", false, func() {
		if a.accountID == "" {
			a.notice("Select an individual account first.", true)
			return
		}
		name, id, e, ctx := strings.TrimSpace(a.folderInput), a.accountID, a.engine, a.ctx
		if name != "" {
			a.run("Folder created", func() error { return e.CreateFolder(ctx, id, name) })
		}
	})
	section(c, "Incoming mail rules")
	for _, r := range a.state.Rules {
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, r.Name+" · "+r.Field+" contains "+r.Contains).FontSize(11).Grow(1)
			textButton(c, "Remove").OnClick(func() { id, s := r.ID, a.store; a.run("Rule removed", func() error { return s.DeleteRule(id) }) })
		})
	}
	field(c, "Rule name", &a.rule.Name, "Client correspondence")
	if a.rule.Field == "" {
		a.rule.Field = "from"
	}
	ui.Select(c, &a.rule.Field, []string{"from", "to", "subject", "body"}).Label("Rule field").Width(220)
	field(c, "Contains", &a.rule.Contains, "@client.com")
	field(c, "Apply tag", &a.rule.Tag, "Clients")
	field(c, "Set category", &a.rule.Category, "Work")
	field(c, "Move to folder (optional)", &a.rule.Folder, "Archive")
	ui.Checkbox(c, &a.rule.Star, "Star matching messages")
	matches := 0
	for _, m := range a.state.Messages {
		if ruleMatches(a.rule, m) {
			matches++
		}
	}
	note(c, fmt.Sprintf("Preview: %d downloaded messages match this rule.", matches))
	ui.Row(c).Gap(8).Children(func() {
		action(c, "check", "Save rule", true, func() {
			r, s := a.rule, a.store
			if strings.TrimSpace(r.Name) == "" || strings.TrimSpace(r.Contains) == "" {
				a.notice("Give the rule a name and matching text.", true)
				return
			}
			r.ID = mailclient.NewID()
			r.Enabled = true
			a.run("Rule saved", func() error { return s.SaveRule(r) })
			a.rule = mailclient.Rule{Field: "from"}
		})
		textButton(c, "Apply to matching mail").OnClick(func() {
			r := a.rule
			if r.Contains == "" {
				return
			}
			for _, m := range a.state.Messages {
				if ruleMatches(r, m) {
					if r.Tag != "" {
						m.Tags = append(m.Tags, r.Tag)
					}
					if r.Category != "" {
						m.Category = r.Category
					}
					m.Starred = m.Starred || r.Star
					if r.Folder != "" {
						m.Folder = r.Folder
					}
					a.mutate(m, r.Folder != "")
				}
			}
		})
	})
}
func ruleMatches(r mailclient.Rule, m mailclient.Message) bool {
	if r.Contains == "" {
		return false
	}
	v := m.From
	switch r.Field {
	case "to":
		v = m.To
	case "subject":
		v = m.Subject
	case "body":
		v = m.Text
	}
	return strings.Contains(strings.ToLower(v), strings.ToLower(r.Contains))
}
func (a *app) notificationSettings(c *ui.Context) {
	section(c, "Notifications")
	ui.Checkbox(c, &a.prefs.Notify, "Desktop notifications").OnChange(a.savePrefs)
	ui.Checkbox(c, &a.prefs.NotificationPreview, "Include message subject in notifications").OnChange(a.savePrefs)
	ui.Checkbox(c, &a.prefs.Background, "Keep running in the notification area when the window closes").OnChange(a.savePrefs)
	ui.Row(c).Gap(16).Children(func() {
		ui.Column(c).Width(160).Children(func() { fieldWidth(c, "Quiet hours start", &a.prefs.QuietStart, "22:00", 160) })
		ui.Column(c).Width(160).Children(func() { fieldWidth(c, "Quiet hours end", &a.prefs.QuietEnd, "08:00", 160) })
	})
	section(c, "Synchronization and sending")
	ui.Checkbox(c, &a.prefs.FullSync, "Download all mail for offline access").OnChange(a.savePrefs)
	if !a.prefs.FullSync {
		ui.Row(c).Gap(8).Children(func() {
			ui.Textf(c, "Download new mail from the last %d days", a.prefs.RecentDays).Width(320)
			textButton(c, "30 days").OnClick(func() { a.prefs.RecentDays = 30; a.savePrefs() })
			textButton(c, "90 days").OnClick(func() { a.prefs.RecentDays = 90; a.savePrefs() })
			textButton(c, "365 days").OnClick(func() { a.prefs.RecentDays = 365; a.savePrefs() })
		})
	}
	ui.Row(c).Gap(8).Children(func() {
		ui.Textf(c, "Check for mail every %d minutes", a.prefs.SyncMinutes).Width(280)
		textButton(c, "Less often").OnClick(func() { a.prefs.SyncMinutes = min(60, a.prefs.SyncMinutes+1); a.savePrefs() })
		textButton(c, "More often").OnClick(func() { a.prefs.SyncMinutes = max(1, a.prefs.SyncMinutes-1); a.savePrefs() })
	})
	ui.Row(c).Gap(8).Children(func() {
		ui.Textf(c, "Undo send delay: %d seconds", a.prefs.SendDelay).Width(280)
		textButton(c, "− 5 seconds").OnClick(func() { a.prefs.SendDelay = max(0, a.prefs.SendDelay-5); a.savePrefs() })
		textButton(c, "+ 5 seconds").OnClick(func() { a.prefs.SendDelay = min(120, a.prefs.SendDelay+5); a.savePrefs() })
	})
	note(c, "Queued and scheduled messages send while Bentomail is running and connected. Use Outbox → Edit draft to cancel a queued send. Failed or uncertain sends require your review.")
	action(c, "check", "Save notification settings", true, a.savePrefs)
}
func (a *app) storageSettings(c *ui.Context) {
	section(c, "Local storage")
	note(c, a.store.DataDir())
	var size int
	for _, m := range a.state.Messages {
		size += len(m.Raw)
	}
	note(c, fmt.Sprintf("%d messages · %d drafts · %s of raw mail", len(a.state.Messages), len(a.state.Drafts), fileSize(size)))
	note(c, "The local database contains email and attachments. Windows protects saved credentials for your user. Backups contain mail and settings; credentials must be re-entered on another computer.")
	ui.Row(c).Gap(8).Wrap().Children(func() {
		action(c, "download", "Back up mail", true, a.backup)
		action(c, "archive", "Restore backup", false, a.restore)
		action(c, "plus", "Import .eml files", false, a.importEML)
	})
	action(c, "sync", "Rebuild search index", false, func() { s := a.store; a.run("Search index rebuilt", s.RebuildIndex) })
	section(c, "Privacy")
	ui.Checkbox(c, &a.prefs.RemoteImages, "Allow remote images in email (may reveal that you opened it)").OnChange(a.savePrefs)
	note(c, "Active scripts, frames, forms, and remote styles are removed. Remote images are blocked unless you enable them. Attachments are saved only when you choose a destination.")
}
func (a *app) shortcutsSettings(c *ui.Context) {
	section(c, "Keyboard shortcuts")
	for _, s := range []string{"Ctrl N     Compose a new message", "Ctrl F     Focus search", "Ctrl Z     Undo a mail action in the workspace", "↑ / ↓      Navigate the message list", "Ctrl click / Shift click     Select multiple messages", "Ctrl A     Select all messages in a focused list", "Ctrl B / I / U     Bold / italic / underline in composer", "Ctrl Shift 7 / 8     Numbered / bulleted list", "Ctrl Z / Ctrl Shift Z     Undo / redo in editor", "Tab / Shift Tab     Move between controls"} {
		ui.Text(c, s).FontSize(12).Padding(5, 0)
	}
}
