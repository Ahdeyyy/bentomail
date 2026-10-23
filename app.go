package main

import (
	"context"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type work struct {
	label string
	fn    func() error
	done  func(error)
}
type app struct {
	store                                                                   *mailclient.Store
	engine                                                                  *mailclient.Engine
	state                                                                   mailclient.State
	prefs                                                                   mailclient.Preferences
	theme                                                                   *ui.Theme
	window                                                                  *mygo.Window
	childWindows                                                            []*app
	ctx                                                                     context.Context
	dispatch                                                                func(func())
	jobs                                                                    chan work
	networkJobs                                                             chan work
	closed                                                                  bool
	status                                                                  string
	statusError                                                             bool
	busy                                                                    int
	syncing                                                                 bool
	syncCancel                                                              context.CancelFunc
	watchers                                                                map[string]context.CancelFunc
	watchConfig                                                             map[string]string
	lastSync, timeSaved                                                     time.Time
	selectedAt                                                              time.Time
	accountID, accountPick, folder, page, query, selectedID, filter         string
	sortOldest, threaded                                                    bool
	list                                                                    ui.ListState
	sidebarMotion                                                           sidebarMotion
	avatarMotion                                                            avatarMotion
	rowIndex                                                                int
	selection                                                               ui.Selection[string]
	searchHandle                                                            ui.Handle
	mailMenu                                                                mailMenuState
	mailMenuTrigger                                                         ui.Handle
	focus                                                                   string
	preview                                                                 ui.NativeWebView
	loadHTML                                                                func(string)
	print                                                                   func()
	printPending                                                            bool
	previewKey                                                              string
	bodyMode                                                                string
	composers                                                               []*composer
	composeID                                                               string
	settingsTab                                                             string
	accountForm                                                             mailclient.Account
	password, accountError                                                  string
	imapPort, smtpPort                                                      string
	tagInput, categoryInput, folderInput, searchName, templateName, linkURL string
	rule                                                                    mailclient.Rule
	searchResult                                                            map[string]bool
	searchResultQuery                                                       string
	searchRevision                                                          uint64
	reminderShown                                                           map[string]bool
	sending                                                                 map[string]bool
	undo                                                                    []mailclient.Message
	oauthProvider, oauthClientID, oauthClientSecret                         string
	aliasForm                                                               mailclient.Identity
	contactName, contactEmail                                               string
	tray                                                                    *mygo.Tray
	closing, allowClose, quitRequested                                      bool
	confirmOpen                                                             bool
	confirmTitle, confirmMessage                                            string
	confirmAction                                                           func()
}

func newApp(s *mailclient.Store) *app {
	a := &app{store: s, engine: mailclient.NewEngine(s), ctx: context.Background(), folder: "inbox", page: "mail", rowIndex: -1, bodyMode: "Formatted", settingsTab: "Accounts", threaded: true, reminderShown: map[string]bool{}, sending: map[string]bool{}, selection: ui.Selection[string]{}}
	a.state = s.Snapshot()
	a.prefs = a.state.Preferences
	if a.prefs.ReadMode == "" {
		a.prefs.ReadMode = "Immediately"
	}
	if a.prefs.ReadingFont == "" {
		a.prefs.ReadingFont = "Martian Grotesk"
	}
	if a.prefs.ListWidth < 260 {
		a.prefs.ListWidth = 350
	}
	if a.prefs.SidebarWidth < 190 {
		a.prefs.SidebarWidth = 224
	}
	if a.prefs.ReadingSize < 11 {
		a.prefs.ReadingSize = 15
	}
	if a.prefs.SyncMinutes < 1 {
		a.prefs.SyncMinutes = 5
	}
	if a.prefs.Density == "" {
		a.prefs.Density = "Comfortable"
	}
	if a.prefs.Layout == "" {
		a.prefs.Layout = "Right"
	}
	if a.prefs.Accent == "" {
		a.prefs.Accent = "Indigo"
	}
	a.prefs.Translucency = normalizedTranslucency(a.prefs.Translucency)
	a.prefs.AnimationSpeed = mailclient.NormalizeAnimationSpeed(a.prefs.AnimationSpeed)
	a.theme = workspaceTheme(a.prefs.Dark, a.prefs.Accent, a.prefs.Translucency)
	a.list.Selected = &a.rowIndex
	a.list.Selection = &a.selection
	a.resetAccountForm()
	a.updateAccountPick()
	a.status = "Ready"
	return a
}
func (a *app) startWorker(ctx context.Context) {
	a.jobs = make(chan work, 128)
	a.networkJobs = make(chan work, 64)
	dispatch := a.dispatch
	s := a.store
	worker := func(jobs <-chan work) {
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-jobs:
				err := j.fn()
				state := s.Snapshot()
				dispatch(func() {
					if a.closed {
						return
					}
					a.busy--
					if state.Revision >= a.state.Revision {
						a.state = state
					}
					if err != nil {
						a.notice(j.label+": "+err.Error(), true)
					} else {
						a.notice(j.label, false)
					}
					if j.done != nil {
						j.done(err)
					}
					a.updateAccountPick()
					a.updatePreview()
					a.updateBadge()
					a.refreshWatchers()
				})
			}
		}
	}
	go worker(a.jobs)
	go worker(a.networkJobs)
}
func (a *app) network(label string, fn func() error, done ...func(error)) {
	if a.networkJobs == nil {
		a.run(label, fn, done...)
		return
	}
	var cb func(error)
	if len(done) > 0 {
		cb = done[0]
	}
	select {
	case a.networkJobs <- work{label, fn, cb}:
		a.busy++
		a.status = label + "…"
		a.statusError = false
	default:
		a.notice("A network operation is already queued. Try again shortly.", true)
	}
}
func (a *app) run(label string, fn func() error, done ...func(error)) {
	var cb func(error)
	if len(done) > 0 {
		cb = done[0]
	}
	if a.jobs == nil {
		err := fn()
		a.state = a.store.Snapshot()
		if err != nil {
			a.notice(label+": "+err.Error(), true)
		} else {
			a.notice(label, false)
		}
		if cb != nil {
			cb(err)
		}
		return
	}
	select {
	case a.jobs <- work{label, fn, cb}:
		a.busy++
		a.status = label + "…"
		a.statusError = false
	default:
		a.notice("Another operation is still running; try again shortly.", true)
	}
}
func (a *app) notice(s string, bad bool) { a.status = s; a.statusError = bad }
func (a *app) tick() {
	now := time.Now()
	if a.prefs.ReadMode == "After 3 seconds" && now.Sub(a.selectedAt) >= 3*time.Second {
		if m, ok := a.selected(); ok && !m.Read {
			m.Read = true
			a.mutate(m, true)
		}
	}
	if now.Sub(a.timeSaved) > 3*time.Second {
		a.saveOpenDrafts()
		a.timeSaved = now
	}
	if now.Sub(a.lastSync) > time.Duration(max(a.prefs.SyncMinutes, 1))*time.Minute {
		a.syncMail()
	}
	for _, d := range a.state.Drafts {
		if d.Status == "queued" && !d.SendAt.IsZero() && !d.SendAt.After(now) && !a.sending[d.ID] {
			a.sendQueued(d)
		}
	}
	for _, m := range a.state.Messages {
		if !m.FollowUp.IsZero() && !m.FollowUp.After(now) && !a.reminderShown[m.ID] {
			a.reminderShown[m.ID] = true
			a.notice("Follow up: "+m.Subject, false)
			a.notify("Follow-up reminder", m.Subject)
		}
	}
}
func (a *app) syncMail() {
	if a.syncing || len(a.state.Accounts) == 0 {
		return
	}
	a.syncing = true
	a.lastSync = time.Now()
	ctx, cancel := context.WithCancel(a.ctx)
	a.syncCancel = cancel
	e, id := a.engine, a.accountID
	before := map[string]bool{}
	for _, m := range a.state.Messages {
		before[m.ID] = true
	}
	a.network("Mail synchronized", func() error { return e.Sync(ctx, id) }, func(err error) {
		a.syncing = false
		cancel()
		a.syncCancel = nil
		if err != nil {
			return
		}
		var fresh []mailclient.Message
		for _, m := range a.state.Messages {
			if !before[m.ID] && !m.Read && canonicalFolder(m.Folder) == "inbox" {
				fresh = append(fresh, m)
			}
		}
		if len(fresh) > 0 {
			body := fmt.Sprintf("%d new messages", len(fresh))
			if a.prefs.NotificationPreview {
				body = fresh[0].Subject
			}
			a.notify("New mail", body)
		}
	})
}
func (a *app) notify(title, body string) {
	if a.window == nil || !a.prefs.Notify || quietNow(a.prefs.QuietStart, a.prefs.QuietEnd, time.Now()) {
		return
	}
	go func() { _ = mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body}).Show() }()
}
func quietNow(start, end string, now time.Time) bool {
	if start == "" || end == "" {
		return false
	}
	s, e1 := time.Parse("15:04", start)
	e, e2 := time.Parse("15:04", end)
	if e1 != nil || e2 != nil {
		return false
	}
	n := now.Hour()*60 + now.Minute()
	lo, hi := s.Hour()*60+s.Minute(), e.Hour()*60+e.Minute()
	if lo > hi {
		return n >= lo || n < hi
	}
	return n >= lo && n < hi
}
func (a *app) account(id string) mailclient.Account {
	for _, v := range a.state.Accounts {
		if v.ID == id {
			return v
		}
	}
	return mailclient.Account{}
}
func (a *app) updateAccountPick() {
	a.accountPick = "All accounts"
	if ac := a.account(a.accountID); ac.ID != "" {
		a.accountPick = ac.Name + " <" + ac.Email + ">"
	} else {
		a.accountID = ""
	}
}
func (a *app) navigate(folder string) {
	a.folder = folder
	a.page = "mail"
	a.selectedID = ""
	a.rowIndex = -1
	a.selection = ui.Selection[string]{}
	a.list.Selection = &a.selection
	a.previewKey = ""
	a.updatePreview()
}
func canonicalFolder(f string) string {
	v := strings.ToLower(f)
	for _, sep := range []string{"/", "."} {
		parts := strings.Split(v, sep)
		v = parts[len(parts)-1]
	}
	switch v {
	case "sent mail", "sent items", "sent messages":
		return "sent"
	case "deleted items", "deleted messages", "bin":
		return "trash"
	case "junk", "junk email":
		return "spam"
	case "all mail", "archives":
		return "archive"
	}
	return v
}
func (a *app) messages() []mailclient.Message {
	var ms []mailclient.Message
	now := time.Now()
	for _, m := range a.state.Messages {
		if a.accountID != "" && m.AccountID != a.accountID {
			continue
		}
		f := canonicalFolder(m.Folder)
		show := false
		switch a.folder {
		case "all":
			show = f != "trash" && f != "spam"
		case "starred":
			show = m.Starred
		case "snoozed":
			show = m.SnoozedUntil.After(now)
		case "followup":
			show = !m.FollowUp.IsZero()
		default:
			if strings.HasPrefix(a.folder, "tag:") {
				show = slices.Contains(m.Tags, strings.TrimPrefix(a.folder, "tag:"))
			} else {
				show = f == a.folder || m.Folder == a.folder
			}
		}
		if a.folder != "snoozed" && m.SnoozedUntil.After(now) {
			show = false
		}
		if !show {
			continue
		}
		switch a.filter {
		case "Unread":
			if m.Read {
				continue
			}
		case "Attachments":
			if len(m.Attachments) == 0 {
				continue
			}
		case "Important":
			if !m.Important {
				continue
			}
		}
		ms = append(ms, m)
	}
	if a.query != "" && a.searchResultQuery == a.query && a.searchRevision == a.state.Revision {
		ms = slices.DeleteFunc(ms, func(m mailclient.Message) bool { return !a.searchResult[m.ID] })
	} else {
		ms = mailclient.Search(ms, a.query)
	}
	slices.SortStableFunc(ms, func(x, y mailclient.Message) int {
		if a.sortOldest {
			return x.Date.Compare(y.Date)
		}
		return y.Date.Compare(x.Date)
	})
	if a.threaded {
		seen := map[string]bool{}
		out := ms[:0]
		for _, m := range ms {
			k := threadKey(m)
			if k != "" && seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, m)
		}
		ms = out
	}
	return ms
}
func threadKey(m mailclient.Message) string {
	if len(m.References) > 0 {
		return m.AccountID + ":" + m.References[0]
	}
	if m.InReplyTo != "" {
		return m.AccountID + ":" + m.InReplyTo
	}
	if m.MessageID != "" {
		return m.AccountID + ":" + m.MessageID
	}
	return m.AccountID + ":" + m.ID
}
func (a *app) selected() (mailclient.Message, bool) {
	for _, m := range a.state.Messages {
		if m.ID == a.selectedID {
			return m, true
		}
	}
	return mailclient.Message{}, false
}
func (a *app) selectMessage(id string) {
	a.selectedID = id
	a.selectedAt = time.Now()
	a.previewKey = ""
	a.updatePreview()
	if m, ok := a.selected(); ok && !m.Read && a.prefs.ReadMode == "Immediately" {
		m.Read = true
		a.mutate(m, true)
	}
}
func (a *app) mutate(m mailclient.Message, remote bool) {
	s, e, ctx := a.store, a.engine, a.ctx
	old := ""
	for i, v := range a.state.Messages {
		if v.ID == m.ID {
			old = v.Folder
			a.state.Messages[i] = m
			break
		}
	}
	run := a.run
	if remote {
		run = a.network
	}
	run("Message updated", func() error {
		if remote {
			return e.UpdateMessage(ctx, m, old)
		}
		return s.SaveMessage(m)
	})
}
func (a *app) selectedMessages() []mailclient.Message {
	var out []mailclient.Message
	for _, m := range a.messages() {
		if a.selection.Has(m.ID) {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		if m, ok := a.selected(); ok {
			out = append(out, m)
		}
	}
	return out
}
func (a *app) bulk(change func(*mailclient.Message), remote bool) {
	ms := a.selectedMessages()
	a.undo = append([]mailclient.Message{}, ms...)
	for _, m := range ms {
		change(&m)
		a.mutate(m, remote)
	}
	a.updatePreview()
}
func (a *app) undoAction() {
	ms := a.undo
	a.undo = nil
	for _, m := range ms {
		a.mutate(m, true)
	}
}
func sender(from string) (string, string) {
	if p, e := mail.ParseAddress(from); e == nil {
		n := p.Name
		if n == "" {
			n = strings.Split(p.Address, "@")[0]
		}
		return n, p.Address
	}
	return from, from
}
func initials(s string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return "?"
	}
	r := []rune(words[0])
	v := string(r[0])
	if len(words) > 1 {
		v += string([]rune(words[len(words)-1])[0])
	}
	return strings.ToUpper(v)
}
func (a *app) savePrefs() {
	a.prefs.Translucency = normalizedTranslucency(a.prefs.Translucency)
	a.prefs.AnimationSpeed = mailclient.NormalizeAnimationSpeed(a.prefs.AnimationSpeed)
	p, s := a.prefs, a.store
	a.theme = workspaceTheme(p.Dark, p.Accent, p.Translucency)
	a.run("Preferences saved", func() error { return s.SavePreferences(p) })
	a.previewKey = ""
	a.updatePreview()
	if a.window != nil {
		if p.Background {
			a.setupTray()
		} else if a.tray != nil {
			a.tray.Destroy()
			a.tray = nil
		}
		source := mygo.ThemeLight
		if p.Dark {
			source = mygo.ThemeDark
		}
		mygo.Theme.SetSource(source)
	}
}
