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
