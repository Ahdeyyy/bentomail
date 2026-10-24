package mailclient

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"
)

func TestSyncReportsActualAccountFolderAndMessageProgress(t *testing.T) {
	backend := memory.New()
	user, err := backend.Login(nil, "username", "password")
	if err != nil {
		t.Fatal(err)
	}
	if err := user.CreateMailbox("Empty"); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(backend)
	srv.AllowInsecureAuth = true
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	s := testStore(t)
	a := localAccount()
	a.IMAPPort, a.Username = ln.Addr().(*net.TCPAddr).Port, "username"
	if err := s.SaveAccount(a, "password"); err != nil {
		t.Fatal(err)
	}
	disabled := a
	disabled.ID, disabled.Enabled = "disabled", false
	if err := s.SaveAccount(disabled, "password"); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Run twice: already cached messages must also advance checked counts.
	for run := 0; run < 2; run++ {
		var updates []SyncProgress
		err := e.SyncWithProgress(ctx, "", func(p SyncProgress) { updates = append(updates, p) })
		if err != nil {
			t.Fatal(err)
		}
		if len(updates) == 0 || updates[0].Stage != "Connecting" || updates[len(updates)-1].Stage != "Finishing account" {
			t.Fatalf("missing connection/completion progress: %+v", updates)
		}
		folders := map[string]bool{}
		inboxStarted, inboxDone, emptyDone := false, false, false
		for _, p := range updates {
			if p.AccountID != a.ID || p.Account != a.Email || p.AccountIndex != 1 || p.AccountsTotal != 1 {
				t.Fatalf("progress included a disabled account or wrong identity: %+v", p)
			}
			if p.Folder == "" {
				continue
			}
			if p.FoldersTotal != 2 || p.FolderIndex < 1 || p.FolderIndex > 2 || p.MessagesDone > p.MessagesTotal {
				t.Fatalf("invalid folder/count progress: %+v", p)
			}
			folders[p.Folder] = true
			if p.Folder == "INBOX" && p.Stage == "Synchronizing messages" && p.MessagesDone == 0 && p.MessagesTotal == 1 {
				inboxStarted = true
			}
			if p.Folder == "INBOX" && p.Stage == "Updating local archive" && p.MessagesDone == 1 && p.MessagesTotal == 1 {
				inboxDone = true
			}
			if p.Folder == "Empty" && p.Stage == "Updating local archive" && p.MessagesDone == 0 && p.MessagesTotal == 0 {
				emptyDone = true
			}
		}
		if len(folders) != 2 || !inboxStarted || !inboxDone || !emptyDone || len(s.Snapshot().Messages) != 1 {
			t.Fatalf("progress missed actual work: %+v", updates)
		}
	}
	called := false
	if err := e.SyncWithProgress(ctx, "other-account", func(SyncProgress) { called = true }); err != nil || called {
		t.Fatal("account filter was ignored")
	}
	cctx, stop := context.WithCancel(ctx)
	stop()
	if err := e.SyncWithProgress(cctx, "", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled synchronization returned %v", err)
	}
}
