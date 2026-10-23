package mailclient

import (
	"encoding/json"
	"math"
	"time"
)

// Account represents an email account configuration.
type Identity struct{ ID, Name, Email, ReplyTo, Signature string }
type Account struct {
	DefaultIdentityID string
	Identities        []Identity
	OAuthProvider     string
	OAuthClientID     string
	ID                string
	Name              string
	Email             string
	ReplyTo           string
	Signature         string
	Color             string
	IMAPHost          string
	SMTPHost          string
	Username          string
	SMTPUsername      string
	AuthMode          string
	AccessToken       string
	IMAPPort          int
	SMTPPort          int
	IMAPTLS           string
	SMTPTLS           string
	Enabled           bool
}

// Attachment represents an email attachment or inline content.
type Attachment struct {
	Name        string
	ContentType string
	ContentID   string
	Data        []byte
}

// Message represents an email message stored or synchronized.
type Message struct {
	ID           string
	AccountID    string
	Folder       string
	MessageID    string
	InReplyTo    string
	Subject      string
	From         string
	To           string
	CC           string
	BCC          string
	Text         string
	HTML         string
	Category     string
	References   []string
	Date         time.Time
	Read         bool
	Starred      bool
	Important    bool
	Tags         []string
	Attachments  []Attachment
	Raw          []byte
	UID          uint32
	SnoozedUntil time.Time
	FollowUp     time.Time
}

// Draft represents a composition draft or message queued for sending.
type Draft struct {
	IdentityID  string
	ID          string
	AccountID   string
	To          string
	CC          string
	BCC         string
	Subject     string
	Text        string
	HTML        string
	InReplyTo   string
	References  []string
	Attachments []Attachment
	Plain       bool
	SendAt      time.Time
	Status      string
	Error       string
	Updated     time.Time
}

// Rule represents an automation filter applied on messages.
type Rule struct {
	ID       string
	Name     string
	Field    string
	Contains string
	Tag      string
	Category string
	Folder   string
	Star     bool
	Enabled  bool
}

// Preferences holds user client preferences.
type Preferences struct {
	TrustedSenders      []string
	DefaultAccountID    string
	ReadMode            string
	ReadingFont         string
	HideAvatars         bool
	FullSync            bool
	RecentDays          int
	Dark                bool
	Translucency        float64
	AnimationSpeed      float64
	Accent              string
	Density             string
	ReadingSize         int
	PreviewLines        int
	Layout              string
	SidebarWidth        float32
	SidebarCollapsed    bool
	ListWidth           float32
	Notify              bool
	NotificationPreview bool
	Background          bool
	RemoteImages        bool
	SyncMinutes         int
	SendDelay           int
	QuietStart          string
	QuietEnd            string
}

const DefaultTranslucency = 65
const MaxTranslucency = 65

const (
	DefaultAnimationSpeed = 1
	MinAnimationSpeed     = 0.25
	MaxAnimationSpeed     = 2
)

func NormalizeAnimationSpeed(value float64) float64 {
	if value == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return DefaultAnimationSpeed
	}
	return max(MinAnimationSpeed, min(MaxAnimationSpeed, value))
}

func NormalizeTranslucency(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return DefaultTranslucency
	}
	return max(0, min(MaxTranslucency, value))
}

// Keep older preferences frosted by default while preserving an explicit 0
// (opaque), including when restoring a backup.
func (p *Preferences) UnmarshalJSON(data []byte) error {
	type preferences Preferences
	value := preferences{Translucency: DefaultTranslucency, AnimationSpeed: DefaultAnimationSpeed}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*p = Preferences(value)
	p.Translucency = NormalizeTranslucency(p.Translucency)
	p.AnimationSpeed = NormalizeAnimationSpeed(p.AnimationSpeed)
	return nil
}

// State represents the complete snapshot of the mail client data model.
type State struct {
	Categories    []string
	Revision      uint64
	TagColors     map[string]string
	Version       int
	UIDValidity   map[string]map[string]uint32
	Accounts      []Account
	Messages      []Message
	Drafts        []Draft
	Rules         []Rule
	Preferences   Preferences
	Tags          []string
	SavedSearches map[string]string
	Contacts      []string
	Templates     map[string]string
	Folders       map[string][]string
}
