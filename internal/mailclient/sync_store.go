package mailclient

import (
	"maps"
	"slices"
	"strings"
)

func cloneMessage(m Message) Message {
	m.Raw = slices.Clone(m.Raw)
	m.Tags = slices.Clone(m.Tags)
	m.References = slices.Clone(m.References)
	m.Attachments = slices.Clone(m.Attachments)
	for i := range m.Attachments {
		m.Attachments[i].Data = slices.Clone(m.Attachments[i].Data)
	}
	return m
}

func cloneState(st State) State {
	st.Accounts = slices.Clone(st.Accounts)
	for i := range st.Accounts {
		st.Accounts[i].Identities = slices.Clone(st.Accounts[i].Identities)
	}
	st.Messages = slices.Clone(st.Messages)
	for i := range st.Messages {
		st.Messages[i] = cloneMessage(st.Messages[i])
	}
	st.Drafts = slices.Clone(st.Drafts)
	for i := range st.Drafts {
		d := &st.Drafts[i]
		d.Attachments = slices.Clone(d.Attachments)
		for j := range d.Attachments {
			d.Attachments[j].Data = slices.Clone(d.Attachments[j].Data)
		}
	}
	st.Rules = slices.Clone(st.Rules)
	st.Tags = slices.Clone(st.Tags)
	st.Categories = slices.Clone(st.Categories)
	st.Contacts = slices.Clone(st.Contacts)
	st.Preferences.TrustedSenders = slices.Clone(st.Preferences.TrustedSenders)
	st.TagColors = maps.Clone(st.TagColors)
	st.Templates = maps.Clone(st.Templates)
	st.SavedSearches = maps.Clone(st.SavedSearches)
	st.Folders = maps.Clone(st.Folders)
	for id, folders := range st.Folders {
		st.Folders[id] = slices.Clone(folders)
	}
	st.UIDValidity = maps.Clone(st.UIDValidity)
	for id, folders := range st.UIDValidity {
		st.UIDValidity[id] = maps.Clone(folders)
	}
	st.SyncModSeq = maps.Clone(st.SyncModSeq)
	for id, folders := range st.SyncModSeq {
		st.SyncModSeq[id] = maps.Clone(folders)
	}
	return st
}

// Sync reads only metadata. Bodies and attachments never leave the store here.
type folderSyncState struct {
	validity uint32
	modSeq   uint64
	prefs    Preferences
	local    map[uint32]Message
	retained map[string]Message
}

// IMAP folder names are case sensitive, except for the reserved INBOX name.
func sameIMAPFolder(a, b string) bool {
	return a == b || strings.EqualFold(a, "INBOX") && strings.EqualFold(b, "INBOX")
}

func (s *Store) syncAccounts() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	accounts := slices.Clone(s.state.Accounts)
	for i := range accounts {
		accounts[i].Identities = slices.Clone(accounts[i].Identities)
	}
	return accounts
}

func (s *Store) folderNames(account string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.state.Folders[account])
}

func (s *Store) folderUIDValidity(account, folder string) uint32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.UIDValidity[account][folder]
}

func (s *Store) messageByID(id string) (Message, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.state.Messages {
		if m.ID == id {
			return cloneMessage(m), true
		}
	}
	return Message{}, false
}

func (s *Store) draftByID(id string) (Draft, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, d := range s.state.Drafts {
		if d.ID != id {
			continue
		}
		d.Attachments = slices.Clone(d.Attachments)
		for i := range d.Attachments {
			d.Attachments[i].Data = slices.Clone(d.Attachments[i].Data)
		}
		return d, true
	}
	return Draft{}, false
}

func (s *Store) folderSyncState(account, folder string) folderSyncState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f := folderSyncState{validity: s.state.UIDValidity[account][folder], modSeq: s.state.SyncModSeq[account][folder], prefs: s.state.Preferences, local: map[uint32]Message{}, retained: map[string]Message{}}
	// Only these scalar preferences are needed by sync.
	f.prefs.TrustedSenders = nil
	for _, m := range s.state.Messages {
		if m.AccountID != account || !sameIMAPFolder(m.Folder, folder) {
			continue
		}
		meta := Message{ID: m.ID, AccountID: account, Folder: m.Folder, UID: m.UID, MessageID: m.MessageID, Read: m.Read, Starred: m.Starred}
		if m.UID > 0 {
			f.local[m.UID] = meta
		} else if m.MessageID != "" {
			f.retained[m.MessageID] = meta
		}
	}
	return f
}

// resetFolderUIDs atomically invalidates both UIDs and the incremental cursor.
func (s *Store) resetFolderUIDs(account, folder string, validity uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.state.UIDValidity[account][folder]
	before := map[int]Message{}
	dirty := maps.Clone(s.dirtyMetadata)
	oldModSeq := maps.Clone(s.state.SyncModSeq[account])
	if old != 0 && old != validity {
		for i := range s.state.Messages {
			m := &s.state.Messages[i]
			if m.AccountID == account && sameIMAPFolder(m.Folder, folder) && m.UID > 0 {
				before[i] = *m
				m.UID = 0
				s.dirtyMetadata[m.ID] = true
			}
		}
	}
	if old == validity {
		return nil
	}
	if s.state.UIDValidity == nil {
		s.state.UIDValidity = map[string]map[string]uint32{}
	}
	if s.state.UIDValidity[account] == nil {
		s.state.UIDValidity[account] = map[string]uint32{}
	}
	s.state.UIDValidity[account][folder] = validity
	if s.state.SyncModSeq[account] != nil {
		delete(s.state.SyncModSeq[account], folder)
	}
	if err := s.persistStateLocked(); err != nil {
		for i, m := range before {
			s.state.Messages[i] = m
		}
		s.dirtyMetadata = dirty
		if old == 0 {
			delete(s.state.UIDValidity[account], folder)
		} else {
			s.state.UIDValidity[account][folder] = old
		}
		if s.state.SyncModSeq != nil {
			s.state.SyncModSeq[account] = oldModSeq
		}
		return err
	}
	return nil
}

func (s *Store) setSyncModSeq(account, folder string, modSeq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.SyncModSeq[account][folder] == modSeq {
		return nil
	}
	old, existed := s.state.SyncModSeq[account][folder]
	if s.state.SyncModSeq == nil {
		s.state.SyncModSeq = map[string]map[string]uint64{}
	}
	if s.state.SyncModSeq[account] == nil {
		s.state.SyncModSeq[account] = map[string]uint64{}
	}
	s.state.SyncModSeq[account][folder] = modSeq
	if err := s.persistStateLocked(); err != nil {
		if existed {
			s.state.SyncModSeq[account][folder] = old
		} else {
			delete(s.state.SyncModSeq[account], folder)
		}
		return err
	}
	return nil
}

// saveSyncMetadata merges only server-owned flags/locations into current mail.
// It cannot replace a newer local edit or an outstanding offline operation.
func (s *Store) saveSyncMetadata(account, folder string, flags map[uint32]imapMessageInfo, remote map[uint32]bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := map[string]bool{}
	for _, p := range s.mutations {
		pending[p.MessageID] = true
	}
	changed := false
	before := map[int]Message{}
	dirty := maps.Clone(s.dirtyMetadata)
	for i := range s.state.Messages {
		m := &s.state.Messages[i]
		if m.AccountID != account || !sameIMAPFolder(m.Folder, folder) || m.UID == 0 || pending[m.ID] {
			continue
		}
		if remote != nil && !remote[m.UID] {
			before[i] = *m
			m.Folder, m.UID = "Local archive", 0
		} else if f, ok := flags[m.UID]; ok && (m.Read != f.Read || m.Starred != f.Star) {
			before[i] = *m
			m.Read, m.Starred = f.Read, f.Star
		} else {
			continue
		}
		s.dirtyMetadata[m.ID] = true
		changed = true
	}
	if !changed {
		return nil
	}
	if err := s.persistStateLocked(); err != nil {
		for i, m := range before {
			s.state.Messages[i] = m
		}
		s.dirtyMetadata = dirty
		return err
	}
	return nil
}

// saveSyncMessages commits a bounded download batch and any rule-triggered
// moves together, so an interruption cannot leave a move without its queue item.
func (s *Store) saveSyncMessages(messages []Message, sourceFolder string) error {
	if len(messages) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.state
	overwritten := map[int]Message{}
	mutations, dirty := slices.Clone(s.mutations), maps.Clone(s.dirtyMessages)
	committed := false
	defer func() {
		if !committed {
			for i, m := range overwritten {
				s.state.Messages[i] = m
			}
			previous.Messages = s.state.Messages[:len(previous.Messages)]
			s.state, s.mutations, s.dirtyMessages = previous, mutations, dirty
		}
	}()
	indices := make(map[string]int, len(s.state.Messages))
	for i, m := range s.state.Messages {
		indices[m.ID] = i
	}
	for _, m := range messages {
		if i, ok := indices[m.ID]; ok {
			known := s.state.Messages[i]
			if _, saved := overwritten[i]; !saved && i < len(previous.Messages) {
				overwritten[i] = known
			}
			m.Tags = slices.Clone(known.Tags)
			m.Category, m.FollowUp, m.SnoozedUntil = known.Category, known.FollowUp, known.SnoozedUntil
		}
		m = applyRules(m, s.state.Rules)
		if i, ok := indices[m.ID]; ok {
			s.state.Messages[i] = m
		} else {
			indices[m.ID] = len(s.state.Messages)
			s.state.Messages = append(s.state.Messages, m)
		}
		s.dirtyMessages[m.ID] = true
		s.addContactLocked(m.From)
		s.addContactLocked(m.To)
		s.addContactLocked(m.CC)
		if m.Folder != sourceFolder {
			if err := s.enqueueMessageUpdateLocked(m, sourceFolder); err != nil {
				return err
			}
		}
	}
	if err := s.persistStateLocked(); err != nil {
		return err
	}
	committed = true
	return nil
}
