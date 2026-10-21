package mailclient

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"path/filepath"
	"strings"
	"time"

	message "github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	mailmime "github.com/emersion/go-message/mail"
	xhtml "golang.org/x/net/html"
)

const maxTotalAttachmentSize = 50 << 20

func BuildMIME(a Account, d Draft) ([]byte, []string, error) {
	var identityErr error
	a, identityErr = SendingIdentity(a, d.IdentityID)
	if identityErr != nil {
		return nil, nil, identityErr
	}
	for _, v := range append([]string{a.Name, a.Email, a.ReplyTo, d.To, d.CC, d.BCC, d.Subject, d.InReplyTo}, d.References...) {
		if strings.ContainsAny(v, "\r\n\x00") {
			return nil, nil, errors.New("mail headers must not contain line breaks")
		}
	}
	from, err := mail.ParseAddress(a.Email)
	if err != nil || from.Address != a.Email {
		return nil, nil, errors.New("invalid sender email")
	}
	from.Name = a.Name
	var h mailmime.Header
	h.SetAddressList("From", []*mail.Address{from})
	h.SetSubject(d.Subject)
	h.SetDate(time.Now())
	domain := strings.Split(a.Email, "@")
	messageID := d.ID
	if messageID == "" {
		messageID = NewID()
	}
	h.SetMessageID(messageID + "@" + domain[len(domain)-1])
	var recipients []string
	seen := map[string]bool{}
	for _, field := range []struct{ key, value string }{{"To", d.To}, {"Cc", d.CC}, {"Bcc", d.BCC}} {
		if strings.TrimSpace(field.value) == "" {
			continue
		}
		addrs, err := mail.ParseAddressList(field.value)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid %s recipients: %w", field.key, err)
		}
		for _, addr := range addrs {
			if !strings.Contains(addr.Address, "@") || strings.ContainsAny(addr.Address, "\r\n\x00<>") {
				return nil, nil, errors.New("invalid recipient email")
			}
			k := strings.ToLower(addr.Address)
			if !seen[k] {
				recipients = append(recipients, addr.Address)
				seen[k] = true
			}
		}
		if field.key != "Bcc" {
			h.SetAddressList(field.key, addrs)
		}
	}
	if len(recipients) == 0 {
		return nil, nil, errors.New("enter at least one recipient")
	}
	if a.ReplyTo != "" {
		addrs, err := mail.ParseAddressList(a.ReplyTo)
		if err != nil {
			return nil, nil, err
		}
		h.SetAddressList("Reply-To", addrs)
	}
	if d.InReplyTo != "" {
		h.SetMsgIDList("In-Reply-To", []string{strings.Trim(d.InReplyTo, "<>")})
	}
	if len(d.References) > 0 {
		refs := []string{}
		for _, v := range d.References {
			if v != "" {
				refs = append(refs, strings.Trim(v, "<>"))
			}
		}
		h.SetMsgIDList("References", refs)
	}
	attachments := append([]Attachment{}, d.Attachments...)
	markup := d.HTML
	if !d.Plain {
		var inline []Attachment
		markup, inline, err = extractInline(markup)
		if err != nil {
			return nil, nil, err
		}
		attachments = append(attachments, inline...)
	}
	total := 0
	for _, at := range attachments {
		total += len(at.Data)
		if total > maxTotalAttachmentSize {
			return nil, nil, errors.New("total attachments exceed 50 MB")
		}
		if strings.ContainsAny(at.ContentID+at.ContentType, "\r\n\x00") {
			return nil, nil, errors.New("invalid attachment metadata")
		}
	}
	var buf bytes.Buffer
	w, err := mailmime.CreateWriter(&buf, h)
	if err != nil {
		return nil, nil, err
	}
	iw, err := w.CreateInline()
	if err != nil {
		return nil, nil, err
	}
	parts := []struct{ ct, body string }{{"text/plain", d.Text}}
	if !d.Plain && markup != "" {
		parts = append(parts, struct{ ct, body string }{"text/html", markup})
	}
	for _, part := range parts {
		var ph mailmime.InlineHeader
		ph.SetContentType(part.ct, map[string]string{"charset": "utf-8"})
		pw, err := iw.CreatePart(ph)
		if err != nil {
			return nil, nil, err
		}
		if _, err = io.WriteString(pw, part.body); err != nil {
			return nil, nil, err
		}
		if err = pw.Close(); err != nil {
			return nil, nil, err
		}
	}
	if err = iw.Close(); err != nil {
		return nil, nil, err
	}
	for _, at := range attachments {
		name := filepath.Base(strings.ReplaceAll(at.Name, "\\", "/"))
		if name == "" || name == "." {
			name = "attachment"
		}
		ct, _, err := mime.ParseMediaType(at.ContentType)
		if err != nil {
			ct = "application/octet-stream"
		}
		var pw io.WriteCloser
		if at.ContentID != "" {
			var ph mailmime.InlineHeader
			ph.SetContentType(ct, map[string]string{"name": name})
			ph.Set("Content-ID", "<"+strings.Trim(at.ContentID, "<>")+">")
			pw, err = w.CreateSingleInline(ph)
		} else {
			var ph mailmime.AttachmentHeader
			ph.SetContentType(ct, nil)
			ph.SetFilename(name)
			pw, err = w.CreateAttachment(ph)
		}
		if err != nil {
			return nil, nil, err
		}
		if _, err = pw.Write(at.Data); err != nil {
			return nil, nil, err
		}
		if err = pw.Close(); err != nil {
			return nil, nil, err
		}
	}
	if err = w.Close(); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), recipients, nil
}
func extractInline(markup string) (string, []Attachment, error) {
	if !strings.Contains(markup, "data:image/") {
		return markup, nil, nil
	}
	doc, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		return "", nil, err
	}
	var ats []Attachment
	var walk func(*xhtml.Node) error
	walk = func(n *xhtml.Node) error {
		if n.Type == xhtml.ElementNode && n.Data == "img" {
			for i, at := range n.Attr {
				if at.Key != "src" || !strings.HasPrefix(at.Val, "data:image/") {
					continue
				}
				meta, data, ok := strings.Cut(at.Val, ",")
				if !ok || !strings.HasSuffix(meta, ";base64") {
					return errors.New("inline image must be base64 encoded")
				}
				if len(data) > maxTotalAttachmentSize*2 {
					return errors.New("inline image is too large")
				}
				decoded, e := base64.StdEncoding.DecodeString(data)
				if e != nil {
					return e
				}
				ct := strings.TrimSuffix(strings.TrimPrefix(meta, "data:"), ";base64")
				if !containsWord("image/png image/jpeg image/gif image/webp", ct) {
					return errors.New("unsupported inline image type")
				}
				cid := NewID() + "@bentomail"
				ats = append(ats, Attachment{Name: "inline-image", ContentType: ct, ContentID: cid, Data: decoded})
				n.Attr[i].Val = "cid:" + cid
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if e := walk(child); e != nil {
				return e
			}
		}
		return nil
	}
	if e := walk(doc); e != nil {
		return "", nil, e
	}
	var b bytes.Buffer
	_ = xhtml.Render(&b, doc)
	return b.String(), ats, nil
}
func ParseMessage(raw []byte) (Message, error) {
	if len(raw) > 100<<20 {
		return Message{}, errors.New("message exceeds 100 MB")
	}
	r, err := mailmime.CreateReader(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) {
		return Message{}, err
	}
	defer r.Close()
	h := r.Header
	m := Message{ID: NewID(), Raw: append([]byte{}, raw...)}
	m.Subject, _ = h.Subject()
	decoder := mime.WordDecoder{}
	for _, field := range []struct {
		key string
		out *string
	}{{"From", &m.From}, {"To", &m.To}, {"Cc", &m.CC}, {"Bcc", &m.BCC}} {
		v := h.Get(field.key)
		if decoded, e := decoder.DecodeHeader(v); e == nil {
			v = decoded
		}
		*field.out = v
	}
	m.MessageID, _ = h.MessageID()
	m.References, _ = h.MsgIDList("References")
	reply, _ := h.MsgIDList("In-Reply-To")
	if len(reply) > 0 {
		m.InReplyTo = reply[0]
	}
	m.Date, _ = h.Date()
	if m.Date.IsZero() {
		m.Date = time.Now()
	}
	for count := 0; count < 2000; count++ {
		part, e := r.NextPart()
		if e == io.EOF {
			break
		}
		if e != nil && !message.IsUnknownCharset(e) {
			return Message{}, e
		}
		data, e := io.ReadAll(io.LimitReader(part.Body, 100<<20+1))
		if e != nil {
			return Message{}, e
		}
		if len(data) > 100<<20 {
			return Message{}, errors.New("decoded part exceeds 100 MB")
		}
		ct, params, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		disp, dp, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		name := dp["filename"]
		if name == "" {
			name = params["name"]
		}
		cid := strings.Trim(part.Header.Get("Content-ID"), "<>")
		if disp != "attachment" && name == "" && cid == "" && (ct == "text/plain" || ct == "text/html" || ct == "") {
			if ct == "text/html" {
				m.HTML += string(data)
			} else {
				m.Text += string(data)
			}
		} else {
			if name == "" {
				name = "attachment"
			}
			m.Attachments = append(m.Attachments, Attachment{Name: name, ContentType: ct, ContentID: cid, Data: data})
		}
	}
	if m.Text == "" && m.HTML != "" {
		doc, _ := xhtml.Parse(strings.NewReader(SanitizeHTML(m.HTML, false)))
		var b strings.Builder
		var walk func(*xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.TextNode {
				b.WriteString(n.Data)
			}
			if n.Type == xhtml.ElementNode && (n.Data == "p" || n.Data == "br" || n.Data == "div") {
				b.WriteByte('\n')
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		if doc != nil {
			walk(doc)
		}
		m.Text = strings.TrimSpace(b.String())
	}
	return m, nil
}
