package mailclient

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
)

const syncBatchMessages = 25
const syncBatchBytes = 8 << 20
const syncAccountWorkers = 3

// Reports are serialized even though accounts run concurrently. Callbacks must
// dispatch UI changes and must not call back into Engine. AccountIndex is the
// account's stable position, not a count of completed accounts.
func (e *Engine) SyncWithProgress(ctx context.Context, accountID string, report func(SyncProgress)) error {
	var accounts []Account
	for _, a := range e.store.syncAccounts() {
		if a.Enabled && (accountID == "" || accountID == a.ID) {
			accounts = append(accounts, a)
		}
	}
	var reportMu sync.Mutex
	publish := func(p SyncProgress) {
		if report != nil {
			reportMu.Lock()
			defer reportMu.Unlock()
			report(p)
		}
	}
	jobs := make(chan int, len(accounts))
	for i := range accounts {
		jobs <- i
	}
	close(jobs)
	errs := make([]error, len(accounts))
	var workers sync.WaitGroup
	for range min(syncAccountWorkers, len(accounts)) {
		workers.Go(func() {
			for i := range jobs {
				a := accounts[i]
				actx, cancel := context.WithTimeout(ctx, 10*time.Minute)
				progress := SyncProgress{AccountID: a.ID, Account: a.Email, AccountIndex: i + 1, AccountsTotal: len(accounts)}
				release, err := e.lockAccount(actx, a.ID)
				if err == nil {
					if current, ok := e.store.GetAccount(a.ID); ok && current.Enabled {
						err = e.syncAccount(actx, current, &progress, publish)
					}
					release()
				}
				cancel()
				if err != nil {
					errs[i] = fmt.Errorf("%s: %w", a.Email, err)
				}
			}
		})
	}
	workers.Wait()
	return errors.Join(append(errs, ctx.Err())...)
}

// Serialize operations on a single account without making unrelated accounts
// wait. Waiting for an account also respects cancellation.
func (e *Engine) lockAccount(ctx context.Context, account string) (func(), error) {
	e.mu.Lock()
	if e.accountLocks == nil {
		e.accountLocks = map[string]chan struct{}{}
	}
	lock := e.accountLocks[account]
	if lock == nil {
		lock = make(chan struct{}, 1)
		e.accountLocks[account] = lock
	}
	e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *Engine) syncAccount(ctx context.Context, a Account, progress *SyncProgress, report func(SyncProgress)) error {
	publish := func(stage string) { progress.Stage = stage; report(*progress) }
	publish("Connecting")
	c, err := e.connect(ctx, a)
	if err != nil {
		return err
	}
	defer c.Close()
	publish("Listing folders")
	folders, err := c.ListFolders(ctx)
	if err != nil {
		return err
	}
	if len(folders) == 0 {
		folders = []string{"INBOX"}
	}
	// Stable order for the remaining folders, with Inbox available first.
	slices.SortStableFunc(folders, func(a, b string) int {
		if strings.EqualFold(a, "INBOX") {
			return -1
		}
		if strings.EqualFold(b, "INBOX") {
			return 1
		}
		return 0
	})
	if err = e.store.SetFolderList(a.ID, folders); err != nil {
		return err
	}
	progress.FoldersTotal = len(folders)
	publish("Applying queued changes")
	var errs []error
	if err = e.processMutations(ctx, c, a.ID); err != nil {
		errs = append(errs, err)
	}
	for i, folder := range folders {
		if err = ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		progress.Folder, progress.FolderIndex = folder, i+1
		progress.MessagesDone, progress.MessagesTotal = 0, 0
		publish("Checking messages")
		if err = e.syncFolder(ctx, c, a.ID, folder, progress, publish); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", folder, err))
		}
	}
	publish("Finishing account")
	return errors.Join(errs...)
}

func (e *Engine) syncFolder(ctx context.Context, c *imapClient, account, folder string, progress *SyncProgress, publish func(string)) error {
	validity, count, modSeq, err := c.SelectForSync(ctx, folder)
	if err != nil {
		return err
	}
	if err = e.store.resetFolderUIDs(account, folder, validity); err != nil {
		return err
	}
	state := e.store.folderSyncState(account, folder)
	var recent map[uint32]bool
	if !state.prefs.FullSync && state.prefs.RecentDays > 0 {
		recent, err = c.recentUIDs(time.Now().AddDate(0, 0, -state.prefs.RecentDays))
		if err != nil {
			return err
		}
	}
	flags, remote, err := c.syncMessageInfo(ctx, count, state.modSeq, modSeq, state.local, recent)
	if err != nil {
		return err
	}
	progress.MessagesTotal = len(remote)
	publish("Synchronizing messages")
	// Apply only metadata, with no body serialization or search-index work.
	if err = e.store.saveSyncMetadata(account, folder, flags, nil); err != nil {
		return err
	}
	pendingUID := map[uint32]bool{}
	for _, p := range e.store.GetPendingMutations() {
		if p.AccountID == account && p.Type == "update" && p.OldFolder == folder {
			pendingUID[p.UID] = true
		}
	}
	var downloads []imapMessageInfo
	for uid := range remote {
		if pendingUID[uid] {
			continue
		}
		if _, ok := state.local[uid]; ok {
			continue
		}
		if recent != nil && !recent[uid] {
			continue
		}
		if info, ok := flags[uid]; ok {
			downloads = append(downloads, info)
		}
	}
	// Newest mail first; bounded command sizes also keep cancellation responsive.
	slices.SortFunc(downloads, func(a, b imapMessageInfo) int {
		if a.UID > b.UID {
			return -1
		}
		if a.UID < b.UID {
			return 1
		}
		return 0
	})
	progress.MessagesDone = len(remote) - len(downloads)
	var errs []error
	for len(downloads) > 0 {
		if err = ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		n, size := 0, uint64(0)
		for n < len(downloads) && n < syncBatchMessages {
			next := uint64(downloads[n].Size)
			if n > 0 && size+next > syncBatchBytes {
				break
			}
			size += next
			n++
		}
		batch := downloads[:n]
		downloads = downloads[n:]
		uids := make([]uint32, len(batch))
		for i := range batch {
			uids[i] = batch[i].UID
		}
		var messages []Message
		bufferedBytes := 0
		flush := func() error {
			if len(messages) == 0 {
				return nil
			}
			if err := e.store.saveSyncMessages(messages, folder); err != nil {
				return err
			}
			messages, bufferedBytes = nil, 0
			publish("Saving downloaded mail")
			return nil
		}
		err = c.FetchRFC822Batch(ctx, uids, func(uid uint32, raw []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			m, err := ParseMessage(raw)
			if err != nil {
				return err
			}
			info := flags[uid]
			m.AccountID, m.Folder, m.UID = account, folder, uid
			m.Read, m.Starred = info.Read, info.Star
			if known, ok := state.retained[m.MessageID]; ok {
				m.ID = known.ID
				delete(state.retained, m.MessageID)
			}
			messages = append(messages, m)
			bufferedBytes += len(raw)
			progress.MessagesDone++
			publish("Synchronizing messages")
			if len(messages) >= syncBatchMessages || bufferedBytes >= syncBatchBytes {
				return flush()
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
		// Preserve completed downloads even if another message failed/cancelled.
		if err = flush(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		publish("Saving downloaded mail")
	}
	if err = ctx.Err(); err != nil {
		return errors.Join(append(errs, err)...)
	}
	progress.MessagesDone = len(remote)
	publish("Updating local archive")
	if err = e.store.saveSyncMetadata(account, folder, nil, remote); err != nil {
		return errors.Join(append(errs, err)...)
	}
	// Advance only after all downloads and local changes have been committed.
	if len(errs) == 0 {
		return e.store.setSyncModSeq(account, folder, modSeq)
	}
	return errors.Join(errs...)
}

func (c *imapClient) syncMessageInfo(ctx context.Context, count int, since, current uint64, local map[uint32]Message, recent map[uint32]bool) (map[uint32]imapMessageInfo, map[uint32]bool, error) {
	flags, remote := map[uint32]imapMessageInfo{}, map[uint32]bool{}
	if count == 0 {
		return flags, remote, nil
	}
	full := func() (map[uint32]imapMessageInfo, map[uint32]bool, error) {
		infos, err := c.FetchUIDAndFlags(ctx)
		flags, remote = map[uint32]imapMessageInfo{}, map[uint32]bool{}
		for _, f := range infos {
			flags[f.UID], remote[f.UID] = f, true
		}
		return flags, remote, err
	}
	if since == 0 || current == 0 || current < since {
		return full()
	}
	// UID membership is still reconciled every time, so expunged mail is safely
	// retained in Local archive even without QRESYNC support.
	uids, err := c.c.UidSearch(imap.NewSearchCriteria())
	if err != nil {
		return nil, nil, err
	}
	for _, uid := range uids {
		remote[uid] = true
	}
	if current > since {
		set := new(imap.SeqSet)
		set.AddRange(1, 0)
		changed, err := c.fetchUIDAndFlags(set, since)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			c.noCondstore = true
			return full()
		}
		for _, f := range changed {
			flags[f.UID] = f
		}
	}
	// Include previously skipped/failed bodies when widening the recent range,
	// even when their server flags haven't changed since the saved cursor.
	missing := new(imap.SeqSet)
	for uid := range remote {
		if _, ok := local[uid]; ok {
			continue
		}
		if _, ok := flags[uid]; ok {
			continue
		}
		if recent == nil || recent[uid] {
			missing.AddNum(uid)
		}
	}
	if !missing.Empty() {
		infos, err := c.fetchUIDAndFlags(missing, 0)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range infos {
			flags[f.UID] = f
		}
	}
	return flags, remote, nil
}
