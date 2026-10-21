package mailclient

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func (s *Store) openDatabase() error {
	db, err := sql.Open("sqlite", filepath.Join(s.dir, "mail.sqlite"))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	s.db = db
	s.dirtyMessages = map[string]bool{}
	s.dirtyDrafts = map[string]bool{}
	fail := func(err error) error { db.Close(); return err }
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS metadata(key TEXT PRIMARY KEY,value BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS messages(id TEXT PRIMARY KEY,payload BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS drafts(id TEXT PRIMARY KEY,payload BLOB NOT NULL);
 CREATE VIRTUAL TABLE IF NOT EXISTS message_search USING fts5(id UNINDEXED,body,tokenize='trigram');`); err != nil {
		return fail(err)
	}
	var data []byte
	err = db.QueryRow(`SELECT value FROM metadata WHERE key='state'`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		for _, m := range s.state.Messages {
			s.dirtyMessages[m.ID] = true
		}
		for _, d := range s.state.Drafts {
			s.dirtyDrafts[d.ID] = true
		}
		return s.persistStateLocked()
	}
	if err != nil {
		return fail(err)
	}
	var state State
	if err = json.Unmarshal(data, &state); err != nil {
		return fail(fmt.Errorf("invalid database metadata: %w", err))
	}
	var mutations []byte
	if e := db.QueryRow(`SELECT value FROM metadata WHERE key='mutations'`).Scan(&mutations); e == nil {
		if err = json.Unmarshal(mutations, &s.mutations); err != nil {
			return fail(err)
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return fail(e)
	}
	if state.Version > 1 {
		return fail(errors.New("database requires a newer Bentomail"))
	}
	if state.Folders == nil {
		state.Folders = map[string][]string{}
	}
	if state.SavedSearches == nil {
		state.SavedSearches = map[string]string{}
	}
	if state.Templates == nil {
		state.Templates = map[string]string{}
	}
	if state.UIDValidity == nil {
		state.UIDValidity = map[string]map[string]uint32{}
	}
	rows, err := db.Query(`SELECT payload FROM messages ORDER BY rowid`)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		if err = rows.Scan(&data); err != nil {
			rows.Close()
			return fail(err)
		}
		var m Message
		if err = json.Unmarshal(data, &m); err != nil {
			rows.Close()
			return fail(err)
		}
		state.Messages = append(state.Messages, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail(err)
	}
	rows, err = db.Query(`SELECT payload FROM drafts ORDER BY rowid`)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		if err = rows.Scan(&data); err != nil {
			rows.Close()
			return fail(err)
		}
		var d Draft
		if err = json.Unmarshal(data, &d); err != nil {
			rows.Close()
			return fail(err)
		}
		state.Drafts = append(state.Drafts, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail(err)
	}
	s.state = state
	return nil
}
func (s *Store) persistDatabaseLocked() error {
	s.state.Revision++
	state := s.state
	state.Version = 1
	state.Messages = nil
	state.Drafts = nil
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO metadata(key,value)VALUES('state',?) ON CONFLICT(key)DO UPDATE SET value=excluded.value`, data); err != nil {
		return err
	}
	mutationData, err := json.Marshal(s.mutations)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO metadata(key,value)VALUES('mutations',?) ON CONFLICT(key)DO UPDATE SET value=excluded.value`, mutationData); err != nil {
		return err
	}
	for id := range s.dirtyMessages {
		var found *Message
		for i := range s.state.Messages {
			if s.state.Messages[i].ID == id {
				found = &s.state.Messages[i]
				break
			}
		}
		if _, err = tx.Exec(`DELETE FROM message_search WHERE id=?`, id); err != nil {
			return err
		}
		if found == nil {
			if _, err = tx.Exec(`DELETE FROM messages WHERE id=?`, id); err != nil {
				return err
			}
			continue
		}
		raw, err := json.Marshal(found)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO messages(id,payload)VALUES(?,?) ON CONFLICT(id)DO UPDATE SET payload=excluded.payload`, id, raw); err != nil {
			return err
		}
		body := found.Subject + " " + found.From + " " + found.To + " " + found.CC + " " + found.Text + " " + found.HTML
		for _, at := range found.Attachments {
			body += " " + at.Name
		}
		if _, err = tx.Exec(`INSERT INTO message_search(id,body)VALUES(?,?)`, id, body); err != nil {
			return err
		}
	}
	for id := range s.dirtyDrafts {
		var found *Draft
		for i := range s.state.Drafts {
			if s.state.Drafts[i].ID == id {
				found = &s.state.Drafts[i]
				break
			}
		}
		if found == nil {
			if _, err = tx.Exec(`DELETE FROM drafts WHERE id=?`, id); err != nil {
				return err
			}
			continue
		}
		raw, err := json.Marshal(found)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO drafts(id,payload)VALUES(?,?) ON CONFLICT(id)DO UPDATE SET payload=excluded.payload`, id, raw); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	clear(s.dirtyMessages)
	clear(s.dirtyDrafts)
	return nil
}
func (s *Store) SearchIDs(query string) (map[string]bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var candidates map[string]bool
	// Trigram candidates preserve substring semantics. Operators and short terms are evaluated below.
	for _, f := range parseFilters(query) {
		if f.negative || f.field != "" || len([]rune(f.val)) < 3 {
			continue
		}
		rows, err := s.db.Query(`SELECT id FROM message_search WHERE body MATCH ?`, `"`+strings.ReplaceAll(f.val, `"`, `""`)+`"`)
		if err != nil {
			return nil, err
		}
		candidates = map[string]bool{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			candidates[id] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		break
	}
	result := map[string]bool{}
	filters := parseFilters(query)
	for _, m := range s.state.Messages {
		if candidates != nil && !candidates[m.ID] {
			continue
		}
		if matchMessage(m, filters) {
			result[m.ID] = true
		}
	}
	return result, nil
}
func (s *Store) RebuildIndex() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.state.Messages {
		s.dirtyMessages[m.ID] = true
	}
	return s.persistStateLocked()
}
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
