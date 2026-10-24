package main

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"bentomail/internal/mailclient"
	"github.com/egoist/mygo/ui"
)

func TestSyncProgressRemainsVisibleAcrossWorkspaceChanges(t *testing.T) {
	a := seededApp(t)
	tt := ui.NewTester(a.view, 1000, 680)
	tt.SetTitleBar(ui.TitleBar{Height: 44, Right: 138})
	tt.Key(ui.Ctrl, ui.KeyF)
	tt.Type("proposal")
	a.syncing = true
	a.syncProgress = mailclient.SyncProgress{Account: "alex@example.com", Stage: "Synchronizing messages", AccountIndex: 1, AccountsTotal: 2, Folder: "INBOX", FolderIndex: 2, FoldersTotal: 5, MessagesDone: 42, MessagesTotal: 100}
	canceled := false
	a.syncCancel = func() { canceled = true }
	for _, collapsed := range []bool{false, true} {
		a.prefs.SidebarCollapsed = collapsed
		for _, page := range []string{"mail", "settings", "compose"} {
			a.page = page
			tt.Frame()
			tt.Frame()
			if !tt.HasText("42 of 100 messages checked · 42%") || !tt.HasText("alex@example.com · Account 1 of 2 · INBOX · Folder 2 of 5") {
				t.Fatal(tt.Texts())
			}
			panel, ok := tt.Find("Synchronization progress")
			if !ok || panel.Y < 44 || panel.H <= 0 || panel.Y+panel.H > 680 {
				t.Fatalf("progress is outside the window: %+v", panel)
			}
			if err := tt.Click("Cancel sync"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !canceled {
		t.Fatal("progress cancellation did not run")
	}
	a.page, a.prefs.SidebarCollapsed = "mail", false
	tt.Frame()
	if dir := os.Getenv("BENTOMAIL_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "sync-progress.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	a.syncing = false
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find("Synchronization progress"); ok {
		t.Fatal("finished progress remained visible")
	}
	tt.Key(ui.Ctrl, ui.KeyF)
	tt.Type(" timeline")
	if a.query != "proposal timeline" {
		t.Fatal("search lost its value during progress changes")
	}
}

func TestSyncQueueRejectionAndCancellationClearProgress(t *testing.T) {
	a := seededApp(t)
	a.networkJobs = make(chan work, 1)
	a.networkJobs <- work{}
	a.syncMail()
	if a.syncing || a.syncCancel != nil || a.syncProgress.Stage != "" || !a.statusError {
		t.Fatal("rejected sync remained active")
	}
	<-a.networkJobs
	a.syncMail()
	run := a.syncRun
	a.receiveSyncProgress(run, mailclient.SyncProgress{Stage: "Connecting"})
	if a.syncProgress.Stage != "Connecting" {
		t.Fatal("current sync progress was ignored")
	}
	j := <-a.networkJobs
	j.done(context.Canceled)
	a.receiveSyncProgress(run, mailclient.SyncProgress{Stage: "Stale update"})
	if a.syncing || a.syncCancel != nil || a.syncProgress.Stage != "" || a.statusError || a.status != "Synchronization canceled" {
		t.Fatal("canceled sync retained progress or showed an error")
	}
	a.syncMail()
	a.receiveSyncProgress(run, mailclient.SyncProgress{Stage: "Previous sync"})
	if a.syncProgress.Stage != "Waiting to synchronize" {
		t.Fatal("previous sync overwrote new progress")
	}
	a.closed = true
	a.receiveSyncProgress(a.syncRun, mailclient.SyncProgress{Stage: "Closed window"})
	if a.syncProgress.Stage != "Waiting to synchronize" {
		t.Fatal("progress changed after window closure")
	}
	a.syncCancel()
}

func TestSyncPublishesCommittedMailAndRejectsStaleSnapshots(t *testing.T) {
	a := seededApp(t)
	a.syncing, a.syncRun = true, 3
	old := a.state
	if err := a.store.SaveMessage(mailclient.Message{ID: "during-sync", Folder: "INBOX", Subject: "Available before sync ends"}); err != nil {
		t.Fatal(err)
	}
	fresh := a.store.Snapshot()
	a.receiveSyncState(2, fresh)
	if a.state.Revision != old.Revision {
		t.Fatal("previous run published into current sync")
	}
	a.receiveSyncState(3, fresh)
	if len(a.state.Messages) != len(old.Messages)+1 || !a.syncing {
		t.Fatal("committed mail was not published during sync")
	}
	a.receiveSyncState(3, old)
	if a.state.Revision != fresh.Revision {
		t.Fatal("older snapshot replaced newer state")
	}
	a.syncing = false
	a.receiveSyncState(3, mailclient.State{Revision: fresh.Revision + 10})
	if a.state.Revision != fresh.Revision {
		t.Fatal("finished sync accepted a late snapshot")
	}
	a.syncing, a.closed = true, true
	a.receiveSyncState(3, mailclient.State{Revision: fresh.Revision + 10})
	if a.state.Revision != fresh.Revision {
		t.Fatal("closed window accepted sync state")
	}
}

func TestSyncRequestsCoalesceAndCancellationDoesNotRestart(t *testing.T) {
	a := seededApp(t)
	a.networkJobs = make(chan work, 4)
	a.syncMailFor("target")
	a.syncMailFor("other")
	a.syncMailFor("other")
	if len(a.syncPending) != 1 || !a.syncPending["other"] {
		t.Fatal("account notifications were not coalesced")
	}
	j := <-a.networkJobs
	j.done(context.Canceled)
	if a.syncing || len(a.syncPending) != 0 || len(a.networkJobs) != 0 {
		t.Fatal("cancellation restarted a pending sync")
	}
	a.syncMailFor("target")
	a.syncMailFor("other")
	a.syncMailFor("")
	j = <-a.networkJobs
	j.done(nil)
	if !a.syncing || len(a.networkJobs) != 1 || len(a.syncPending) != 0 {
		t.Fatal("full refresh did not subsume pending account refreshes")
	}
	a.syncCancel()
}

func TestSyncSnapshotsPreserveQueuedMailEdits(t *testing.T) {
	a := seededApp(t)
	a.networkJobs = make(chan work, 4)
	a.syncing, a.syncRun = true, 1
	m := a.state.Messages[0]
	m.Starred = !m.Starred
	a.mutate(m, true)
	first := <-a.networkJobs
	a.receiveSyncState(1, a.store.Snapshot())
	if a.state.Messages[0].Starred != m.Starred {
		t.Fatal("batch snapshot overwrote queued edit")
	}
	m.Read = !m.Read
	a.mutate(m, true)
	second := <-a.networkJobs
	first.done(nil)
	if _, ok := a.mailEdits[m.ID]; !ok {
		t.Fatal("earlier completion removed newer edit")
	}
	a.receiveSyncState(1, a.store.Snapshot())
	if a.state.Messages[0].Read != m.Read {
		t.Fatal("batch snapshot overwrote newer edit")
	}
	second.done(nil)
	if len(a.mailEdits) != 0 {
		t.Fatal("completed edits retained their overlays")
	}
}
