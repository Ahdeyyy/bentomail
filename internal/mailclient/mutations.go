package mailclient

import (
	"encoding/json"
	"time"
)

// queueMessageUpdate commits the visible local change and the remote work together.
func (s *Store) queueMessageUpdate(m Message, oldFolder string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for i, v := range s.state.Messages {
		if v.ID == m.ID {
			s.state.Messages[i] = m
			found = true
			break
		}
	}
	if !found {
		s.state.Messages = append(s.state.Messages, m)
	}
	s.dirtyMessages[m.ID] = true
	compact := Message{ID: m.ID, AccountID: m.AccountID, Folder: m.Folder, Read: m.Read, Starred: m.Starred, Tags: m.Tags}
	data, err := json.Marshal(compact)
	if err != nil {
		return err
	}
	mutation := RemoteMutation{ID: NewID(), Type: "update", AccountID: m.AccountID, MessageID: m.ID, UID: m.UID, Folder: m.Folder, OldFolder: oldFolder, Data: data, CreatedAt: time.Now()}
	for i, old := range s.mutations {
		if old.Type == "update" && old.MessageID == m.ID {
			mutation.ID = old.ID
			mutation.UID = old.UID
			mutation.OldFolder = old.OldFolder
			mutation.CreatedAt = old.CreatedAt
			s.mutations[i] = mutation
			return s.persistStateLocked()
		}
	}
	s.mutations = append(s.mutations, mutation)
	return s.persistStateLocked()
}
