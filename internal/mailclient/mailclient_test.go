package mailclient

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func localAccount() Account {
	return Account{ID: "test", Name: "Zoë, Project Lead", Email: "zoe@example.com", IMAPHost: "127.0.0.1", SMTPHost: "127.0.0.1", IMAPTLS: "None", SMTPTLS: "None", Enabled: true}
}
func TestMIMEIdentityAttachmentsAndBCC(t *testing.T) {
	a := localAccount()
	a.ReplyTo = "replies@example.com"
	d := Draft{ID: "stable", To: "Taylor <taylor@example.net>", CC: "colleague@example.net", BCC: "hidden@example.net", Subject: "Résumé — Q4", Text: "Hello, team", HTML: "<p><b>Hello</b>, team<img src=\"data:image/png;base64,aGVsbG8=\"></p>", Attachments: []Attachment{{Name: "report, final.txt", ContentType: "text/plain", Data: []byte("an attachment\n.\n")}}}
	raw, rcpts, err := BuildMIME(a, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(rcpts) != 3 {
		t.Fatal(rcpts)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	from, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil || from.Name != a.Name {
		t.Fatalf("From %q: %v", msg.Header.Get("From"), err)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Fatal("BCC leaked")
	}
	parsed, err := ParseMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Subject != d.Subject || parsed.Text != d.Text || len(parsed.Attachments) != 2 {
		t.Fatalf("MIME round trip: %+v", parsed)
	}
	if !strings.Contains(parsed.HTML, "cid:") || strings.Contains(parsed.HTML, "data:image") {
		t.Fatal(parsed.HTML)
	}
	if string(parsed.Attachments[0].Data) != string(d.Attachments[0].Data) {
		t.Fatal("attachment changed")
	}
	d.To = "good@example.net\r\nBcc:evil@example.net"
	if _, _, err = BuildMIME(a, d); err == nil {
		t.Fatal("accepted header injection")
	}
	d.To = "not an email"
	if _, _, err = BuildMIME(a, d); err == nil {
		t.Fatal("accepted invalid address")
	}
}
func TestSanitizerBlocksActiveContentAndRemoteLeaks(t *testing.T) {
	source := `<head><meta http-equiv=refresh content="0;https://evil.test"><style>@import 'https://evil.test';</style></head><p onclick='evil()' style='background:url(https://evil.test)'>hello<script>evil()</script><iframe src=https://evil.test></iframe><img src=https://evil.test/pixel srcset='https://evil.test/2 2x'><img src='data:image/png;base64,aGVsbG8='><a href='jav&#97;script:alert(1)'>bad</a><a href='https://example.com'>safe</a><svg><image href=https://evil.test/></svg></p>`
	clean := SanitizeHTML(source, false)
	for _, bad := range []string{"evil.test", "onclick", "script", "iframe", "srcset", "background", "svg"} {
		if strings.Contains(clean, bad) {
			t.Fatalf("unsafe %s remained: %s", bad, clean)
		}
	}
	if !strings.Contains(clean, "https://example.com") || !strings.Contains(clean, "data:image/png") {
		t.Fatal(clean)
	}
	if !strings.Contains(SanitizeHTML(`<img src="https://example.com/p.png">`, true), "https://example.com") {
		t.Fatal("explicit remote opt in lost")
	}
}
func TestStoreRecoveryBackupAndSecretProtection(t *testing.T) {
	s := testStore(t)
	a := localAccount()
	if err := s.SaveAccount(a, "super-secret-password"); err != nil {
		t.Fatal(err)
	}
	d, _ := s.SaveDraft(Draft{ID: "interrupted", AccountID: a.ID, Status: "sending"})
	if err := s.SaveMessage(Message{ID: "one", Subject: "Local mail", Text: "saved", Date: time.Now()}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, "secrets.dat"))
	if err != nil || bytes.Contains(data, []byte("super-secret")) {
		t.Fatal("credential was not protected")
	}
	state, _ := os.ReadFile(filepath.Join(s.dir, "mail.sqlite"))
	if bytes.Contains(state, []byte("super-secret")) {
		t.Fatal("secret leaked into state")
	}
	again, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { again.Close() })
	if again.Snapshot().Drafts[0].Status != "uncertain" {
		t.Fatal("interrupted send was retried")
	}
	password, _, err := again.GetAccountSecret(a.ID)
	if err != nil || password != "super-secret-password" {
		t.Fatal("credential round trip")
	}
	_ = d
	backup := filepath.Join(t.TempDir(), "backup.json")
	if err = s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	backupBytes, _ := os.ReadFile(backup)
	if bytes.Contains(backupBytes, []byte("super-secret")) {
		t.Fatal("backup leaked password")
	}
	if err = s.DeleteMessage("one"); err != nil {
		t.Fatal(err)
	}
	if err = s.Restore(backup); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Messages) != 1 {
		t.Fatal("restore lost mail")
	}
	before := s.Snapshot()
	_ = os.WriteFile(backup, []byte("broken"), 0600)
	if err = s.Restore(backup); err == nil || len(s.Snapshot().Messages) != len(before.Messages) {
		t.Fatal("invalid restore modified live data")
	}
	brokenDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(brokenDir, "state.json"), []byte("broken"), 0600)
	if _, err = Open(brokenDir); err == nil {
		t.Fatal("corrupt store silently reset")
	}
}
func TestSearchOperators(t *testing.T) {
	ms := []Message{{ID: "a", From: "Jordan <j@example.com>", To: "alex@example.com", Subject: "Q4 client proposal", Text: "exact phrase to find", Category: "Work", Tags: []string{"Client A"}, Date: time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local), Attachments: []Attachment{{Name: "budget.pdf"}}}, {ID: "b", Subject: "Other proposal", Read: true, Text: "not related"}}
	for _, q := range []string{`from:j@example.com subject:proposal is:unread`, `tag:"Client A" has:attachment -subject:other`, `"exact phrase" before:2026-10-21 after:2026-10-19`, `attachment:pdf category:work`} {
		found := Search(ms, q)
		if len(found) != 1 || found[0].ID != "a" {
			t.Fatalf("query %q: %+v", q, found)
		}
	}
}

func TestIndexedSearchRestoreAndDelete(t *testing.T) {
	s := testStore(t)
	for _, m := range []Message{{ID: "a", Subject: "Quarterly proposal", Text: "quoted exact phrase", Tags: []string{"Work"}}, {ID: "b", Subject: "Other"}} {
		if err := s.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{`proposal`, `"exact phrase" tag:Work`, `subject:quarterly -is:read`} {
		got, err := s.SearchIDs(query)
		if err != nil || len(got) != 1 || !got["a"] {
			t.Fatalf("index %q = %v, %v", query, got, err)
		}
	}
	backup := filepath.Join(t.TempDir(), "backup.json")
	if err := s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMessage("a"); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchIDs("proposal")
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	beforeRestoreRevision := s.Snapshot().Revision
	if err = s.Restore(backup); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Revision <= beforeRestoreRevision {
		t.Fatal("restore revision moved backwards")
	}
	got, err = s.SearchIDs("proposal")
	if err != nil || !got["a"] {
		t.Fatal(got, err)
	}
	again, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if len(again.Snapshot().Messages) != 2 {
		t.Fatal("restored database did not persist")
	}
}
func TestAliasIdentityAndPreflightSendFailure(t *testing.T) {
	s := testStore(t)
	a := localAccount()
	a.Identities = []Identity{{ID: "alias", Name: "Acme Accounts", Email: "billing@example.com", ReplyTo: "accounts@example.com"}}
	if err := s.SaveAccount(a, ""); err != nil {
		t.Fatal(err)
	}
	d := Draft{ID: "alias-test", AccountID: a.ID, IdentityID: "alias", To: "recipient@example.net", Text: "Invoice"}
	raw, _, err := BuildMIME(a, d)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseMessage(raw)
	if err != nil || !strings.Contains(parsed.From, "Acme Accounts") || !strings.Contains(parsed.From, "billing@example.com") {
		t.Fatal(parsed.From, err)
	}
	d.IdentityID = "deleted"
	d.Status = "queued"
	if _, err = s.SaveDraft(d); err != nil {
		t.Fatal(err)
	}
	if err = NewEngine(s).Send(context.Background(), d); err == nil {
		t.Fatal("sent with a deleted identity")
	}
	if s.Snapshot().Drafts[0].Status != "failed" {
		t.Fatal("preflight error remained queued for automatic retry")
	}
}

func TestIDLEWatcherStopsOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(memory.New())
	srv.AllowInsecureAuth = true
	go srv.Serve(ln)
	defer srv.Close()
	s := testStore(t)
	a := localAccount()
	a.Username = "username"
	a.IMAPPort = ln.Addr().(*net.TCPAddr).Port
	if err = s.SaveAccount(a, "password"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { NewEngine(s).WatchInbox(ctx, a.ID, func() {}); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("IDLE watcher did not stop")
	}
}
func TestPendingMoveDoesNotDuplicateSourceOrLoseMetadata(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(memory.New())
	srv.AllowInsecureAuth = true
	go srv.Serve(ln)
	defer srv.Close()
	s := testStore(t)
	a := localAccount()
	a.Username = "username"
	a.IMAPPort = ln.Addr().(*net.TCPAddr).Port
	if err = s.SaveAccount(a, "password"); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = e.Sync(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	m := s.Snapshot().Messages[0]
	originalFolder := m.Folder
	m.Folder = "Archive"
	if err = e.UpdateMessage(ctx, m, originalFolder); err == nil {
		t.Fatal("test server unexpectedly supports MOVE")
	}
	m.Tags = []string{"Client"}
	m.Category = "Work"
	if err = s.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	_ = e.Sync(ctx, a.ID)
	snapshot := s.Snapshot()
	if len(snapshot.Messages) != 1 || snapshot.Messages[0].Folder != "Archive" || snapshot.Messages[0].Category != "Work" {
		t.Fatalf("pending move duplicated/lost local state: %+v", snapshot.Messages)
	}
	again, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if len(again.GetPendingMutations()) != 1 {
		t.Fatal("queued remote change was not persisted in SQLite")
	}
}

type capturedMail struct {
	raw   []byte
	rcpts []string
}

func smtpFixture(t *testing.T, dropAfterData bool) (int, <-chan capturedMail) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	captured := make(chan capturedMail, 4)
	var wg sync.WaitGroup
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				r := textproto.NewReader(bufio.NewReader(conn))
				w := bufio.NewWriter(conn)
				reply := func(s string) { fmt.Fprint(w, s+"\r\n"); w.Flush() }
				reply("220 loopback.test ESMTP")
				var rcpts []string
				for {
					line, err := r.ReadLine()
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						reply("250-loopback.test")
						reply("250 AUTH PLAIN LOGIN")
					case strings.HasPrefix(line, "AUTH PLAIN"):
						reply("235 authenticated")
					case strings.HasPrefix(line, "MAIL FROM:"):
						reply("250 ok")
					case strings.HasPrefix(line, "RCPT TO:"):
						rcpts = append(rcpts, line)
						reply("250 ok")
					case line == "DATA":
						reply("354 end with dot")
						raw, err := io.ReadAll(r.DotReader())
						if err != nil {
							return
						}
						captured <- capturedMail{raw, rcpts}
						if dropAfterData {
							return
						}
						reply("250 accepted")
					case line == "QUIT":
						reply("221 bye")
						return
					default:
						reply("500 unsupported")
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { ln.Close(); wg.Wait() })
	return ln.Addr().(*net.TCPAddr).Port, captured
}
func TestSMTPDeliveryPersistsAndRefusesDuplicate(t *testing.T) {
	port, captured := smtpFixture(t, false)
	s := testStore(t)
	a := localAccount()
	a.SMTPPort = port
	if err := s.SaveAccount(a, ""); err != nil {
		t.Fatal(err)
	}
	d, err := s.SaveDraft(Draft{ID: "send-once", AccountID: a.ID, To: "one@example.net", BCC: "private@example.net", Subject: "Delivery test", Text: "First line\n.\nLast line", Status: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(s)
	if err = e.Send(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-captured:
		if len(got.rcpts) != 2 || bytes.Contains(got.raw, []byte("Bcc:")) || !bytes.Contains(got.raw, []byte("First line")) {
			t.Fatal(string(got.raw))
		}
	case <-time.After(time.Second):
		t.Fatal("no SMTP message")
	}
	if err = e.Send(context.Background(), d); err == nil {
		t.Fatal("stale draft caused duplicate send")
	}
	if s.Snapshot().Drafts[0].Status != "sent" || len(s.Snapshot().Messages) != 1 {
		t.Fatal("send receipt not persisted")
	}
}
func TestUncertainDeliveryRequiresReview(t *testing.T) {
	port, captured := smtpFixture(t, true)
	s := testStore(t)
	a := localAccount()
	a.SMTPPort = port
	_ = s.SaveAccount(a, "")
	d, _ := s.SaveDraft(Draft{ID: "uncertain", AccountID: a.ID, To: "one@example.net", Text: "Test", Status: "queued"})
	e := NewEngine(s)
	if err := e.Send(context.Background(), d); err == nil {
		t.Fatal("missing final reply was treated as success")
	}
	<-captured
	if s.Snapshot().Drafts[0].Status != "uncertain" {
		t.Fatal(s.Snapshot().Drafts)
	}
	if err := e.Send(context.Background(), d); err == nil {
		t.Fatal("uncertain mail resent")
	}
}
func TestIMAPSyncAndOfflineReadState(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(memory.New())
	srv.AllowInsecureAuth = true
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	s := testStore(t)
	a := localAccount()
	a.IMAPPort = ln.Addr().(*net.TCPAddr).Port
	a.Username = "username"
	if err = s.SaveAccount(a, "password"); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = e.Sync(ctx, ""); err != nil {
		t.Fatal(err)
	}
	snapshot := s.Snapshot()
	if len(snapshot.Messages) != 1 || snapshot.Messages[0].UID != 6 {
		t.Fatal(snapshot.Messages)
	}
	m := snapshot.Messages[0]
	m.Read = false
	if err = e.UpdateMessage(ctx, m, m.Folder); err != nil {
		t.Fatal(err)
	}
	if err = e.Sync(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Messages[0].Read {
		t.Fatal("read state did not sync")
	}
	if err = e.Sync(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Messages) != 1 {
		t.Fatal("sync duplicated mail")
	}
	srv.Close()
	m.Starred = true
	if err = e.UpdateMessage(ctx, m, m.Folder); err == nil {
		t.Fatal("offline update claimed server success")
	}
	if len(s.GetPendingMutations()) != 1 || !s.Snapshot().Messages[0].Starred {
		t.Fatal("offline action lost")
	}
}
