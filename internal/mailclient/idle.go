package mailclient

import (
	"context"
	"github.com/emersion/go-imap/client"
	"time"
)

// WatchInbox uses a dedicated IDLE connection. Normal polling remains a fallback.
func (e *Engine) WatchInbox(ctx context.Context, accountID string, changed func()) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		a, ok := e.store.GetAccount(accountID)
		if !ok || !a.Enabled {
			return
		}
		c, err := e.connect(ctx, a)
		if err == nil {
			supported, supportErr := c.c.Support("IDLE")
			if supportErr != nil {
				err = supportErr
			} else if !supported {
				c.Close()
				return
			}
			if err == nil {
				_, _, err = c.SelectFolder(ctx, "INBOX")
			}
			if err == nil {
				_ = c.conn.SetDeadline(time.Time{})
				c.c.Timeout = 0
				updates := make(chan client.Update, 32)
				c.c.Updates = updates
				stop := make(chan struct{})
				done := make(chan error, 1)
				go func() { done <- c.c.Idle(stop, nil) }()
				renew := time.NewTimer(20 * time.Minute)
				debounce := time.NewTimer(time.Hour)
				if !debounce.Stop() {
					<-debounce.C
				}
				pending := false
				running := true
				for running {
					select {
					case <-ctx.Done():
						running = false
					case <-renew.C:
						running = false
					case <-done:
						running = false
					case <-updates:
						if !pending {
							pending = true
							debounce.Reset(350 * time.Millisecond)
						}
					case <-debounce.C:
						pending = false
						changed()
					}
				}
				close(stop)
				renew.Stop()
				debounce.Stop()
				// IDLE may still be running; don't issue LOGOUT concurrently.
				// Closing the transport also interrupts an unresponsive server.
				c.terminate()
				backoff = 5 * time.Second
			} else {
				c.Close()
			}
		}
		if ctx.Err() != nil {
			return
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}
