package mailclient

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type storedSecret struct {
	RefreshToken string    `json:"refresh_token,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	Expires      time.Time `json:"expires,omitempty"`
	Password     string    `json:"password,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
}

// RemoteMutation tracks an action queued while offline.
type RemoteMutation struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	Type      string    `json:"type"` // "flags", "delete", "move", "create_folder", "append"
	MessageID string    `json:"message_id,omitempty"`
	UID       uint32    `json:"uid,omitempty"`
	Folder    string    `json:"folder,omitempty"`
	OldFolder string    `json:"old_folder,omitempty"`
	Flags     []string  `json:"flags,omitempty"`
	Data      []byte    `json:"data,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Store provides durable, synchronized storage for bentomail.
type Store struct {
	oauthMu       sync.Mutex
	db            *sql.DB
	dirtyMessages map[string]bool
	dirtyDrafts   map[string]bool
	dir           string
	mu            sync.RWMutex
	state         State
	secrets       map[string]storedSecret // accountID -> storedSecret
	mutations     []RemoteMutation
}

// isLoopback checks if a host is localhost or a loopback IP.
func isLoopback(host string) bool {
	h := strings.TrimSpace(host)
	if strings.EqualFold(h, "localhost") || h == "127.0.0.1" || h == "::1" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Open initializes or loads a persistent store from the specified directory.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
	}

	s := &Store{
		dir:     dir,
		secrets: make(map[string]storedSecret),
		state: State{
			Version:       1,
			UIDValidity:   make(map[string]map[string]uint32),
			SavedSearches: make(map[string]string),
			Templates:     make(map[string]string),
			Folders:       make(map[string][]string),
			Preferences: Preferences{
				Translucency:   DefaultTranslucency,
				AnimationSpeed: DefaultAnimationSpeed,
				Density:        "Comfortable",
				ReadingSize:    14,
				PreviewLines:   2,
				Layout:         "Right",
				SidebarWidth:   220,
				ListWidth:      320,
				Notify:         true,
				RemoteImages:   false,
				SyncMinutes:    5,
				SendDelay:      10,
				FullSync:       true,
				RecentDays:     30,
				ReadMode:       "Immediately",
				ReadingFont:    "Martian Grotesk",
			},
		},
	}

	statePath := filepath.Join(dir, "state.json")
	_, dbErr := os.Stat(filepath.Join(dir, "mail.sqlite"))
	if data, err := os.ReadFile(statePath); err == nil && os.IsNotExist(dbErr) {
		var st State
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, fmt.Errorf("mail database is corrupt; restore a backup: %w", err)
		} else {
			if st.Version > 1 {
				return nil, errors.New("mail database was created by a newer Bentomail")
			}
			s.state = st
			if s.state.UIDValidity == nil {
				s.state.UIDValidity = make(map[string]map[string]uint32)
			}
			if s.state.SavedSearches == nil {
				s.state.SavedSearches = make(map[string]string)
			}
			if s.state.Templates == nil {
				s.state.Templates = make(map[string]string)
			}
			if s.state.Folders == nil {
				s.state.Folders = make(map[string][]string)
			}
		}
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Load encrypted secrets
	secretsPath := filepath.Join(dir, "secrets.dat")
	if encData, err := os.ReadFile(secretsPath); err == nil && len(encData) > 0 {
		if decData, err := decryptSecret(encData); err == nil {
			var loadedSecrets map[string]storedSecret
			if err := json.Unmarshal(decData, &loadedSecrets); err == nil {
				s.secrets = loadedSecrets
			} else {
				return nil, fmt.Errorf("invalid credential store: %w", err)
			}
		} else {
			return nil, fmt.Errorf("could not unlock saved credentials: %w", err)
		}
	}

	// Load offline mutations
	mutPath := filepath.Join(dir, "mutations.json")
	if data, err := os.ReadFile(mutPath); err == nil {
		var muts []RemoteMutation
		if err := json.Unmarshal(data, &muts); err == nil {
			s.mutations = muts
		}
	}

	if err := s.openDatabase(); err != nil {
		return nil, err
	}
	for i := range s.state.Drafts {
		if s.state.Drafts[i].Status == "sending" {
			s.state.Drafts[i].Status = "uncertain"
			s.state.Drafts[i].Error = "Bentomail closed during delivery. Check Sent before retrying."
			s.dirtyDrafts[s.state.Drafts[i].ID] = true
		}
	}
	if len(s.dirtyDrafts) > 0 {
		if err := s.persistStateLocked(); err != nil {
			s.Close()
			return nil, err
		}
	}
	return s, nil
}

// DataDir returns the data directory path.
func (s *Store) DataDir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dir
}

// Snapshot returns a complete deep copy of the client state.
func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Deep copy via JSON serialization
	b, _ := json.Marshal(s.state)
	var copyState State
	_ = json.Unmarshal(b, &copyState)

	return copyState
}

// SaveAccount adds or updates an account and securely saves credentials.
func (s *Store) SaveAccount(a Account, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a.ID == "" {
		a.ID = NewID()
	}

	// Validate TLS mode: "TLS", "STARTTLS", "None"
	if a.IMAPTLS == "" {
		a.IMAPTLS = "TLS"
	}
	a.SMTPPort, a.SMTPTLS = SMTPTransport(a.SMTPPort, a.SMTPTLS)

	for _, mode := range []string{a.IMAPTLS, a.SMTPTLS} {
		if mode != "TLS" && mode != "STARTTLS" && mode != "None" {
			return fmt.Errorf("invalid TLS configuration '%s', must be 'TLS', 'STARTTLS', or 'None'", mode)
		}
	}

	if a.IMAPTLS == "None" && !isLoopback(a.IMAPHost) {
		return errors.New("TLS mode 'None' for IMAP is ONLY allowed on loopback/local test servers")
	}
	if a.SMTPTLS == "None" && !isLoopback(a.SMTPHost) {
		return errors.New("TLS mode 'None' for SMTP is ONLY allowed on loopback/local test servers")
	}

	if a.AuthMode == "" {
		a.AuthMode = "password"
	}
	if a.AuthMode != "password" && a.AuthMode != "oauth2" {
		return fmt.Errorf("invalid AuthMode '%s', must be 'password' or 'oauth2'", a.AuthMode)
	}

	// Update encrypted secret store (never persist AccessToken in plaintext)
	sec := s.secrets[a.ID]
	if password != "" {
		if a.AuthMode == "oauth2" {
			sec.AccessToken = password
		} else {
			sec.Password = password
		}
	}
	if a.AccessToken != "" {
		sec.AccessToken = a.AccessToken
	}
	s.secrets[a.ID] = sec

	// Scrub AccessToken from Account struct before writing to state
	accountToPersist := a
	accountToPersist.AccessToken = ""

	found := false
	for i, existing := range s.state.Accounts {
		if existing.ID == a.ID {
			s.state.Accounts[i] = accountToPersist
			found = true
			break
		}
	}
	if !found {
		s.state.Accounts = append(s.state.Accounts, accountToPersist)
	}

	// Initialize default folders for account if empty
	if len(s.state.Folders[a.ID]) == 0 {
		s.state.Folders[a.ID] = []string{"INBOX", "Sent", "Drafts", "Archive", "Trash"}
	}

	if err := s.persistSecretsLocked(); err != nil {
		return fmt.Errorf("failed to save encrypted credentials: %w", err)
	}
	return s.persistStateLocked()
}

// DeleteAccount removes an account, its credentials, and optionally all its mail.
func (s *Store) DeleteAccount(id string, keepMail bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var newAccounts []Account
	for _, acc := range s.state.Accounts {
		if acc.ID != id {
			newAccounts = append(newAccounts, acc)
		}
	}
	s.state.Accounts = newAccounts
	delete(s.secrets, id)
	if keepMail {
		for i := range s.state.Messages {
			if s.state.Messages[i].AccountID == id {
				s.state.Messages[i].Folder = "Local archive"
				s.state.Messages[i].UID = 0
				s.dirtyMessages[s.state.Messages[i].ID] = true
			}
		}
	}

	if !keepMail {
		var newMessages []Message
		for _, m := range s.state.Messages {
			if m.AccountID == id {
				s.dirtyMessages[m.ID] = true
			}
			if m.AccountID != id {
				newMessages = append(newMessages, m)
			}
		}
		s.state.Messages = newMessages

		var newDrafts []Draft
		for _, d := range s.state.Drafts {
			if d.AccountID == id {
				s.dirtyDrafts[d.ID] = true
			}
			if d.AccountID != id {
				newDrafts = append(newDrafts, d)
			}
		}
		s.state.Drafts = newDrafts
		delete(s.state.Folders, id)
	}

	_ = s.persistSecretsLocked()
	return s.persistStateLocked()
}

// SaveMessage adds or updates a message in the store and updates contacts.
func (s *Store) SaveMessage(m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m.ID == "" {
		m.ID = NewID()
	}
	s.dirtyMessages[m.ID] = true

	found := false
	for i, existing := range s.state.Messages {
		if existing.ID == m.ID {
			s.state.Messages[i] = m
			found = true
			break
		}
	}
	if !found {
		s.state.Messages = append(s.state.Messages, m)
	}

	// Extract and update contacts
	s.addContactLocked(m.From)
	s.addContactLocked(m.To)
	s.addContactLocked(m.CC)

	return s.persistStateLocked()
}

// DeleteMessage removes a message by ID.
func (s *Store) DeleteMessage(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirtyMessages[id] = true

	var newMsgs []Message
	for _, m := range s.state.Messages {
		if m.ID != id {
			newMsgs = append(newMsgs, m)
		}
	}
	s.state.Messages = newMsgs
	return s.persistStateLocked()
}

// SaveDraft inserts or updates a draft, updating its timestamp.
func (s *Store) SaveDraft(d Draft) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if d.ID == "" {
		d.ID = NewID()
	}
	d.Updated = time.Now()
	s.dirtyDrafts[d.ID] = true

	found := false
	for i, existing := range s.state.Drafts {
		if existing.ID == d.ID {
			s.state.Drafts[i] = d
			found = true
			break
		}
	}
	if !found {
		s.state.Drafts = append(s.state.Drafts, d)
	}

	if err := s.persistStateLocked(); err != nil {
		return d, err
	}
	return d, nil
}

// DeleteDraft removes a draft by ID.
func (s *Store) DeleteDraft(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirtyDrafts[id] = true

	var newDrafts []Draft
	for _, d := range s.state.Drafts {
		if d.ID != id {
			newDrafts = append(newDrafts, d)
		}
	}
	s.state.Drafts = newDrafts
	return s.persistStateLocked()
}

// SavePreferences updates user preferences.
func (s *Store) SavePreferences(p Preferences) error {
	p.Translucency = NormalizeTranslucency(p.Translucency)
	p.AnimationSpeed = NormalizeAnimationSpeed(p.AnimationSpeed)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Preferences = p
	return s.persistStateLocked()
}

// SaveRule inserts or updates a message filtering rule.
func (s *Store) SaveRule(r Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if r.ID == "" {
		r.ID = NewID()
	}

	found := false
	for i, existing := range s.state.Rules {
		if existing.ID == r.ID {
			s.state.Rules[i] = r
			found = true
			break
		}
	}
	if !found {
		s.state.Rules = append(s.state.Rules, r)
	}

	return s.persistStateLocked()
}

// DeleteRule removes a rule by ID.
func (s *Store) DeleteRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var newRules []Rule
	for _, r := range s.state.Rules {
		if r.ID != id {
			newRules = append(newRules, r)
		}
	}
	s.state.Rules = newRules
	return s.persistStateLocked()
}

// SaveTags replaces the available tags list.
func (s *Store) SaveTags(tags []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Tags = tags
	return s.persistStateLocked()
}

func (s *Store) SaveTagColor(tag, color string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.TagColors == nil {
		s.state.TagColors = map[string]string{}
	}
	s.state.TagColors[tag] = color
	return s.persistStateLocked()
}
func (s *Store) DeleteTag(tag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tags := s.state.Tags[:0]
	for _, v := range s.state.Tags {
		if v != tag {
			tags = append(tags, v)
		}
	}
	s.state.Tags = tags
	delete(s.state.TagColors, tag)
	for i, m := range s.state.Messages {
		filtered := []string{}
		for _, v := range m.Tags {
			if v != tag {
				filtered = append(filtered, v)
			}
		}
		if len(filtered) != len(m.Tags) {
			s.state.Messages[i].Tags = filtered
			s.dirtyMessages[m.ID] = true
		}
	}
	return s.persistStateLocked()
}
func (s *Store) SaveCategory(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.state.Categories {
		if strings.EqualFold(v, name) {
			return nil
		}
	}
	s.state.Categories = append(s.state.Categories, name)
	return s.persistStateLocked()
}
func (s *Store) SaveContact(address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return err
	}
	if !strings.Contains(parsed.Address, "@") {
		return errors.New("contact email must include a domain")
	}
	for i, v := range s.state.Contacts {
		existing, err := mail.ParseAddress(v)
		if err == nil && strings.EqualFold(existing.Address, parsed.Address) {
			s.state.Contacts[i] = parsed.String()
			return s.persistStateLocked()
		}
	}
	s.addContactLocked(address)
	return s.persistStateLocked()
}

func (s *Store) DeleteContact(address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := []string{}
	for _, v := range s.state.Contacts {
		if v != address {
			filtered = append(filtered, v)
		}
	}
	s.state.Contacts = filtered
	return s.persistStateLocked()
}

// SaveSearch stores a named search query.
func (s *Store) SaveSearch(name, query string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.SavedSearches == nil {
		s.state.SavedSearches = make(map[string]string)
	}
	s.state.SavedSearches[name] = query
	return s.persistStateLocked()
}

// SaveTemplate stores an email composition template.
func (s *Store) SaveTemplate(name, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.Templates == nil {
		s.state.Templates = make(map[string]string)
	}
	s.state.Templates[name] = body
	return s.persistStateLocked()
}

// ImportEML parses raw RFC 822 email data, applies rules, and stores the message.
func (s *Store) ImportEML(accountID string, raw []byte) error {
	msg, err := ParseMessage(raw)
	if err != nil {
		return fmt.Errorf("failed to parse EML: %w", err)
	}
	msg.AccountID = accountID
	if msg.Folder == "" {
		msg.Folder = "INBOX"
	}

	// Apply filtering rules
	msg = s.ApplyRules(msg)

	return s.SaveMessage(msg)
}

// ApplyRules executes configured rules on a message.
func (s *Store) ApplyRules(m Message) Message {
	s.mu.RLock()
	rules := s.state.Rules
	s.mu.RUnlock()

	for _, r := range rules {
		if !r.Enabled || strings.TrimSpace(r.Contains) == "" {
			continue
		}
		target := ""
		switch strings.ToLower(r.Field) {
		case "from":
			target = m.From
		case "to":
			target = m.To + " " + m.CC
		case "subject":
			target = m.Subject
		case "body":
			target = m.Text + " " + m.HTML
		default:
			target = m.Subject + " " + m.From + " " + m.Text
		}

		if strings.Contains(strings.ToLower(target), strings.ToLower(r.Contains)) {
			if r.Tag != "" {
				hasTag := false
				for _, t := range m.Tags {
					if strings.EqualFold(t, r.Tag) {
						hasTag = true
						break
					}
				}
				if !hasTag {
					m.Tags = append(m.Tags, r.Tag)
				}
			}
			if r.Category != "" {
				m.Category = r.Category
			}
			if r.Folder != "" {
				m.Folder = r.Folder
			}
			if r.Star {
				m.Starred = true
			}
		}
	}
	return m
}

// Backup writes portable mail and settings. Credentials and pending network actions are excluded.
func (s *Store) Backup(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state := s.state
	state.Version = 1
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	target, _ := filepath.Abs(path)
	own, _ := filepath.Abs(filepath.Join(s.dir, "state.json"))
	database, _ := filepath.Abs(filepath.Join(s.dir, "mail.sqlite"))
	if strings.EqualFold(target, own) || strings.HasPrefix(strings.ToLower(target), strings.ToLower(database)) {
		return errors.New("choose a backup path outside the active database")
	}
	return atomicWrite(path, data)
}

// Restore validates the entire backup before replacing live state.
func (s *Store) Restore(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > 1<<30 {
		return errors.New("backup exceeds 1 GB")
	}
	var st State
	if err = json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("invalid backup: %w", err)
	}
	if st.Version != 1 {
		return errors.New("unsupported backup version")
	}
	if st.SavedSearches == nil {
		st.SavedSearches = map[string]string{}
	}
	if st.Templates == nil {
		st.Templates = map[string]string{}
	}
	if st.Folders == nil {
		st.Folders = map[string][]string{}
	}
	if st.UIDValidity == nil {
		st.UIDValidity = map[string]map[string]uint32{}
	}
	for i := range st.Accounts {
		st.Accounts[i].AccessToken = ""
	}
	for i := range st.Drafts {
		if st.Drafts[i].Status == "queued" || st.Drafts[i].Status == "sending" {
			st.Drafts[i].Status = "draft"
			st.Drafts[i].SendAt = time.Time{}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.mutations) > 0 {
		return errors.New("synchronize pending offline changes before restoring")
	}
	for _, m := range s.state.Messages {
		s.dirtyMessages[m.ID] = true
	}
	for _, m := range st.Messages {
		s.dirtyMessages[m.ID] = true
	}
	for _, d := range s.state.Drafts {
		s.dirtyDrafts[d.ID] = true
	}
	for _, d := range st.Drafts {
		s.dirtyDrafts[d.ID] = true
	}
	old := s.state
	if st.Revision < old.Revision {
		st.Revision = old.Revision
	}
	s.state = st
	if err = s.persistStateLocked(); err != nil {
		s.state = old
		return err
	}
	return nil
}
func (s *Store) GetAccount(id string) (Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.state.Accounts {
		if a.ID == id {
			if sec, ok := s.secrets[id]; ok && sec.AccessToken != "" {
				a.AccessToken = sec.AccessToken
			}
			return a, true
		}
	}
	return Account{}, false
}

func (s *Store) GetAccountSecret(accountID string) (password string, accessToken string, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sec, ok := s.secrets[accountID]
	if !ok {
		return "", "", errors.New("no credentials found for account")
	}
	return sec.Password, sec.AccessToken, nil
}

func (s *Store) QueueMutation(m RemoteMutation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.Type == "update" {
		for i, old := range s.mutations {
			if old.Type == m.Type && old.MessageID == m.MessageID {
				m.ID = old.ID
				m.UID = old.UID
				m.OldFolder = old.OldFolder
				m.CreatedAt = old.CreatedAt
				s.mutations[i] = m
				return s.persistMutationsLocked()
			}
		}
	}
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	s.mutations = append(s.mutations, m)
	return s.persistMutationsLocked()
}

func (s *Store) GetPendingMutations() []RemoteMutation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copied := make([]RemoteMutation, len(s.mutations))
	copy(copied, s.mutations)
	return copied
}

func (s *Store) RemoveMutation(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var remaining []RemoteMutation
	for _, m := range s.mutations {
		if m.ID != id {
			remaining = append(remaining, m)
		}
	}
	s.mutations = remaining
	return s.persistMutationsLocked()
}

func (s *Store) SetFolderList(accountID string, folders []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Folders == nil {
		s.state.Folders = make(map[string][]string)
	}
	s.state.Folders[accountID] = folders
	return s.persistStateLocked()
}

func (s *Store) addContactLocked(raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	addrs, err := mail.ParseAddressList(raw)
	if err != nil {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email != "" && !s.hasContactLocked(email) {
			s.state.Contacts = append(s.state.Contacts, email)
		}
		return
	}
	for _, addr := range addrs {
		email := strings.ToLower(strings.TrimSpace(addr.Address))
		if email != "" && !s.hasContactLocked(email) {
			s.state.Contacts = append(s.state.Contacts, addr.String())
		}
	}
}

func (s *Store) hasContactLocked(email string) bool {
	for _, c := range s.state.Contacts {
		if parsed, err := mail.ParseAddress(c); err == nil {
			c = parsed.Address
		}
		if strings.EqualFold(c, email) {
			return true
		}
	}
	return false
}

// Atomic file persistence

func (s *Store) persistStateLocked() error {
	if s.db != nil {
		return s.persistDatabaseLocked()
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.dir, "state.json"), data)
}

func (s *Store) persistSecretsLocked() error {
	data, err := json.Marshal(s.secrets)
	if err != nil {
		return err
	}
	enc, err := encryptSecret(data)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.dir, "secrets.dat"), enc)
}

func (s *Store) persistMutationsLocked() error {
	if s.db != nil {
		return s.persistStateLocked()
	}
	data, err := json.Marshal(s.mutations)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.dir, "mutations.json"), data)
}

func atomicWrite(filename string, data []byte) error {
	tmpFile := filename + ".tmp"
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(tmpFile)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmpFile)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}

	return os.Rename(tmpFile, filename)
}

func (s *Store) setUIDValidity(account, folder string, value uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.UIDValidity == nil {
		s.state.UIDValidity = map[string]map[string]uint32{}
	}
	if s.state.UIDValidity[account] == nil {
		s.state.UIDValidity[account] = map[string]uint32{}
	}
	if s.state.UIDValidity[account][folder] == value {
		return nil
	}
	s.state.UIDValidity[account][folder] = value
	return s.persistStateLocked()
}
func (s *Store) completeSend(d Draft, m Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirtyDrafts[d.ID] = true
	s.dirtyMessages[m.ID] = true
	for i, v := range s.state.Drafts {
		if v.ID == d.ID {
			s.state.Drafts[i] = d
		}
	}
	s.state.Messages = append(s.state.Messages, m)
	s.addContactLocked(d.To)
	s.addContactLocked(d.CC)
	s.addContactLocked(d.BCC)
	return s.persistStateLocked()
}
