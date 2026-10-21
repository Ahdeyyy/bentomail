package mailclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A scripted server verifies the shutdown command without relying on a real
// mailbox, and fails if cleanup sends CLOSE (which can expunge deleted mail).
func shutdownServer(t *testing.T, respond bool) (int, <-chan string, <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	command := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err = io.WriteString(conn, "* OK [CAPABILITY IMAP4rev1] test ready\r\n"); err != nil {
			done <- err
			return
		}
		r := bufio.NewReader(conn)
		line, err := r.ReadString('\n')
		if err != nil {
			done <- err
			return
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			done <- fmt.Errorf("unexpected command: %q", line)
			return
		}
		command <- fields[1]
		if respond {
			_, err = fmt.Fprintf(conn, "* BYE logging out\r\n%s OK LOGOUT completed\r\n", fields[0])
			if err != nil {
				done <- err
				return
			}
		}
		// A successful logout and a cancelled command must both release TCP.
		_, err = r.ReadByte()
		if err != io.EOF {
			done <- fmt.Errorf("connection not released: %v", err)
			return
		}
		done <- nil
	}()
	return ln.Addr().(*net.TCPAddr).Port, command, done
}

func waitForIMAPReader(t *testing.T, c *imapClient) {
	t.Helper()
	select {
	case <-c.c.LoggedOut():
	case <-time.After(time.Second):
		t.Fatal("IMAP reader did not stop")
	}
}

func waitForShutdownCommand(t *testing.T, commands <-chan string) string {
	t.Helper()
	select {
	case cmd := <-commands:
		return cmd
	case <-time.After(3 * time.Second):
		t.Fatal("server did not receive an IMAP command")
		return ""
	}
}

func waitForServerShutdown(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server connection was not released")
	}
}

func TestIMAPGracefulLogoutAndUnresponsiveServer(t *testing.T) {
	for _, respond := range []bool{true, false} {
		t.Run(fmt.Sprintf("serverResponds=%t", respond), func(t *testing.T) {
			port, commands, done := shutdownServer(t, respond)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, err := dialIMAP(ctx, "127.0.0.1", port, "None")
			if err != nil {
				t.Fatal(err)
			}
			defer c.terminate()
			start := time.Now()
			err = c.Close()
			if respond && err != nil {
				t.Fatal(err)
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("logout exceeded its bounded timeout")
			}
			if cmd := waitForShutdownCommand(t, commands); cmd != "LOGOUT" {
				t.Fatalf("cleanup sent %s instead of LOGOUT", cmd)
			}
			waitForServerShutdown(t, done)
			waitForIMAPReader(t, c)
		})
	}
}

func TestIMAPCancelInterruptsPendingCommand(t *testing.T) {
	port, commands, done := shutdownServer(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := dialIMAP(ctx, "127.0.0.1", port, "None")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	result := make(chan error, 1)
	go func() { result <- c.c.Noop() }()
	if cmd := waitForShutdownCommand(t, commands); cmd != "NOOP" {
		t.Fatalf("unexpected command %q", cmd)
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled command succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt the command")
	}
	waitForServerShutdown(t, done)
	waitForIMAPReader(t, c)
}

func TestIMAPTransportIntentionalCloseIsEOF(t *testing.T) {
	local, remote := net.Pipe()
	defer remote.Close()
	c := &imapTransport{Conn: local, closing: new(atomic.Bool)}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("intentional close returned %v", err)
	}
	// A socket closed outside the transport is an unexpected failure and
	// must remain visible rather than be normalized to EOF.
	local2, remote2 := net.Pipe()
	defer remote2.Close()
	c2 := &imapTransport{Conn: local2, closing: new(atomic.Bool)}
	local2.Close()
	if _, err := c2.Read(make([]byte, 1)); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("unexpected failure was hidden: %v", err)
	}
}
