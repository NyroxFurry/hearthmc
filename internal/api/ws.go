package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"hearth/internal/events"
	"hearth/internal/perms"
	"hearth/internal/store"
)

type wsIn struct {
	Op     string `json:"op"`
	Server string `json:"s"`
	Cmd    string `json:"cmd"`
}

// GET /api/ws — one multiplexed WebSocket per browser tab.
func (a *API) websocket(w http.ResponseWriter, r *http.Request, u *store.User) error {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		return nil // Accept already wrote the response
	}
	c.SetReadLimit(64 << 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var access atomic.Pointer[map[string]*perms.Set]
	refresh := func() {
		m := map[string]*perms.Set{}
		member, _ := a.Store.MemberServerPerms(ctx, u.ID)
		for _, inst := range a.Servers.List() {
			srv := inst.Server()
			switch {
			case u.IsAdmin:
				m[srv.ID] = perms.Full(srv.OwnerID == u.ID)
			case srv.OwnerID == u.ID:
				m[srv.ID] = perms.Full(true)
			default:
				if p, ok := member[srv.ID]; ok {
					m[srv.ID] = perms.FromList(p)
				}
			}
		}
		access.Store(&m)
	}
	refresh()
	can := func(server, perm string) bool {
		m := *access.Load()
		ps, ok := m[server]
		if !ok {
			return false
		}
		return perm == "" || ps.Has(perm)
	}

	var subMu sync.RWMutex
	consoleSubs := map[string]bool{}

	sub := a.Bus.Subscribe(1024, func(e events.Event) bool {
		switch e.Type {
		case events.Task:
			return u.IsAdmin
		case events.ServerMeta:
			return true // handled below (refresh + visibility)
		case events.Console:
			subMu.RLock()
			on := consoleSubs[e.Server]
			subMu.RUnlock()
			return on && can(e.Server, perms.ConsoleRead)
		case events.Players, events.MCEvent:
			return can(e.Server, perms.PlayersRead)
		case events.Backup:
			return can(e.Server, perms.BackupsRead)
		default:
			return can(e.Server, "")
		}
	})
	defer a.Bus.Unsubscribe(sub)

	send := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		return c.Write(wctx, websocket.MessageText, b)
	}
	var writeMu sync.Mutex
	safeSend := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return send(v)
	}

	// reader
	go func() {
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var in wsIn
			if json.Unmarshal(data, &in) != nil {
				continue
			}
			switch in.Op {
			case "sub":
				if !can(in.Server, perms.ConsoleRead) {
					safeSend(map[string]any{"t": "error", "s": in.Server, "d": "Keine Berechtigung für die Konsole"})
					continue
				}
				inst := a.Servers.Get(in.Server)
				if inst == nil {
					continue
				}
				subMu.Lock()
				consoleSubs[in.Server] = true
				subMu.Unlock()
				safeSend(map[string]any{"t": "history", "s": in.Server, "d": inst.ConsoleLines(1500)})
			case "unsub":
				subMu.Lock()
				delete(consoleSubs, in.Server)
				subMu.Unlock()
			case "cmd":
				inst := a.Servers.Get(in.Server)
				if inst == nil || !can(in.Server, perms.ConsoleCommand) {
					safeSend(map[string]any{"t": "error", "s": in.Server, "d": "Keine Berechtigung für Befehle"})
					continue
				}
				if len(in.Cmd) > 2000 {
					continue
				}
				if err := inst.Command(in.Cmd, u.Username); err != nil {
					safeSend(map[string]any{"t": "error", "s": in.Server, "d": err.Error()})
					continue
				}
				a.audit(r, in.Server, "server.command", map[string]any{"command": in.Cmd})
			case "ping":
				safeSend(map[string]any{"t": "pong"})
			}
		}
	}()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	safeSend(map[string]any{"t": "hello"})
	for {
		select {
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return nil
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				return nil
			}
		case e, okc := <-sub.C:
			if !okc {
				return nil
			}
			if e.Type == events.ServerMeta {
				refresh()
				if !can(e.Server, "") && !u.IsAdmin {
					// a deleted server or lost access is still announced so the UI can react
					if m, ok := e.Data.(map[string]any); !ok || m["action"] != "deleted" {
						continue
					}
				}
			}
			if err := safeSend(e); err != nil {
				return nil
			}
			if sub.TakeOverflow() {
				safeSend(map[string]any{"t": "resync"})
			}
		}
	}
}
