package mailclient

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"time"
)

var ErrDeliveryUncertain = errors.New("delivery status is uncertain; check Sent and the recipient before retrying")

type smtpClient struct {
	client *smtp.Client
	conn   net.Conn
	stop   func() bool
	host   string
}

func dialSMTP(ctx context.Context, host string, port int, mode string) (*smtpClient, error) {
	if mode == "" {
		mode = "TLS"
	}
	if mode != "TLS" && mode != "STARTTLS" && mode != "None" {
		return nil, fmt.Errorf("unknown SMTP security mode %q", mode)
	}
	if mode == "None" && !isLoopback(host) {
		return nil, errors.New("unencrypted SMTP is allowed only on loopback")
	}
	if port == 0 {
		port = 587
		if mode == "TLS" {
			port = 465
		}
	}
	d := net.Dialer{Timeout: 20 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(60 * time.Second)
	if v, ok := ctx.Deadline(); ok {
		deadline = v
	}
	_ = conn.SetDeadline(deadline)
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	fail := func(err error) (*smtpClient, error) { stop(); _ = conn.Close(); return nil, err }
	conf := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if mode == "TLS" {
		secure := tls.Client(conn, conf)
		if err := secure.HandshakeContext(ctx); err != nil {
			return fail(err)
		}
		conn = secure
	}
	cli, err := smtp.NewClient(conn, host)
	if err != nil {
		return fail(err)
	}
	if err = cli.Hello("bentomail"); err != nil {
		return fail(err)
	}
	if mode == "STARTTLS" {
		if err = cli.StartTLS(conf); err != nil {
			return fail(err)
		}
	}
	return &smtpClient{client: cli, conn: conn, stop: stop, host: host}, nil
}
func (c *smtpClient) Close() error {
	if c.stop != nil {
		c.stop()
	}
	return c.client.Close()
}

type oauthSMTP struct{ user, token string }

func (a oauthSMTP) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + a.user + "\x01auth=Bearer " + a.token + "\x01\x01"), nil
}
func (a oauthSMTP) Next(challenge []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("OAuth authentication rejected")
	}
	return nil, nil
}

type loginSMTP struct {
	user, password string
	step           int
}

func (a *loginSMTP) Start(*smtp.ServerInfo) (string, []byte, error) { return "LOGIN", nil, nil }
func (a *loginSMTP) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	if a.step == 1 {
		return []byte(a.user), nil
	}
	if a.step == 2 {
		return []byte(a.password), nil
	}
	return nil, errors.New("unexpected authentication challenge")
}
func (c *smtpClient) Authenticate(ctx context.Context, mode, user, secret string) error {
	if user == "" && secret == "" {
		return nil
	}
	if mode == "oauth2" {
		return c.client.Auth(oauthSMTP{user, secret})
	}
	if ok, methods := c.client.Extension("AUTH"); ok {
		for _, method := range []string{"PLAIN", "LOGIN"} {
			if containsWord(methods, method) {
				if method == "LOGIN" {
					return c.client.Auth(&loginSMTP{user: user, password: secret})
				}
				return c.client.Auth(smtp.PlainAuth("", user, secret, c.host))
			}
		}
	}
	return errors.New("SMTP server does not advertise a supported authentication method")
}
func (c *smtpClient) SendMail(ctx context.Context, from string, to []string, raw []byte) error {
	if err := c.client.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err := c.client.Rcpt(addr); err != nil {
			return err
		}
	}
	w, err := c.client.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(raw); err != nil {
		return fmt.Errorf("%w: %v", ErrDeliveryUncertain, err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("%w: %v", ErrDeliveryUncertain, err)
	}
	return nil
}
