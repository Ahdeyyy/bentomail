package mailclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
)

type measuredBackend struct {
	backend.Backend
	bodies     atomic.Int32
	beforeBody func()
}

func (b *measuredBackend) Login(info *imap.ConnInfo, user, pass string) (backend.User, error) {
	u, err := b.Backend.Login(info, user, pass)
	if err != nil {
		return nil, err
	}
	return &measuredUser{User: u, stats: b}, nil
}

type measuredUser struct {
	backend.User
	stats *measuredBackend
}

func (u *measuredUser) GetMailbox(name string) (backend.Mailbox, error) {
	m, err := u.User.GetMailbox(name)
	if err != nil {
		return nil, err
	}
	return &measuredMailbox{Mailbox: m, stats: u.stats}, nil
}

type measuredMailbox struct {
	backend.Mailbox
	stats *measuredBackend
}

func (m *measuredMailbox) ListMessages(uid bool, set *imap.SeqSet, items []imap.FetchItem, ch chan<- *imap.Message) error {
	for _, item := range items {
		if section, err := imap.ParseBodySectionName(item); err == nil && section.Peek {
			m.stats.bodies.Add(1)
			if m.stats.beforeBody != nil {
				m.stats.beforeBody()
			}
			break
		}
	}
	return m.Mailbox.ListMessages(uid, set, items, ch)
}

func measuredSyncServer(t *testing.T, count int, before func()) (int, *measuredBackend, *memory.Mailbox) {
	t.Helper()
	mem := memory.New()
	u, _ := mem.Login(nil, "username", "password")
	box, _ := u.GetMailbox("INBOX")
	mb := box.(*memory.Mailbox)
	mb.Messages = nil
	for i := 1; i <= count; i++ {
		raw := []byte(fmt.Sprintf("From: a@example.com\r\nTo: b@example.com\r\nMessage-ID: <batch-%d@example.com>\r\nSubject: Batch %d\r\nContent-Type: text/plain\r\n\r\nOffline body %d", i, i, i))
		mb.Messages = append(mb.Messages, &memory.Message{Uid: uint32(i), Date: time.Now(), Size: uint32(len(raw)), Body: raw})
	}
	if err := u.CreateMailbox("A before Inbox"); err != nil {
		t.Fatal(err)
	}
	stats := &measuredBackend{Backend: mem, beforeBody: before}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(stats)
	srv.AllowInsecureAuth = true
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port, stats, mb
}

func addSyncAccount(t *testing.T, s *Store, id string, port int) Account {
	t.Helper()
	a := localAccount()
	a.ID, a.Email, a.Username, a.IMAPPort = id, id+"@example.com", "username", port
	if err := s.SaveAccount(a, "password"); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSyncBatchesDownloadsPrioritizesInboxAndResumesCancellation(t *testing.T) {
	port, stats, _ := measuredSyncServer(t, 61, nil)
	s := testStore(t)
	a := addSyncAccount(t, s, "batch", port)
	e := NewEngine(s)
	ctx, cancel := context.WithCancel(context.Background())
	firstFolder := ""
	err := e.SyncWithProgress(ctx, a.ID, func(p SyncProgress) {
		if firstFolder == "" && p.Folder != "" {
			firstFolder = p.Folder
		}
		if p.Stage == "Saving downloaded mail" {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel result: %v", err)
	}
	if firstFolder != "INBOX" {
		t.Fatalf("first folder: %s", firstFolder)
	}
	if got := len(s.Snapshot().Messages); got != 25 {
		t.Fatalf("committed %d messages before cancellation, want 25", got)
	}
	again, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Snapshot().Messages) != 25 {
		t.Fatal("completed batch was not durable")
	}
	again.Close()
	if err = e.Sync(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Messages) != 61 {
		t.Fatal("resume lost or duplicated messages")
	}
	if got := stats.bodies.Load(); got != 3 {
		t.Fatalf("downloaded with %d body commands; want 3 for 61 messages", got)
	}
	if err = e.Sync(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if stats.bodies.Load() != 3 {
		t.Fatal("unchanged refresh downloaded bodies again")
	}
}

func TestSyncMetadataPersistsWithoutRewritingBodiesOrIndex(t *testing.T) {
	s := testStore(t)
	for _, m := range []Message{{ID: "one", AccountID: "a", Folder: "INBOX", UID: 1, Subject: "Searchable", Raw: []byte("raw"), Attachments: []Attachment{{Name: "file", Data: []byte("attachment")}}}, {ID: "two", AccountID: "a", Folder: "INBOX", UID: 2, Subject: "Other"}} {
		if err := s.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	var before []byte
	var rowid int64
	s.db.QueryRow(`SELECT payload FROM messages WHERE id='one'`).Scan(&before)
	s.db.QueryRow(`SELECT rowid FROM message_search WHERE id='one'`).Scan(&rowid)
	revision := s.Snapshot().Revision
	flags := map[uint32]imapMessageInfo{1: {UID: 1, Read: true, Star: true}, 2: {UID: 2, Read: true}}
	if err := s.saveSyncMetadata("a", "INBOX", flags, nil); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Revision != revision+1 {
		t.Fatal("metadata batch did not use one transaction")
	}
	var after []byte
	var afterRow int64
	s.db.QueryRow(`SELECT payload FROM messages WHERE id='one'`).Scan(&after)
	s.db.QueryRow(`SELECT rowid FROM message_search WHERE id='one'`).Scan(&afterRow)
	if !bytes.Equal(before, after) || rowid != afterRow {
		t.Fatal("flag refresh rewrote payload or index")
	}
	if err := s.saveSyncMetadata("a", "INBOX", nil, map[uint32]bool{1: true}); err != nil {
		t.Fatal(err)
	}
	again, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	st := again.Snapshot()
	if !st.Messages[0].Read || !st.Messages[0].Starred || string(st.Messages[0].Attachments[0].Data) != "attachment" {
		t.Fatal("metadata overlay or offline content lost on reopen")
	}
	if st.Messages[1].Folder != "Local archive" || st.Messages[1].UID != 0 {
		t.Fatal("remote removal did not preserve local copy")
	}
	found, err := again.SearchIDs("searchable is:read")
	if err != nil || !found["one"] {
		t.Fatal("search did not use current flags", found, err)
	}
	// A later full save must supersede the metadata overlay durably.
	m := st.Messages[0]
	m.Read = false
	if err = s.SaveMessage(m); err != nil {
		t.Fatal(err)
	}
	var overlays int
	s.db.QueryRow(`SELECT count(*) FROM message_state WHERE id='one'`).Scan(&overlays)
	if overlays != 0 {
		t.Fatal("full save left an obsolete metadata overlay")
	}
}

func TestSyncRuleMovesAndRetainedMetadataCommitInOneBatch(t *testing.T) {
	s := testStore(t)
	if err := s.SaveMessage(Message{ID: "retained", AccountID: "a", Folder: "INBOX", MessageID: "retained@example.com", Tags: []string{"Personal"}, Category: "Work"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRule(Rule{ID: "rule", Enabled: true, Field: "subject", Contains: "Move", Folder: "Archive", Tag: "Rule"}); err != nil {
		t.Fatal(err)
	}
	revision := s.Snapshot().Revision
	batch := []Message{{ID: "retained", AccountID: "a", Folder: "INBOX", MessageID: "retained@example.com", UID: 9, Subject: "Move me"}, {ID: "new", AccountID: "a", Folder: "INBOX", UID: 10, Subject: "Move too"}}
	if err := s.saveSyncMessages(batch, "INBOX"); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Revision != revision+1 {
		t.Fatal("messages and rule moves were not committed together")
	}
	again, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	st := again.Snapshot()
	if len(st.Messages) != 2 || len(again.GetPendingMutations()) != 2 {
		t.Fatal("rule moves lost their queue items")
	}
	m := st.Messages[0]
	if m.Folder != "Archive" || m.Category != "Work" || !slices.Contains(m.Tags, "Personal") || !slices.Contains(m.Tags, "Rule") {
		t.Fatal("retained metadata lost", m)
	}
}

func TestSyncAccountConcurrencyIsBoundedAndReportsAreSerialized(t *testing.T) {
	s := testStore(t)
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var active, maxActive atomic.Int32
	before := func() {
		n := active.Add(1)
		for old := maxActive.Load(); n > old && !maxActive.CompareAndSwap(old, n); old = maxActive.Load() {
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
	}
	for i := 0; i < 4; i++ {
		port, _, _ := measuredSyncServer(t, 1, before)
		addSyncAccount(t, s, fmt.Sprint(i), port)
	}
	done := make(chan error, 1)
	var callbacks atomic.Int32
	var overlapping atomic.Bool
	go func() {
		done <- NewEngine(s).SyncWithProgress(context.Background(), "", func(SyncProgress) {
			if callbacks.Add(1) != 1 {
				overlapping.Store(true)
			}
			callbacks.Add(-1)
		})
	}()
	for range 3 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("accounts did not overlap")
		}
	}
	select {
	case <-entered:
		t.Fatal("more than three account workers started")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("sync workers did not finish")
	}
	if maxActive.Load() != 3 || overlapping.Load() {
		t.Fatal("concurrency/report bounds failed", maxActive.Load(), overlapping.Load())
	}
}

func TestSyncAccountLockCancellationAndSnapshotIsolation(t *testing.T) {
	s := testStore(t)
	e := NewEngine(s)
	release, err := e.lockAccount(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = e.lockAccount(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	other, err := e.lockAccount(context.Background(), "b")
	if err != nil {
		t.Fatal(err)
	}
	other()
	if err = s.SaveMessage(Message{ID: "m", AccountID: "a", Folder: "INBOX", UID: 1, Raw: []byte("raw"), Tags: []string{"Tag"}, Attachments: []Attachment{{Data: []byte("attachment")}}}); err != nil {
		t.Fatal(err)
	}
	if err = s.setSyncModSeq("a", "INBOX", 123); err != nil {
		t.Fatal(err)
	}
	st := s.Snapshot()
	st.Messages[0].Raw[0] = 'X'
	st.Messages[0].Tags[0] = "Changed"
	st.Messages[0].Attachments[0].Data[0] = 'X'
	st.SyncModSeq["a"]["INBOX"] = 999
	copy := s.Snapshot()
	if string(copy.Messages[0].Raw) != "raw" || copy.Messages[0].Tags[0] != "Tag" || string(copy.Messages[0].Attachments[0].Data) != "attachment" || copy.SyncModSeq["a"]["INBOX"] != 123 {
		t.Fatal("snapshot aliases store memory")
	}
	f := s.folderSyncState("a", "INBOX")
	if len(f.local[1].Raw) != 0 || len(f.local[1].Attachments) != 0 {
		t.Fatal("sync metadata copied payload")
	}
}

func TestSyncDatabaseFailuresRollBackBatchesMetadataAndCursors(t *testing.T) {
	s := testStore(t)
	if err := s.SaveMessage(Message{ID: "old", AccountID: "a", Folder: "INBOX", UID: 1, Subject: "Before"}); err != nil {
		t.Fatal(err)
	}
	if err := s.resetFolderUIDs("a", "INBOX", 1); err != nil {
		t.Fatal(err)
	}
	if err := s.setSyncModSeq("a", "INBOX", 10); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRule(Rule{ID: "move", Enabled: true, Contains: "Move", Folder: "Archive"}); err != nil {
		t.Fatal(err)
	}
	before := s.Snapshot()
	// Fail within the transaction after its metadata writes have executed.
	if _, err := s.db.Exec(`CREATE TRIGGER fail_message BEFORE INSERT ON messages BEGIN SELECT RAISE(ABORT,'synthetic write failure'); END`); err != nil {
		t.Fatal(err)
	}
	err := s.saveSyncMessages([]Message{{ID: "old", AccountID: "a", Folder: "INBOX", UID: 1, Subject: "Move old", Raw: []byte("rewritten")}, {ID: "new", AccountID: "a", Folder: "INBOX", UID: 2, Subject: "Move", From: "new@example.com"}}, "INBOX")
	if err == nil {
		t.Fatal("batch write failure was hidden")
	}
	if !reflect.DeepEqual(before, s.Snapshot()) || len(s.GetPendingMutations()) != 0 {
		t.Fatal("failed batch changed visible state or its queue")
	}
	if _, err = s.db.Exec(`DROP TRIGGER fail_message; CREATE TRIGGER fail_metadata BEFORE UPDATE ON metadata BEGIN SELECT RAISE(ABORT,'synthetic metadata failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.saveSyncMetadata("a", "INBOX", map[uint32]imapMessageInfo{1: {Read: true}}, nil); err == nil {
		t.Fatal("metadata failure was hidden")
	}
	if err = s.setSyncModSeq("a", "INBOX", 11); err == nil {
		t.Fatal("cursor failure was hidden")
	}
	if err = s.resetFolderUIDs("a", "INBOX", 2); err == nil {
		t.Fatal("UID reset failure was hidden")
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("failed metadata/UID reset advanced visible state")
	}
	again, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if !reflect.DeepEqual(before, again.Snapshot()) {
		t.Fatal("failed transaction changed durable state")
	}
}

func TestSyncKeepsCaseSensitiveFoldersSeparateAndPreservesPendingFlags(t *testing.T) {
	s := testStore(t)
	for _, m := range []Message{{ID: "upper", AccountID: "a", Folder: "Work", UID: 1}, {ID: "lower", AccountID: "a", Folder: "work", UID: 1}} {
		if err := s.SaveMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.folderSyncState("a", "Work").local) != 1 {
		t.Fatal("case-sensitive folders were merged")
	}
	if err := s.saveSyncMetadata("a", "Work", map[uint32]imapMessageInfo{1: {Read: true}}, nil); err != nil {
		t.Fatal(err)
	}
	st := s.Snapshot()
	if !st.Messages[0].Read || st.Messages[1].Read {
		t.Fatal("flag change leaked into a different folder")
	}
	m := st.Messages[1]
	m.Starred = true
	if err := s.queueMessageUpdate(m, m.Folder); err != nil {
		t.Fatal(err)
	}
	if err := s.saveSyncMetadata("a", "work", map[uint32]imapMessageInfo{1: {}}, map[uint32]bool{}); err != nil {
		t.Fatal(err)
	}
	m = s.Snapshot().Messages[1]
	if !m.Starred || m.Folder != "work" || m.UID != 1 {
		t.Fatal("server refresh overwrote pending local changes")
	}
}

func TestSyncMetadataMigrationAndBackupRestore(t *testing.T) {
	s := testStore(t)
	if err := s.SaveMessage(Message{ID: "legacy", AccountID: "a", Folder: "INBOX", UID: 1, Subject: "Portable", Raw: []byte("offline original")}); err != nil {
		t.Fatal(err)
	}
	// Simulate the schema of an existing installation before metadata rows.
	if _, err := s.db.Exec(`DROP TABLE message_state`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	upgraded, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if err = upgraded.saveSyncMetadata("a", "INBOX", map[uint32]imapMessageInfo{1: {Read: true, Star: true}}, nil); err != nil {
		t.Fatal(err)
	}
	if err = upgraded.setSyncModSeq("a", "INBOX", 1<<34); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.json")
	if err = upgraded.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err = upgraded.DeleteMessage("legacy"); err != nil {
		t.Fatal(err)
	}
	if err = upgraded.Restore(backup); err != nil {
		t.Fatal(err)
	}
	again, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	st := again.Snapshot()
	if len(st.Messages) != 1 || !st.Messages[0].Read || !st.Messages[0].Starred || string(st.Messages[0].Raw) != "offline original" || st.SyncModSeq["a"]["INBOX"] != 1<<34 {
		t.Fatal("migration/backup lost metadata or content")
	}
}

func BenchmarkSyncState(b *testing.B) {
	s := &Store{state: State{Messages: make([]Message, 200)}}
	for i := range s.state.Messages {
		s.state.Messages[i] = Message{ID: fmt.Sprint(i), AccountID: "a", Folder: "INBOX", UID: uint32(i + 1), Raw: bytes.Repeat([]byte("mail"), 4096), Attachments: []Attachment{{Data: bytes.Repeat([]byte("file"), 4096)}}}
	}
	b.Run("previous_JSON_snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			raw, _ := json.Marshal(s.state)
			var st State
			_ = json.Unmarshal(raw, &st)
		}
	})
	b.Run("folder_metadata", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.folderSyncState("a", "INBOX")
		}
	})
	b.Run("UI_snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.Snapshot()
		}
	})
}
