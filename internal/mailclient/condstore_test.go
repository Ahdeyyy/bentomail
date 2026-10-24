package mailclient

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap"
)

type wireMail struct {
	uid    uint32
	raw    string
	flags  string
	modSeq uint64
	date   time.Time
}

// A wire fixture exercises the optional extension independently of go-imap's
// server, which does not implement CONDSTORE. Ordinary sync uses that server in
// the other integration tests.
type condstoreFixture struct {
	mu                                  sync.Mutex
	validity                            uint32
	modSeq                              uint64
	mail                                []wireMail
	commands                            []string
	rejectSelect, rejectDelta, noModSeq bool
	omitBody                            uint32
	port                                int
}

func newWireMail(uid uint32, modSeq uint64, daysOld int) wireMail {
	return wireMail{uid: uid, modSeq: modSeq, date: time.Now().AddDate(0, 0, -daysOld), raw: fmt.Sprintf("From: a@example.com\r\nTo: b@example.com\r\nSubject: Wire %d\r\nMessage-ID: <wire-%d@example.com>\r\nContent-Type: text/plain\r\n\r\nOffline wire body %d", uid, uid, uid)}
}

func newCondstoreFixture(t *testing.T, mail []wireMail, modSeq uint64) *condstoreFixture {
	t.Helper()
	f := &condstoreFixture{validity: 1, modSeq: modSeq, mail: mail}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.port = ln.Addr().(*net.TCPAddr).Port
	var wg sync.WaitGroup
	var connsMu sync.Mutex
	conns := map[net.Conn]bool{}
	wg.Go(func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			connsMu.Lock()
			conns[conn] = true
			connsMu.Unlock()
			wg.Go(func() {
				defer conn.Close()
				defer func() { connsMu.Lock(); delete(conns, conn); connsMu.Unlock() }()
				f.serve(conn)
			})
		}
	})
	t.Cleanup(func() {
		ln.Close()
		connsMu.Lock()
		for conn := range conns {
			conn.Close()
		}
		connsMu.Unlock()
		wg.Wait()
	})
	return f
}

var changedSincePattern = regexp.MustCompile(`CHANGEDSINCE ([0-9]+)`)

func (f *condstoreFixture) serve(conn net.Conn) {
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	fmt.Fprint(conn, "* PREAUTH [CAPABILITY IMAP4rev1 CONDSTORE] test ready\r\n")
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return
		}
		tag := fields[0]
		command := strings.Join(fields[1:], " ")
		f.mu.Lock()
		f.commands = append(f.commands, command)
		mail := slices.Clone(f.mail)
		validity, modSeq := f.validity, f.modSeq
		rejectSelect, rejectDelta, noModSeq, omit := f.rejectSelect, f.rejectDelta, f.noModSeq, f.omitBody
		f.mu.Unlock()
		switch strings.ToUpper(fields[1]) {
		case "CAPABILITY":
			fmt.Fprint(conn, "* CAPABILITY IMAP4rev1 CONDSTORE\r\n")
		case "LIST":
			fmt.Fprint(conn, "* LIST () \"/\" \"INBOX\"\r\n")
		case "SELECT":
			if rejectSelect && strings.Contains(command, "CONDSTORE") {
				fmt.Fprintf(conn, "%s BAD unsupported select parameter\r\n", tag)
				continue
			}
			fmt.Fprintf(conn, "* %d EXISTS\r\n* FLAGS (\\Seen \\Flagged)\r\n* OK [UIDVALIDITY %d] UIDs valid\r\n* OK [UIDNEXT 1000] next UID\r\n", len(mail), validity)
			if !rejectSelect && strings.Contains(command, "CONDSTORE") {
				if noModSeq {
					fmt.Fprint(conn, "* OK [NOMODSEQ] not persistent\r\n")
				} else {
					fmt.Fprintf(conn, "* OK [HIGHESTMODSEQ %d] modseq\r\n", modSeq)
				}
			}
		case "UID":
			if len(fields) < 3 {
				return
			}
			switch strings.ToUpper(fields[2]) {
			case "SEARCH":
				var since time.Time
				for i, v := range fields {
					if v == "SINCE" && i+1 < len(fields) {
						since, _ = time.Parse("2-Jan-2006", strings.Trim(fields[i+1], "\""))
					}
				}
				fmt.Fprint(conn, "* SEARCH")
				for _, m := range mail {
					if since.IsZero() || !m.date.Before(since) {
						fmt.Fprintf(conn, " %d", m.uid)
					}
				}
				fmt.Fprint(conn, "\r\n")
			case "FETCH":
				if len(fields) < 4 {
					return
				}
				set, err := imap.ParseSeqSet(fields[3])
				if err != nil {
					return
				}
				var since uint64
				if match := changedSincePattern.FindStringSubmatch(command); len(match) > 0 {
					if rejectDelta {
						fmt.Fprintf(conn, "%s BAD unsupported fetch parameter\r\n", tag)
						continue
					}
					since, _ = strconv.ParseUint(match[1], 10, 64)
				}
				for i, m := range mail {
					if !set.Contains(m.uid) || (since > 0 && m.modSeq <= since) {
						continue
					}
					if strings.Contains(command, "BODY.PEEK[]") {
						if m.uid == omit {
							continue
						}
						fmt.Fprintf(conn, "* %d FETCH (UID %d BODY[] {%d}\r\n%s)\r\n", i+1, m.uid, len(m.raw), m.raw)
					} else {
						fmt.Fprintf(conn, "* %d FETCH (UID %d FLAGS (%s) RFC822.SIZE %d MODSEQ (%d))\r\n", i+1, m.uid, m.flags, len(m.raw), m.modSeq)
					}
				}
			default:
				fmt.Fprintf(conn, "%s BAD unsupported UID command\r\n", tag)
				continue
			}
		case "LOGOUT":
			fmt.Fprintf(conn, "* BYE logout\r\n%s OK logout complete\r\n", tag)
			return
		default:
			fmt.Fprintf(conn, "%s BAD unsupported command\r\n", tag)
			continue
		}
		if _, err = fmt.Fprintf(conn, "%s OK complete\r\n", tag); err != nil {
			return
		}
	}
}

func (f *condstoreFixture) change(fn func()) { f.mu.Lock(); defer f.mu.Unlock(); fn() }
func (f *condstoreFixture) takeCommands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmds := f.commands
	f.commands = nil
	return cmds
}

func wireSyncAccount(t *testing.T, s *Store, f *condstoreFixture) Account {
	t.Helper()
	a := localAccount()
	a.IMAPPort = f.port
	if err := s.SaveAccount(a, ""); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCONDSTOREIncrementalRefreshRemovalAndUIDValidityRecovery(t *testing.T) {
	const high = uint64(1<<33) + 17 // Mod-sequences must not be truncated to 32 bits.
	f := newCondstoreFixture(t, []wireMail{newWireMail(1, high, 0), newWireMail(2, high, 0)}, high)
	s := testStore(t)
	a := wireSyncAccount(t, s, f)
	e := NewEngine(s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Sync(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if got := s.folderSyncState(a.ID, "INBOX").modSeq; got != high {
		t.Fatalf("saved mod-sequence %d, want %d", got, high)
	}
	f.takeCommands()
	if err := e.Sync(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range f.takeCommands() {
		if strings.HasPrefix(cmd, "UID FETCH") {
			t.Fatal("unchanged mailbox fetched messages/flags", cmd)
		}
	}
	f.change(func() {
		f.modSeq = high + 1
		f.mail = f.mail[:1]
		f.mail[0].flags = "\\Seen \\Flagged"
		f.mail[0].modSeq = high + 1
		f.mail = append(f.mail, newWireMail(3, high+1, 0))
	})
	if err := e.Sync(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	cmds := f.takeCommands()
	delta := false
	for _, cmd := range cmds {
		if strings.Contains(cmd, fmt.Sprintf("CHANGEDSINCE %d", high)) {
			delta = true
		}
	}
	if !delta {
		t.Fatal("changed flags were not fetched incrementally", cmds)
	}
	st := s.Snapshot()
	var retained Message
	if len(st.Messages) != 3 {
		t.Fatal("new mail or retained removal missing", st.Messages)
	}
	for _, m := range st.Messages {
		if m.MessageID == "wire-1@example.com" {
			if !m.Read || !m.Starred {
				t.Fatal("delta flags not applied")
			}
			retained = m
		}
		if m.MessageID == "wire-2@example.com" && (m.Folder != "Local archive" || m.UID != 0) {
			t.Fatal("expunged mail was not archived")
		}
	}
	retained.Tags = []string{"Keep"}
	retained.Category = "Work"
	if err := s.SaveMessage(retained); err != nil {
		t.Fatal(err)
	}
	f.change(func() { f.validity = 2; f.modSeq = 7; f.mail = f.mail[:1]; f.mail[0].uid = 9; f.mail[0].modSeq = 7 })
	if err := e.Sync(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	st = s.Snapshot()
	if len(st.Messages) != 3 {
		t.Fatal("UIDVALIDITY reset duplicated retained mail")
	}
	for _, m := range st.Messages {
		if m.MessageID == retained.MessageID && (m.ID != retained.ID || m.UID != 9 || m.Category != "Work" || !slices.Contains(m.Tags, "Keep")) {
			t.Fatal("UIDVALIDITY recovery lost metadata", m)
		}
	}
	if s.folderSyncState(a.ID, "INBOX").modSeq != 7 {
		t.Fatal("UIDVALIDITY reset kept obsolete cursor")
	}
}

func TestCONDSTOREWidenedRangeAndFailedDownloadRetry(t *testing.T) {
	f := newCondstoreFixture(t, []wireMail{newWireMail(1, 100, 0), newWireMail(2, 100, 120)}, 100)
	s := testStore(t)
	a := wireSyncAccount(t, s, f)
	e := NewEngine(s)
	prefs := s.Snapshot().Preferences
	prefs.FullSync = false
	prefs.RecentDays = 30
	if err := s.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	if err := e.Sync(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Messages) != 1 {
		t.Fatal("recent range downloaded old body")
	}
	prefs.FullSync = true
	if err := s.SavePreferences(prefs); err != nil {
		t.Fatal(err)
	}
	f.change(func() { f.omitBody = 2; f.modSeq = 101 })
	if err := e.Sync(context.Background(), a.ID); err == nil {
		t.Fatal("missing body was not reported")
	}
	if s.folderSyncState(a.ID, "INBOX").modSeq != 100 {
		t.Fatal("failed download advanced cursor")
	}
	f.change(func() { f.omitBody = 0 })
	if err := e.Sync(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Messages) != 2 || s.folderSyncState(a.ID, "INBOX").modSeq != 101 {
		t.Fatal("unchanged old body was not retried after widening range")
	}
}

func TestCONDSTORERejectedOrNonPersistentExtensionsFallBack(t *testing.T) {
	for _, mode := range []string{"SELECT", "CHANGEDSINCE", "NOMODSEQ"} {
		t.Run(mode, func(t *testing.T) {
			f := newCondstoreFixture(t, []wireMail{newWireMail(1, 100, 0)}, 100)
			s := testStore(t)
			a := wireSyncAccount(t, s, f)
			e := NewEngine(s)
			f.change(func() { f.rejectSelect = mode == "SELECT"; f.noModSeq = mode == "NOMODSEQ" })
			if err := e.Sync(context.Background(), a.ID); err != nil {
				t.Fatal(err)
			}
			f.change(func() {
				f.rejectDelta = mode == "CHANGEDSINCE"
				f.modSeq++
				f.mail[0].modSeq = f.modSeq
				f.mail[0].flags = "\\Seen"
			})
			if err := e.Sync(context.Background(), a.ID); err != nil {
				t.Fatal(err)
			}
			if !s.Snapshot().Messages[0].Read {
				t.Fatal("fallback lost flag change")
			}
		})
	}
}
