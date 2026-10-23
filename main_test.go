package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo/ui"
)

func testApp(t *testing.T) *app {
	t.Helper()
	s, err := mailclient.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return newApp(s)
}
func seededApp(t *testing.T) *app {
	a := testApp(t)
	ac := mailclient.Account{ID: "work", Name: "Alex Morgan", Email: "alex@example.com", IMAPHost: "127.0.0.1", SMTPHost: "127.0.0.1", IMAPPort: 1143, SMTPPort: 1025, IMAPTLS: "None", SMTPTLS: "None", Enabled: true}
	if err := a.store.SaveAccount(ac, ""); err != nil {
		t.Fatal(err)
	}
	subjects := []string{"A clearer plan for the week", "Design review · Project North", "Updated proposal for your review", "The October report is ready", "Quick question about the timeline", "Notes from our planning session"}
	names := []string{"Jordan Lee <jordan@example.com>", "Sam Rivera <sam@example.com>", "Riley Chen <riley@example.com>"}
	for i, subject := range subjects {
		m := mailclient.Message{ID: subject, MessageID: subject, AccountID: ac.ID, Folder: "INBOX", Subject: subject, From: names[i%3], To: ac.Email, Text: "Hi Alex,\n\nI've put together the next steps for our project. Everything is looking good for the upcoming milestone.\n\nPlease have a look and let me know what you think. I'd love to get your feedback before Friday.\n\nThanks,\nJordan", Date: time.Now().Add(-time.Duration(i) * time.Hour), Read: i > 1, Tags: []string{"Work"}}
		if err := a.store.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	_ = a.store.SaveTags([]string{"Work", "Clients", "Finance"})
	a.state = a.store.Snapshot()
	return a
}
func TestEmptyWorkspaceAndSetup(t *testing.T) {
	a := testApp(t)
	tt := ui.NewTester(a.view, 1440, 900)
	if !tt.HasText("Select a message") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Add account"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Connected accounts") {
		t.Fatal(tt.Texts())
	}
	tt.Frame()
	a.page = "mail"
	tt.Frame()
	a.page = "settings"
	tt.Frame()
}
func TestSearchAndReadThenCompose(t *testing.T) {
	a := seededApp(t)
	tt := ui.NewTester(a.view, 1440, 900)
	if err := tt.Click("Search mail"); err != nil {
		t.Fatal(err)
	}
	tt.Type("subject:proposal")
	tt.Frame()
	ms := a.messages()
	if len(ms) != 1 || !strings.Contains(ms[0].Subject, "proposal") {
		t.Fatalf("search got %v", ms)
	}
	a.selectMessage(ms[0].ID)
	tt.Frame()
	if !tt.HasText("Reply") {
		t.Fatal(tt.Texts())
	}
	if err := tt.Click("Reply"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	co := a.activeComposer()
	if co == nil || co.draft.AccountID != "work" || co.draft.To == "" {
		t.Fatal("reply identity was not preserved")
	}
	if !strings.HasPrefix(co.draft.Subject, "Re:") {
		t.Fatal(co.draft.Subject)
	}
	tt.Frame()
	a.closeComposer(co.draft.ID, false)
	tt.Frame()
	if len(a.store.Snapshot().Drafts) != 1 {
		t.Fatal("draft was not saved")
	}
}
func TestNativeComposerFormattingAndPersistence(t *testing.T) {
	a := seededApp(t)
	a.newComposer(nil, "new")
	tt := ui.NewTester(a.view, 1440, 900)
	co := a.activeComposer()
	if err := tt.Click("Message body"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Hello team")
	tt.Frame()
	co.doc.SetSelection(0, 5)
	if err := tt.Click("B"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(co.snapshot().HTML, "<b>") {
		t.Fatal(co.snapshot().HTML)
	}
	co.toEntry = "recipient@example.com"
	co.draft.Subject = "Weekly update"
	co.dirty = true
	a.saveOpenDrafts()
	if len(a.store.Snapshot().Drafts) != 1 {
		t.Fatal("not saved")
	}
	a.queueComposer(co)
	tt.Frame()
	if a.folder != "outbox" {
		t.Fatal(a.status)
	}
	if a.state.Drafts[0].Status != "queued" {
		t.Fatal(a.state.Drafts)
	}
}
func TestSettingsAndThemeLifetimes(t *testing.T) {
	a := seededApp(t)
	tt := ui.NewTester(a.view, 1440, 900)
	for _, tab := range []string{"Accounts", "Appearance", "Organization", "Notifications", "Storage", "Shortcuts"} {
		a.page = "settings"
		a.settingsTab = tab
		tt.Frame()
		tt.Frame()
		a.page = "mail"
		tt.Frame()
	}
	a.prefs.Dark = true
	a.savePrefs()
	tt.Frame()
}
func TestQuietHours(t *testing.T) {
	now := time.Date(2026, 10, 21, 23, 0, 0, 0, time.Local)
	if !quietNow("22:00", "08:00", now) || quietNow("22:00", "08:00", now.Add(-12*time.Hour)) {
		t.Fatal("overnight quiet hours")
	}
}
func TestRenderWorkspace(t *testing.T) {
	dir := os.Getenv("BENTOMAIL_SCREENSHOTS")
	if dir == "" {
		t.Skip("set BENTOMAIL_SCREENSHOTS to export native UI renders")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	a := seededApp(t)
	a.selectedID = a.state.Messages[0].ID
	tt := ui.NewTester(a.view, 1440, 900)
	for _, name := range []string{"workspace", "bottom", "hidden", "drafts", "outbox", "collapsed", "account-switcher", "composer", "composer-small", "settings", "settings-dropdown", "settings-125", "settings-150", "settings-small", "settings-dark", "storage-dark", "storage", "dark"} {
		switch name {
		case "bottom":
			a.prefs.Layout = "Bottom"
		case "hidden":
			a.prefs.Layout = "Hidden"
		case "drafts":
			a.folder = "drafts"
			a.state.Drafts = append(a.state.Drafts, mailclient.Draft{ID: "render-draft", AccountID: "work", Subject: "Notes for our next review", To: "jordan@example.com", Status: "draft"})
		case "outbox":
			a.folder = "outbox"
			a.state.Drafts = append(a.state.Drafts, mailclient.Draft{ID: "render-outbox", AccountID: "work", Subject: "The weekly update", To: "sam@example.com", Status: "queued", SendAt: time.Now().Add(time.Hour)})
		case "collapsed":
			a.folder = "inbox"
			a.prefs.Layout = "Right"
			a.prefs.SidebarCollapsed = true
		case "account-switcher":
			a.prefs.SidebarCollapsed = false
			tt.Frame()
			if err := tt.Click("Active account"); err != nil {
				t.Fatal(err)
			}
		case "composer":
			tt.Key(0, ui.KeyEscape)
			a.newComposer(nil, "new")
		case "composer-small":
			tt.SetSize(1000, 680)
		case "settings":
			tt.SetSize(1440, 900)
			a.page = "settings"
			a.settingsTab = "Appearance"
		case "settings-dropdown":
			if err := tt.Click("Indigo"); err != nil {
				t.Fatal(err)
			}
		case "settings-125":
			tt.Key(0, ui.KeyEscape)
			tt.SetScale(1.25)
		case "settings-150":
			tt.SetScale(1.5)
		case "settings-small":
			tt.SetScale(1)
			tt.SetSize(1000, 680)
			tt.Scroll(800, 500, 0, 650)
		case "settings-dark":
			tt.SetSize(1440, 900)
			tt.Scroll(800, 500, 0, -650)
			a.prefs.Dark = true
			a.theme = workspaceTheme(true, "Indigo")
		case "storage-dark":
			a.settingsTab = "Storage"
			tt.SetSize(1000, 680)
		case "storage":
			a.prefs.Dark = false
			a.theme = workspaceTheme(false, "Indigo")
		case "dark":
			tt.SetSize(1440, 900)
			a.page = "mail"
			a.prefs.Dark = true
			a.theme = workspaceTheme(true, "Indigo")
		}
		tt.Frame()
		tt.Frame()
		tt.Advance(250 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

func TestActionsStayVisibleAtMinimumWindowSize(t *testing.T) {
	a := seededApp(t)
	a.selectedID = a.state.Messages[0].ID
	tt := ui.NewTester(a.view, 1000, 680)
	for _, label := range []string{"Reply", "Forward"} {
		r, ok := tt.Find(label)
		if !ok || r.Y+r.H > 668 {
			t.Fatalf("%s outside minimum window: %+v", label, r)
		}
	}
	a.newComposer(nil, "new")
	tt.Frame()
	r, ok := tt.Find("Send")
	if !ok || r.Y+r.H > 668 {
		t.Fatalf("send outside minimum window: %+v", r)
	}
}
