package mailclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	store        *Store
	mu           sync.Mutex
	sendMu       sync.Mutex
	accountLocks map[string]chan struct{}
}

func NewEngine(s *Store) *Engine { return &Engine{store: s} }
func (e *Engine) credentials(a Account) (string, error) {
	password, token, err := e.store.GetAccountSecret(a.ID)
	if err != nil {
		if a.Username == "" && isLoopback(a.IMAPHost) && isLoopback(a.SMTPHost) {
			return "", nil
		}
		return "", errors.New("saved credential is missing; open account settings")
	}
	if a.AuthMode == "oauth2" {
		if a.OAuthProvider != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return e.store.oauthSecret(ctx, a)
		}
		if token != "" {
			return token, nil
		}
	}
	return password, nil
}
func loginIMAP(ctx context.Context, c *imapClient, a Account, secret string) error {
	if a.AuthMode == "oauth2" {
		return c.AuthenticateXOAUTH2(ctx, a.Username, secret)
	}
	if a.Username == "" && secret == "" {
		return nil
	}
	return c.Login(ctx, a.Username, secret)
}
func (e *Engine) connect(ctx context.Context, a Account) (*imapClient, error) {
	secret, err := e.credentials(a)
	if err != nil {
		return nil, err
	}
	c, err := dialIMAP(ctx, a.IMAPHost, a.IMAPPort, a.IMAPTLS)
	if err != nil {
		return nil, err
	}
	if err = loginIMAP(ctx, c, a, secret); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (e *Engine) TestAccount(ctx context.Context, a Account, password string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	secret := password
	if secret == "" {
		secret, _ = e.credentials(a)
	}
	c, err := dialIMAP(ctx, a.IMAPHost, a.IMAPPort, a.IMAPTLS)
	if err != nil {
		return fmt.Errorf("IMAP: %w", err)
	}
	defer c.Close()
	if err = loginIMAP(ctx, c, a, secret); err != nil {
		return fmt.Errorf("IMAP authentication: %w", err)
	}
	s, err := dialSMTP(ctx, a.SMTPHost, a.SMTPPort, a.SMTPTLS)
	if err != nil {
		return fmt.Errorf("SMTP: %w", err)
	}
	defer s.Close()
	user := a.SMTPUsername
	if user == "" {
		user = a.Username
	}
	if err = s.Authenticate(ctx, a.AuthMode, user, secret); err != nil {
		return fmt.Errorf("SMTP authentication: %w", err)
	}
	return nil
}
func (e *Engine) Send(ctx context.Context, request Draft) error {
	e.sendMu.Lock()
	defer e.sendMu.Unlock()
	d, found := e.store.draftByID(request.ID)
	if !found {
		return errors.New("draft no longer exists")
	}
	release, err := e.lockAccount(ctx, d.AccountID)
	if err != nil {
		return err
	}
	defer release()
	// The draft may have been edited while waiting for this account's sync.
	current, found := e.store.draftByID(request.ID)
	if !found || current.AccountID != d.AccountID {
		return errors.New("draft changed accounts or was removed while waiting; review it before sending")
	}
	d = current
	if d.Status == "sent" || d.Status == "sending" || d.Status == "uncertain" {
		return errors.New("message was already sent or delivery is uncertain; review it before creating another send")
	}
	reject := func(err error) error {
		d.Status = "failed"
		d.Error = err.Error()
		_, saveErr := e.store.SaveDraft(d)
		return errors.Join(err, saveErr)
	}
	a, ok := e.store.GetAccount(d.AccountID)
	if !ok || !a.Enabled {
		return reject(errors.New("sending account is missing or disabled"))
	}
	sendingIdentity, identityErr := SendingIdentity(a, d.IdentityID)
	if identityErr != nil {
		return reject(identityErr)
	}
	raw, to, err := BuildMIME(a, d)
	if err != nil {
		return reject(err)
	}
	secret, err := e.credentials(a)
	if err != nil {
		return reject(err)
	}
	d.Status = "sending"
	d.Error = ""
	if _, err = e.store.SaveDraft(d); err != nil {
		return err
	}
	fail := func(err error) error {
		d.Status = "failed"
		if errors.Is(err, ErrDeliveryUncertain) {
			d.Status = "uncertain"
		}
		d.Error = err.Error()
		_, saveErr := e.store.SaveDraft(d)
		return errors.Join(err, saveErr)
	}
	c, err := dialSMTP(ctx, a.SMTPHost, a.SMTPPort, a.SMTPTLS)
	if err != nil {
		return fail(err)
	}
	defer c.Close()
	user := a.SMTPUsername
	if user == "" {
		user = a.Username
	}
	if err = c.Authenticate(ctx, a.AuthMode, user, secret); err != nil {
		return fail(err)
	}
	if err = c.SendMail(ctx, sendingIdentity.Email, to, raw); err != nil {
		return fail(err)
	}
	m, err := ParseMessage(raw)
	if err != nil {
		return fail(fmt.Errorf("%w: sent copy could not be parsed", ErrDeliveryUncertain))
	}
	m.AccountID = a.ID
	m.Folder = specialFolder(e.store.folderNames(a.ID), "sent")
	m.Read = true
	m.BCC = d.BCC
	// SMTP success is recorded before any IMAP work. An interrupted send becomes uncertain on startup.
	d.Status = "sent"
	d.Error = ""
	if err = e.store.completeSend(d, m); err != nil {
		return fmt.Errorf("%w: could not persist sent receipt: %v", ErrDeliveryUncertain, err)
	}
	folder := specialFolder(e.store.folderNames(a.ID), "sent")
	if err = e.store.QueueMutation(RemoteMutation{Type: "append", AccountID: a.ID, Folder: folder, MessageID: m.ID, Data: raw}); err != nil {
		return fmt.Errorf("sent successfully; could not queue server Sent copy: %w", err)
	}
	return nil
}
func specialFolder(folders []string, kind string) string {
	for _, f := range folders {
		v := strings.ToLower(f)
		if kind == "sent" && (strings.HasSuffix(v, "sent") || strings.HasSuffix(v, "sent mail") || strings.HasSuffix(v, "sent items") || strings.HasSuffix(v, "sent messages")) {
			return f
		}
		if kind == "archive" && (strings.HasSuffix(v, "archive") || strings.HasSuffix(v, "archives")) {
			return f
		}
		if kind == "trash" && (strings.HasSuffix(v, "trash") || strings.HasSuffix(v, "deleted items") || strings.HasSuffix(v, "bin")) {
			return f
		}
	}
	switch kind {
	case "sent":
		return "Sent"
	case "archive":
		return "Archive"
	case "trash":
		return "Trash"
	}
	return kind
}
func (e *Engine) Sync(ctx context.Context, accountID string) error {
	return e.SyncWithProgress(ctx, accountID, nil)
}

// SyncProgress describes work within the current account and folder. Message
// counts include cached messages whose flags are checked, not just downloads.
// Totals are local to the folder; other accounts may be syncing concurrently.
type SyncProgress struct {
	AccountID, Account, Folder, Stage string
	AccountIndex, AccountsTotal       int
	FolderIndex, FoldersTotal         int
	MessagesDone, MessagesTotal       int
}

func (e *Engine) queueUpdate(m Message, oldFolder string) error {
	return e.store.queueMessageUpdate(m, oldFolder)
}
func (e *Engine) UpdateMessage(ctx context.Context, m Message, oldFolder string) error {
	release, err := e.lockAccount(ctx, m.AccountID)
	if err != nil {
		return err
	}
	defer release()
	if m.UID == 0 {
		pending := false
		for _, p := range e.store.GetPendingMutations() {
			if p.Type == "update" && p.MessageID == m.ID {
				pending = true
				break
			}
		}
		if !pending {
			return e.store.SaveMessage(m)
		}
	}
	if err := e.queueUpdate(m, oldFolder); err != nil {
		return err
	}
	a, ok := e.store.GetAccount(m.AccountID)
	if !ok || !a.Enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c, err := e.connect(ctx, a)
	if err != nil {
		return fmt.Errorf("saved locally; server changes queued: %w", err)
	}
	defer c.Close()
	return e.processMutations(ctx, c, a.ID)
}
func (e *Engine) CreateFolder(ctx context.Context, accountID, name string) error {
	release, err := e.lockAccount(ctx, accountID)
	if err != nil {
		return err
	}
	defer release()
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\r\n\x00") {
		return errors.New("invalid folder name")
	}
	if err := e.store.QueueMutation(RemoteMutation{Type: "create_folder", AccountID: accountID, Folder: name}); err != nil {
		return err
	}
	a, ok := e.store.GetAccount(accountID)
	if !ok {
		return errors.New("account not found")
	}
	c, err := e.connect(ctx, a)
	if err != nil {
		return fmt.Errorf("folder creation queued: %w", err)
	}
	defer c.Close()
	return e.processMutations(ctx, c, accountID)
}
func (e *Engine) processMutations(ctx context.Context, c *imapClient, accountID string) error {
	for _, p := range e.store.GetPendingMutations() {
		if p.AccountID != accountID {
			continue
		}
		var err error
		switch p.Type {
		case "create_folder":
			err = c.CreateFolder(ctx, p.Folder)
		case "append":
			if _, _, err = c.SelectFolder(ctx, p.Folder); err != nil {
				if err = c.CreateFolder(ctx, p.Folder); err == nil {
					_, _, err = c.SelectFolder(ctx, p.Folder)
				}
			}
			if err != nil {
				return err
			}
			parsed, parseErr := ParseMessage(p.Data)
			if parseErr != nil {
				return parseErr
			}
			existing, findErr := c.findMessageID(parsed.MessageID)
			if findErr != nil {
				return findErr
			}
			if len(existing) == 0 {
				err = c.Append(ctx, p.Folder, p.Data)
			}
			if err != nil {
				return fmt.Errorf("Sent copy still queued: %w", err)
			}
		case "update":
			var m Message
			if err = json.Unmarshal(p.Data, &m); err != nil {
				return err
			}
			if latest, ok := e.store.messageByID(m.ID); ok {
				m = latest
			}
			if p.UID == 0 {
				break
			}
			validity, _, selectErr := c.SelectFolder(ctx, p.OldFolder)
			if selectErr != nil {
				return selectErr
			}
			expected := e.store.folderUIDValidity(accountID, p.OldFolder)
			if expected != 0 && validity != expected {
				return errors.New("mailbox identity changed; queued change needs review before synchronization")
			}
			op := "-FLAGS"
			if m.Read {
				op = "+FLAGS"
			}
			if err = c.StoreFlags(ctx, p.UID, op, "\\Seen"); err != nil {
				return err
			}
			op = "-FLAGS"
			if m.Starred {
				op = "+FLAGS"
			}
			if err = c.StoreFlags(ctx, p.UID, op, "\\Flagged"); err != nil {
				return err
			}
			// Only safe, atom-compatible tags are synchronized as IMAP keywords.
			for _, tag := range m.Tags {
				if validKeyword(tag) {
					if err = c.StoreFlags(ctx, p.UID, "+FLAGS", tag); err != nil {
						break
					}
				}
			}
			if err != nil {
				return err
			}
			if p.Folder != "" && p.Folder != p.OldFolder {
				target := p.Folder
				switch strings.ToLower(target) {
				case "sent", "trash", "archive":
					target = specialFolder(e.store.folderNames(accountID), strings.ToLower(target))
				}
				err = c.MoveMessage(ctx, p.UID, target)
				if err == nil {
					m.UID = 0
					m.Folder = target
					err = e.store.SaveMessage(m)
				}
			}
		default:
			return fmt.Errorf("unknown queued action %q", p.Type)
		}
		if err != nil {
			return fmt.Errorf("server change remains queued: %w", err)
		}
		if err = e.store.RemoveMutation(p.ID); err != nil {
			return err
		}
	}
	return nil
}
func validKeyword(s string) bool { return s != "" && !strings.ContainsAny(s, " (){]%*\"\\\r\n\x00") }
