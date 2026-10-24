package mailclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/commands"
	"github.com/emersion/go-imap/responses"
)

type imapMessageInfo struct {
	UID        uint32
	Size       uint32
	Flags      []string
	Read, Star bool
}
type imapClient struct {
	c           *client.Client
	conn        net.Conn
	stop        func() bool
	ctx         context.Context
	noCondstore bool
}

// go-imap has a reader running between commands. A deliberate local close
// should end that reader with EOF, rather than log an expected socket error.
// Unexpected read failures still propagate unchanged.
type imapTransport struct {
	net.Conn
	closing *atomic.Bool
}

func (c *imapTransport) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil && c.closing.Load() {
		return n, io.EOF
	}
	return n, err
}
func (c *imapTransport) Close() error {
	c.closing.Store(true)
	return c.Conn.Close()
}

func dialIMAP(ctx context.Context, host string, port int, mode string) (*imapClient, error) {
	if mode == "" {
		mode = "TLS"
	}
	if mode != "TLS" && mode != "STARTTLS" && mode != "None" {
		return nil, fmt.Errorf("unknown IMAP security mode %q", mode)
	}
	if mode == "None" && !isLoopback(host) {
		return nil, errors.New("unencrypted IMAP is allowed only on loopback")
	}
	if port == 0 {
		port = 993
		if mode != "TLS" {
			port = 143
		}
	}
	d := net.Dialer{Timeout: 20 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(90 * time.Second)
	if v, ok := ctx.Deadline(); ok {
		deadline = v
	}
	_ = conn.SetDeadline(deadline)
	raw := conn
	closing := new(atomic.Bool)
	abort := func() { closing.Store(true); _ = raw.Close() }
	stop := context.AfterFunc(ctx, abort)
	fail := func(e error) (*imapClient, error) { stop(); abort(); return nil, e }
	conf := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if mode == "TLS" {
		v := tls.Client(conn, conf)
		if err := v.HandshakeContext(ctx); err != nil {
			return fail(err)
		}
		conn = v
	}
	conn = &imapTransport{Conn: conn, closing: closing}
	c, err := client.New(conn)
	if err != nil {
		return fail(err)
	}
	c.Timeout = 60 * time.Second
	if mode == "STARTTLS" {
		if err = c.StartTLS(conf); err != nil {
			return fail(err)
		}
	}
	return &imapClient{c: c, conn: conn, stop: stop, ctx: ctx}, nil
}
func (c *imapClient) terminate() error { c.stop(); return c.conn.Close() }
func (c *imapClient) Close() error {
	defer c.terminate()
	if c.ctx.Err() != nil || c.c.State() == imap.LogoutState {
		return nil
	}
	// Always release the socket, even if a server doesn't answer LOGOUT.
	c.c.Timeout = 2 * time.Second
	return c.c.Logout()
}
func (c *imapClient) Login(ctx context.Context, user, password string) error {
	return c.c.Login(user, password)
}

type oauthIMAP struct{ user, token string }

func (a oauthIMAP) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + a.user + "\x01auth=Bearer " + a.token + "\x01\x01"), nil
}
func (a oauthIMAP) Next([]byte) ([]byte, error) {
	return nil, errors.New("OAuth authentication rejected")
}
func (c *imapClient) AuthenticateXOAUTH2(ctx context.Context, user, token string) error {
	return c.c.Authenticate(oauthIMAP{user, token})
}
func (c *imapClient) ListFolders(ctx context.Context) ([]string, error) {
	ch := make(chan *imap.MailboxInfo, 16)
	done := make(chan error, 1)
	go func() { done <- c.c.List("", "*", ch) }()
	var out []string
	for item := range ch {
		skip := false
		for _, attr := range item.Attributes {
			if strings.EqualFold(attr, imap.NoSelectAttr) {
				skip = true
			}
		}
		if !skip {
			out = append(out, item.Name)
		}
	}
	return out, <-done
}
func (c *imapClient) SelectFolder(ctx context.Context, folder string) (uint32, int, error) {
	s, e := c.c.Select(folder, false)
	if e != nil {
		return 0, 0, e
	}
	return s.UidValidity, int(s.Messages), nil
}

// SelectForSync opts into CONDSTORE only when advertised. A mailbox may return
// NOMODSEQ, and servers rejecting the extension fall back to ordinary SELECT.
func (c *imapClient) SelectForSync(ctx context.Context, folder string) (uint32, int, uint64, error) {
	supported, err := c.c.Support("CONDSTORE")
	if err != nil {
		return 0, 0, 0, err
	}
	if !supported || c.noCondstore {
		v, n, err := c.SelectFolder(ctx, folder)
		return v, n, 0, err
	}
	cmd := (&commands.Select{Mailbox: folder}).Command()
	cmd.Arguments = append(cmd.Arguments, []interface{}{imap.RawString("CONDSTORE")})
	mbox := &imap.MailboxStatus{Name: folder, Items: map[imap.StatusItem]interface{}{}}
	base := &responses.Select{Mailbox: mbox}
	var modSeq uint64
	handler := responses.HandlerFunc(func(resp imap.Resp) error {
		if s, ok := resp.(*imap.StatusResp); ok {
			if s.Code == "HIGHESTMODSEQ" && len(s.Arguments) == 1 {
				modSeq, _ = strconv.ParseUint(fmt.Sprint(s.Arguments[0]), 10, 64)
				return nil
			}
			if s.Code == "NOMODSEQ" {
				modSeq = 0
				return nil
			}
		}
		return base.Handle(resp)
	})
	c.c.SetState(imap.AuthenticatedState, mbox)
	status, err := c.c.Execute(cmd, handler)
	if err != nil {
		return 0, 0, 0, err
	}
	if err = status.Err(); err != nil {
		c.noCondstore = true
		v, n, err := c.SelectFolder(ctx, folder)
		return v, n, 0, err
	}
	mbox.ReadOnly = status.Code == imap.CodeReadOnly
	c.c.SetState(imap.SelectedState, mbox)
	return mbox.UidValidity, int(mbox.Messages), modSeq, nil
}
func uidSet(uid uint32) *imap.SeqSet { s := new(imap.SeqSet); s.AddNum(uid); return s }
func (c *imapClient) FetchUIDAndFlags(ctx context.Context) ([]imapMessageInfo, error) {
	s := new(imap.SeqSet)
	s.AddRange(1, 0)
	return c.fetchUIDAndFlags(s, 0)
}

func (c *imapClient) fetchUIDAndFlags(s *imap.SeqSet, since uint64) ([]imapMessageInfo, error) {
	ch := make(chan *imap.Message, 32)
	done := make(chan error, 1)
	go func() {
		if since == 0 {
			done <- c.c.UidFetch(s, []imap.FetchItem{imap.FetchUid, imap.FetchFlags, imap.FetchRFC822Size}, ch)
			return
		}
		defer close(ch)
		cmd := (&commands.Uid{Cmd: &commands.Fetch{SeqSet: s, Items: []imap.FetchItem{imap.FetchUid, imap.FetchFlags, imap.FetchRFC822Size}}}).Command()
		cmd.Arguments = append(cmd.Arguments, []interface{}{imap.RawString("CHANGEDSINCE"), imap.RawString(strconv.FormatUint(since, 10))})
		status, err := c.c.Execute(cmd, &responses.Fetch{Messages: ch, SeqSet: s, Uid: true})
		if err == nil {
			err = status.Err()
		}
		done <- err
	}()
	var out []imapMessageInfo
	for m := range ch {
		out = append(out, imapMessageInfo{UID: m.Uid, Size: m.Size, Flags: m.Flags, Read: containsWord(strings.Join(m.Flags, " "), imap.SeenFlag), Star: containsWord(strings.Join(m.Flags, " "), imap.FlaggedFlag)})
	}
	return out, <-done
}
func (c *imapClient) FetchRFC822(ctx context.Context, uid uint32) ([]byte, error) {
	var raw []byte
	err := c.FetchRFC822Batch(ctx, []uint32{uid}, func(_ uint32, data []byte) error { raw = data; return nil })
	return raw, err
}

// Stream each result to the consumer instead of accumulating entire batches
// (which can contain large attachments) in memory. Always drain the response
// channel, including on callback failure, so the IMAP reader cannot get stuck.
func (c *imapClient) FetchRFC822Batch(ctx context.Context, uids []uint32, consume func(uint32, []byte) error) error {
	if len(uids) == 0 {
		return nil
	}
	set := new(imap.SeqSet)
	set.AddNum(uids...)
	sec := &imap.BodySectionName{Peek: true}
	ch := make(chan *imap.Message)
	done := make(chan error, 1)
	go func() { done <- c.c.UidFetch(set, []imap.FetchItem{imap.FetchUid, sec.FetchItem()}, ch) }()
	var errs []error
	seen := map[uint32]bool{}
	for m := range ch {
		if !set.Contains(m.Uid) || seen[m.Uid] {
			continue
		}
		seen[m.Uid] = true
		if err := ctx.Err(); err != nil {
			continue
		}
		r := m.GetBody(sec)
		if r == nil {
			errs = append(errs, fmt.Errorf("UID %d: empty message payload", m.Uid))
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(r, (100<<20)+1))
		if err == nil && len(raw) > 100<<20 {
			err = errors.New("message exceeds 100 MB download limit")
		}
		if err == nil && len(raw) == 0 {
			err = errors.New("empty message payload")
		}
		if err == nil {
			err = consume(m.Uid, raw)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("UID %d: %w", m.Uid, err))
		}
	}
	if err := <-done; err != nil {
		errs = append(errs, err)
	}
	if ctx.Err() != nil {
		return errors.Join(append(errs, ctx.Err())...)
	}
	for _, uid := range uids {
		if !seen[uid] {
			errs = append(errs, fmt.Errorf("UID %d disappeared during download; will retry if still present", uid))
		}
	}
	return errors.Join(errs...)
}
func (c *imapClient) StoreFlags(ctx context.Context, uid uint32, op string, flags ...string) error {
	values := make([]interface{}, len(flags))
	for i, v := range flags {
		values[i] = v
	}
	return c.c.UidStore(uidSet(uid), imap.StoreItem(op+".SILENT"), values, nil)
}
func (c *imapClient) MoveMessage(ctx context.Context, uid uint32, folder string) error {
	supported, err := c.c.Support("MOVE")
	if err != nil {
		return err
	}
	if !supported {
		return errors.New("server lacks UID MOVE; move remains queued to avoid expunging unrelated mail")
	}
	return c.c.UidMove(uidSet(uid), folder)
}
func (c *imapClient) CreateFolder(ctx context.Context, folder string) error {
	return c.c.Create(folder)
}
func (c *imapClient) Append(ctx context.Context, folder string, raw []byte) error {
	return c.c.Append(folder, []string{imap.SeenFlag}, time.Now(), bytes.NewReader(raw))
}
func containsWord(list, word string) bool {
	for _, v := range strings.Fields(list) {
		if strings.EqualFold(v, word) {
			return true
		}
	}
	return false
}

func (c *imapClient) recentUIDs(since time.Time) (map[uint32]bool, error) {
	criteria := imap.NewSearchCriteria()
	criteria.Since = since
	uids, err := c.c.UidSearch(criteria)
	if err != nil {
		return nil, err
	}
	out := make(map[uint32]bool, len(uids))
	for _, id := range uids {
		out[id] = true
	}
	return out, nil
}

func (c *imapClient) findMessageID(id string) ([]uint32, error) {
	criteria := imap.NewSearchCriteria()
	criteria.Header.Set("Message-Id", "<"+strings.Trim(id, "<>")+">")
	return c.c.UidSearch(criteria)
}
