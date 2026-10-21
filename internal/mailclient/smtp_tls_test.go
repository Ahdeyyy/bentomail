package mailclient

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestSMTPStandardPortSecurity(t *testing.T) {
	for _, tc := range []struct {
		port     int
		mode     string
		wantPort int
		wantMode string
	}{
		{0, "", 587, "STARTTLS"}, {0, "TLS", 465, "TLS"},
		{587, "TLS", 587, "STARTTLS"}, {25, "TLS", 25, "STARTTLS"},
		{465, "STARTTLS", 465, "TLS"}, {465, "", 465, "TLS"},
		{2525, "TLS", 2525, "TLS"}, {1025, "None", 1025, "None"},
	} {
		port, mode := SMTPTransport(tc.port, tc.mode)
		if port != tc.wantPort || mode != tc.wantMode {
			t.Fatalf("SMTPTransport(%d, %q) = %d, %q", tc.port, tc.mode, port, mode)
		}
	}
	s := testStore(t)
	a := localAccount()
	a.SMTPPort, a.SMTPTLS = 587, "TLS"
	if err := s.SaveAccount(a, ""); err != nil {
		t.Fatal(err)
	}
	saved, _ := s.GetAccount(a.ID)
	if saved.SMTPTLS != "STARTTLS" {
		t.Fatal("saved account retained direct TLS on STARTTLS port")
	}
}

func TestSMTPEncryptedDelivery(t *testing.T) {
	certServer := httptest.NewTLSServer(nil)
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	for _, tc := range []struct {
		name, mode string
		trusted    bool
	}{
		{"STARTTLS", "STARTTLS", true}, {"direct TLS", "TLS", true},
		{"untrusted STARTTLS", "STARTTLS", false}, {"untrusted direct TLS", "TLS", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			done := make(chan error, 1)
			go func() {
				done <- func() error {
					conn, err := ln.Accept()
					if err != nil {
						return err
					}
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
					secure := false
					upgrade := func() error {
						tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
						if err := tlsConn.Handshake(); err != nil {
							return err
						}
						conn, secure = tlsConn, true
						return nil
					}
					if tc.mode == "TLS" {
						if err := upgrade(); err != nil {
							return err
						}
					}
					r := textproto.NewReader(bufio.NewReader(conn))
					reply := func(s string) { fmt.Fprint(conn, s+"\r\n") }
					reply("220 loopback.test ESMTP")
					for {
						line, err := r.ReadLine()
						if err != nil {
							return err
						}
						switch {
						case strings.HasPrefix(line, "EHLO"):
							if secure {
								reply("250-loopback.test\r\n250 AUTH PLAIN")
							} else {
								reply("250-loopback.test\r\n250 STARTTLS")
							}
						case line == "STARTTLS":
							reply("220 ready for TLS")
							if err := upgrade(); err != nil {
								return err
							}
							r = textproto.NewReader(bufio.NewReader(conn))
						case strings.HasPrefix(line, "AUTH PLAIN"), strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
							if !secure {
								return fmt.Errorf("credentials or mail sent before TLS")
							}
							if strings.HasPrefix(line, "AUTH") {
								reply("235 authenticated")
							} else {
								reply("250 ok")
							}
						case line == "DATA":
							reply("354 end with dot")
							raw, err := io.ReadAll(r.DotReader())
							if err != nil {
								return err
							}
							if !strings.Contains(string(raw), "Encrypted delivery") {
								return fmt.Errorf("message missing")
							}
							reply("250 accepted")
							return nil
						default:
							return fmt.Errorf("unexpected command %q", line)
						}
					}
				}()
			}()
			roots := x509.NewCertPool()
			if tc.trusted {
				roots.AddCert(certServer.Certificate())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, err := dialSMTPWithTLS(ctx, "127.0.0.1", ln.Addr().(*net.TCPAddr).Port, tc.mode, &tls.Config{RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12})
			if !tc.trusted {
				if err == nil {
					c.Close()
					t.Fatal("untrusted certificate accepted")
				}
				<-done
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if err := c.Authenticate(ctx, "password", "sender", "secret"); err != nil {
				t.Fatal(err)
			}
			if err := c.SendMail(ctx, "sender@example.com", []string{"recipient@example.com"}, []byte("Subject: Encrypted delivery\r\n\r\nHello\r\n")); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSMTPRequiresSTARTTLS(t *testing.T) {
	port, _ := smtpFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := dialSMTP(ctx, "127.0.0.1", port, "STARTTLS")
	if err == nil {
		c.Close()
		t.Fatal("server without STARTTLS was accepted")
	}
}
