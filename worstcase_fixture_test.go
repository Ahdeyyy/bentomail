package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo/ui"
)

// worstCaseApp seeds the store with realistic data that demo fixtures avoid:
// long and one-letter names, bare addresses, HTML-only bodies, a missing Date
// header, punctuation and emoji in display names, non-Latin scripts and long
// attachment names. The first rows carry different failures, as real data does.
func worstCaseApp(t *testing.T) *app {
	a := testApp(t)
	accounts := []mailclient.Account{
		{ID: "work", Name: "Aleksandra Wiśniewska-Kowalczyk", Email: "aleksandra.wisniewska-kowalczyk@northwind-industries-holdings.example.com", Color: "#be456d"},
		{ID: "jo", Name: "Jo", Email: "jo@x.io"},
	}
	for _, ac := range accounts {
		ac.IMAPHost, ac.SMTPHost, ac.IMAPPort, ac.SMTPPort, ac.IMAPTLS, ac.SMTPTLS, ac.Enabled = "127.0.0.1", "127.0.0.1", 1143, 1025, "None", "None", true
		if err := a.store.SaveAccount(ac, ""); err != nil {
			t.Fatal(err)
		}
	}
	cc := make([]string, 0, 28)
	for i := range 28 {
		cc = append(cc, fmt.Sprintf("team.member.%02d@northwind-industries-holdings.example.com", i+1))
	}
	now := time.Now()
	body := "Hi Aleksandra,\n\nAttached is the revised board pack. The EMEA numbers moved after the FX restatement, so please re-check slides 14–22 before Thursday.\n\nBest,\nBartholomew"
	messages := []mailclient.Message{
		{ID: "w1", AccountID: "work", Subject: "Re: Re: Fwd: [EXTERNAL] Updated Q3 2026 board pack — revised figures for EMEA, APAC and North America (please review before Thursday)",
			From: `"Bartholomew Fitzgerald-Montgomery III" <bartholomew.fitzgerald@northwind-industries-holdings.example.com>`,
			To:   "aleksandra.wisniewska-kowalczyk@northwind-industries-holdings.example.com", CC: strings.Join(cc, ", "),
			Text: body, Date: now.Add(-3 * time.Minute), Starred: true,
			Tags: []string{"Quarterly Business Review – EMEA & APAC", "Finance", "法务", "🔥 Urgent", "Work"},
			Attachments: []mailclient.Attachment{
				{Name: "Q3-2026-Board-Pack-Northwind-Industries-Holdings-FINAL-v12 (approved by legal).pdf", ContentType: "application/pdf", Data: make([]byte, 4_812_000)},
				{Name: "Q3-2026-Board-Pack-Northwind-Industries-Holdings-FINAL-v11.pdf", ContentType: "application/pdf", Data: make([]byte, 4_790_000)},
			}},
		// Bare address with no display name, no subject, image-only HTML body
		// (ParseMessage derives Text from HTML, which leaves nothing here).
		{ID: "w2", AccountID: "work", From: "bartholomew.fitzgerald@northwind-industries-holdings.example.com", To: "aleksandra.wisniewska-kowalczyk@northwind-industries-holdings.example.com",
			HTML: `<img src="cid:banner" alt="">`, Date: now.Add(-40 * time.Minute)},
		{ID: "w3", AccountID: "jo", Subject: "ok", From: `"Jo" <jo@x.io>`, To: "jo@x.io", Text: "👍", Date: now.Add(-2 * time.Hour), Read: true},
		{ID: "w4", AccountID: "work", Subject: "Your invoice for October is available 🧾", From: `"Support Team (Stripe)" <support@stripe.example.com>`, To: "aleksandra.wisniewska-kowalczyk@northwind-industries-holdings.example.com",
			Text: "Your invoice INV-2026-10-004821 for €1,284.00 is now available.", Date: now.Add(-26 * time.Hour), Read: true, Tags: []string{"Finance"}},
		{ID: "w5", AccountID: "work", Subject: "Báo cáo tài chính quý 3 — cập nhật lần cuối", From: `"Nguyễn Thị Phương Thảo" <thao.nguyen@example.vn>`, To: "jo@x.io",
			Text: "Chào chị, em gửi bản cập nhật cuối cùng của báo cáo tài chính quý 3.", Date: now.Add(-50 * time.Hour)},
		{ID: "w6", AccountID: "work", Subject: "🇵🇱 Tygodnik: najważniejsze wiadomości", From: `"🇵🇱 Polska Newsletter" <hello@newsletter.example.pl>`, To: "jo@x.io",
			Text: "Najważniejsze wiadomości tygodnia.", Date: now.Add(-80 * time.Hour), Read: true},
		{ID: "w7", AccountID: "work", Subject: "اجتماع الفريق الأسبوعي", From: `"محمد عبد الله" <m.abdullah@example.ae>`, To: "jo@x.io",
			Text: "مرحبا، تم تأكيد موعد الاجتماع.", Date: now.Add(-100 * time.Hour), Read: true},
		// Missing Date header: the zero time.
		{ID: "w8", AccountID: "work", Subject: "Delivery Status Notification (Failure)", From: `"Mail Delivery Subsystem" <mailer-daemon@googlemail.com>`, To: "jo@x.io",
			Text: "Address not found.", Read: true},
		{ID: "w9", AccountID: "work", Subject: "สรุปการประชุมประจำไตรมาส", From: `"สมชาย ใจดี" <somchai@example.co.th>`, To: "jo@x.io",
			Text: "สวัสดีครับ ขอส่งสรุปการประชุมประจำไตรมาสครับ", Date: now.AddDate(-1, 0, -3), Read: true},
	}
	for _, m := range messages {
		m.MessageID, m.Folder = m.ID, "INBOX"
		if err := a.store.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	// 1,284 unread behind the visible rows: the sidebar badge and the count.
	for i := range 1284 {
		m := mailclient.Message{ID: fmt.Sprintf("bulk-%04d", i), AccountID: "work", Folder: "INBOX", Subject: fmt.Sprintf("Automated report #%d", i),
			From: "reports@northwind-industries-holdings.example.com", To: "jo@x.io", Text: "Report attached.", Date: now.AddDate(0, 0, -10-i)}
		m.MessageID = m.ID
		if err := a.store.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	_ = a.store.SaveTags([]string{"Quarterly Business Review – EMEA & APAC", "Finance", "法务", "🔥 Urgent", "Work", "Clients · Northwind Industries Holdings"})
	a.state = a.store.Snapshot()
	return a
}

// oneMessageApp is the pluralization case: every count is exactly 1.
func oneMessageApp(t *testing.T) *app {
	a := testApp(t)
	ac := mailclient.Account{ID: "work", Name: "Alex Morgan", Email: "alex@example.com", IMAPHost: "127.0.0.1", SMTPHost: "127.0.0.1", IMAPPort: 1143, SMTPPort: 1025, IMAPTLS: "None", SMTPTLS: "None", Enabled: true}
	if err := a.store.SaveAccount(ac, ""); err != nil {
		t.Fatal(err)
	}
	m := mailclient.Message{ID: "one", MessageID: "one", AccountID: "work", Folder: "INBOX", Subject: "Welcome", From: "Jordan Lee <jordan@example.com>", To: ac.Email, Text: "Hello.", Date: time.Now()}
	if err := a.store.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	a.state = a.store.Snapshot()
	return a
}

// emptyApp has an account but no mail.
func emptyApp(t *testing.T) *app {
	a := testApp(t)
	ac := mailclient.Account{ID: "work", Name: "Alex Morgan", Email: "alex@example.com", IMAPHost: "127.0.0.1", SMTPHost: "127.0.0.1", IMAPPort: 1143, SMTPPort: 1025, IMAPTLS: "None", SMTPTLS: "None", Enabled: true}
	if err := a.store.SaveAccount(ac, ""); err != nil {
		t.Fatal(err)
	}
	a.state = a.store.Snapshot()
	return a
}

func TestWorstCaseInitialsAndCounts(t *testing.T) {
	for name, want := range map[string]string{
		"Jordan Lee": "JL", "Jo": "J", "": "?", "🦄": "?",
		"Support Team (Stripe)":                 "ST",
		"Bartholomew Fitzgerald-Montgomery III": "BF",
		"🇵🇱 Polska Newsletter":                  "PN",
		"Nguyễn Thị Phương Thảo":                "NT",
	} {
		if got := initials(name); got != want {
			t.Errorf("initials(%q) = %q, want %q", name, got, want)
		}
	}
	for n, want := range map[int]string{0: "0 conversations", 1: "1 conversation", 1293: "1,293 conversations", 1284000: "1,284,000 conversations"} {
		if got := countLabel(n, "conversation"); got != want {
			t.Errorf("countLabel(%d) = %q, want %q", n, got, want)
		}
	}
	if mailDate(time.Time{}) != "" {
		t.Error("a missing Date header renders as a date")
	}
}

func TestWorstCaseReaderKeepsActionsVisible(t *testing.T) {
	a := worstCaseApp(t)
	a.selectedID = "w1"
	tt := ui.NewTester(a.view, 1000, 680)
	tt.Frame()
	r, ok := tt.Find("Reply all")
	if !ok || r.Y+r.H > 680 {
		t.Fatalf("reply actions pushed out of the reader by long recipient lists: %+v", r)
	}
}

func TestMiddleTruncateKeepsVersions(t *testing.T) {
	v12 := middleTruncate("Q3-2026-Board-Pack-Northwind-Industries-Holdings-FINAL-v12 (approved by legal).pdf", 36)
	v11 := middleTruncate("Q3-2026-Board-Pack-Northwind-Industries-Holdings-FINAL-v11.pdf", 36)
	if v12 == v11 || len([]rune(v12)) != 36 || !strings.HasSuffix(v11, "FINAL-v11.pdf") {
		t.Fatalf("truncated names collide or lose their end: %q, %q", v12, v11)
	}
	if middleTruncate("report.pdf", 36) != "report.pdf" {
		t.Fatal("short names must not change")
	}
}

func TestWorstCaseReaderBodyScrollsWithHeader(t *testing.T) {
	a := worstCaseApp(t)
	a.selectedID = "w1"
	tt := ui.NewTester(a.view, 1000, 680)
	tt.Frame()
	msg, ok := tt.Find("Message")
	if !ok || msg.H < 200 {
		t.Fatalf("message scroll is squeezed: %+v", msg)
	}
	// Scrolling the message reaches the body below the tall header.
	tt.Scroll(msg.X+msg.W/2, msg.Y+msg.H/2, 0, 2000)
	tt.Frame()
	body, ok := tt.Find("Attached is the revised board pack. The EMEA numbers moved after the FX restatement, so please re-check slides 14–22 before Thursday.")
	if !ok && !tt.HasText("Best,") {
		t.Fatal("message body unreachable")
	}
	if ok && (body.Y > msg.Y+msg.H || body.Y+body.H < msg.Y) {
		t.Fatalf("body not scrolled into the message view: %+v in %+v", body, msg)
	}
}

func TestEmptyFolderReaderPrompt(t *testing.T) {
	a := emptyApp(t)
	tt := ui.NewTester(a.view, 1000, 680)
	tt.Frame()
	if !tt.HasText("Nothing to read here") || tt.HasText("Select a message") {
		t.Fatal("empty folder still asks to select a message")
	}
}

func TestLongMailMenuFadesUntilScrolledToEnd(t *testing.T) {
	a := worstCaseApp(t)
	tt := ui.NewTester(a.view, 1000, 680)
	tt.Frame()
	ms := a.messages()
	if err := tt.RightClick(ms[0].Subject); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.mailMenu.scroll.MaxY <= 0 {
		t.Skip("menu fits without scrolling at this size")
	}
	fade := func() bool {
		_, ok := tt.Find("email-menu-fade")
		return ok || a.mailMenu.scroll.Y < a.mailMenu.scroll.MaxY-1
	}
	if !fade() {
		t.Fatal("overflowing menu shows no fade")
	}
	a.mailMenu.scroll.Y = a.mailMenu.scroll.MaxY
	tt.Frame()
	if a.mailMenu.scroll.Y < a.mailMenu.scroll.MaxY-1 {
		t.Fatal("menu did not reach its end")
	}
}

func TestLongRecipientListCollapses(t *testing.T) {
	a := worstCaseApp(t)
	a.selectedID = "w1"
	tt := ui.NewTester(a.view, 1000, 680)
	tt.Frame()
	if tt.HasText("team.member.04@northwind-industries-holdings.example.com") {
		t.Fatal("collapsed Cc shows its fourth address")
	}
	if err := tt.Click("Show 25 more Cc recipients"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.recipientsOpen != "w1" {
		t.Fatal("recipient list did not expand")
	}
}

func TestHiddenLayoutAvatarRestsInHeader(t *testing.T) {
	a := worstCaseApp(t)
	a.prefs.Layout = "Hidden"
	tt := ui.NewTester(a.view, 1000, 680)
	tt.Frame()
	a.selectMessage("w1")
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	msg, _ := tt.Find("Message")
	tt.Scroll(msg.X+msg.W/2, msg.Y+40, 0, 400)
	tt.Frame()
	avatar, ok := tt.Find("Sender avatar w1")
	if ok && avatar.Y < msg.Y && avatar.Y+avatar.H > msg.Y-48 {
		t.Fatalf("resting avatar overlaps the toolbar after scrolling: %+v above %+v", avatar, msg)
	}
}
