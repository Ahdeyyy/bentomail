package main

import (
	"context"
	"encoding/json"
)

func (a *app) refreshWatchers() {
	if a.window == nil {
		return
	}
	if a.watchers == nil {
		a.watchers = map[string]context.CancelFunc{}
		a.watchConfig = map[string]string{}
	}
	active := map[string]bool{}
	for _, account := range a.state.Accounts {
		if !account.Enabled {
			continue
		}
		active[account.ID] = true
		raw, _ := json.Marshal(account)
		key := string(raw)
		if a.watchConfig[account.ID] == key {
			continue
		}
		if cancel := a.watchers[account.ID]; cancel != nil {
			cancel()
		}
		ctx, cancel := context.WithCancel(a.ctx)
		a.watchers[account.ID] = cancel
		a.watchConfig[account.ID] = key
		e, id, dispatch := a.engine, account.ID, a.dispatch
		go e.WatchInbox(ctx, id, func() {
			dispatch(func() {
				if !a.closed {
					a.syncMail()
				}
			})
		})
	}
	for id, cancel := range a.watchers {
		if !active[id] {
			cancel()
			delete(a.watchers, id)
			delete(a.watchConfig, id)
		}
	}
}
