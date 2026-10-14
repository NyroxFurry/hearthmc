// Package notify delivers events to Discord and generic webhooks.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"hearth/internal/settings"
	"hearth/internal/store"
	"hearth/internal/version"
)

type EventInfo struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

var Events = []EventInfo{
	{"server.started", "Server gestartet"},
	{"server.stopped", "Server gestoppt"},
	{"server.crashed", "Server abgestürzt"},
	{"backup.completed", "Backup fertig"},
	{"backup.failed", "Backup fehlgeschlagen"},
	{"player.join", "Spieler betritt den Server"},
	{"player.leave", "Spieler verlässt den Server"},
	{"player.death", "Spieler stirbt"},
	{"player.advancement", "Spieler erzielt Fortschritt"},
	{"player.chat", "Chatnachricht"},
}

func ValidEvent(e string) bool {
	return slices.ContainsFunc(Events, func(i EventInfo) bool { return i.Key == e })
}

var colors = map[string]int{
	"server.started": 0x3ba55d, "server.stopped": 0x8a8f98, "server.crashed": 0xed4245,
	"backup.completed": 0x5865f2, "backup.failed": 0xed4245,
	"player.join": 0x57f287, "player.leave": 0xfaa61a, "player.death": 0x992d22,
	"player.advancement": 0xfee75c, "player.chat": 0x99aab5,
}

type Message struct {
	Event      string    `json:"event"`
	ServerID   string    `json:"serverId"`
	ServerName string    `json:"serverName"`
	Title      string    `json:"title"`
	Text       string    `json:"text"`
	PlayerName string    `json:"playerName,omitempty"`
	PlayerUUID string    `json:"playerUuid,omitempty"`
	At         time.Time `json:"at"`
}

type Notifier struct {
	Store    *store.Store
	Settings *settings.Service
	Log      *slog.Logger

	client *http.Client
	queue  chan job
}

type job struct {
	hook *store.Webhook
	msg  Message
	test bool
	done chan error
}

func (n *Notifier) Start(ctx context.Context) {
	n.client = &http.Client{Timeout: 15 * time.Second}
	n.queue = make(chan job, 500)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-n.queue:
				err := n.deliver(j.hook, j.msg)
				if j.done != nil {
					j.done <- err
				}
			}
		}
	}()
}

// Send dispatches msg to all enabled hooks subscribed to its event.
func (n *Notifier) Send(msg Message) {
	if n.queue == nil {
		return
	}
	if msg.At.IsZero() {
		msg.At = time.Now()
	}
	hooks, err := n.Store.ListWebhooks(context.Background(), "*")
	if err != nil {
		return
	}
	for _, h := range hooks {
		if !h.Enabled || (h.ServerID != "" && h.ServerID != msg.ServerID) || !slices.Contains(h.Events, msg.Event) {
			continue
		}
		select {
		case n.queue <- job{hook: h, msg: msg}:
		default:
			n.Log.Warn("webhook queue full, dropping event", "event", msg.Event)
		}
	}
}

// Test sends a test message synchronously.
func (n *Notifier) Test(h *store.Webhook) error {
	done := make(chan error, 1)
	msg := Message{Event: "server.started", ServerName: "Test", Title: "Testnachricht",
		Text: "Dieser Webhook ist korrekt eingerichtet. ✅", At: time.Now()}
	select {
	case n.queue <- job{hook: h, msg: msg, test: true, done: done}:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("Warteschlange voll")
	}
	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		return fmt.Errorf("Zeitüberschreitung")
	}
}

func (n *Notifier) deliver(h *store.Webhook, msg Message) error {
	var body any
	switch h.Kind {
	case "discord":
		embed := map[string]any{
			"title":       msg.Title,
			"description": msg.Text,
			"color":       colors[msg.Event],
			"timestamp":   msg.At.UTC().Format(time.RFC3339),
			"footer":      map[string]string{"text": msg.ServerName},
		}
		if base := strings.TrimRight(n.Settings.Get(settings.PanelURL), "/"); base != "" && msg.PlayerUUID != "" {
			embed["thumbnail"] = map[string]string{"url": base + "/api/heads/" + msg.PlayerUUID + "?size=64"}
		}
		body = map[string]any{
			"username": n.Settings.Get(settings.PanelName),
			"embeds":   []any{embed},
		}
	default:
		body = msg
	}
	b, _ := json.Marshal(body)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, _ := http.NewRequest("POST", h.URL, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", version.UserAgent(n.Settings.Get(settings.Contact)))
		resp, err := n.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode == 429 {
			wait := 2 * time.Second
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if f, err := strconv.ParseFloat(ra, 64); err == nil {
					wait = time.Duration(f*1000) * time.Millisecond
				}
			}
			if wait > 30*time.Second {
				wait = 30 * time.Second
			}
			time.Sleep(wait)
			lastErr = fmt.Errorf("rate limited")
			continue
		}
		if resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			break
		}
		lastErr = nil
		break
	}
	status := "ok " + time.Now().Format("02.01. 15:04")
	if lastErr != nil {
		status = "Fehler: " + lastErr.Error()
	}
	if h.ID != 0 {
		n.Store.SetWebhookStatus(context.Background(), h.ID, status)
	}
	return lastErr
}
