package minecraft

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hearth/internal/httpx"
)

// ListEntry is an entry of whitelist.json, ops.json, banned-players.json or banned-ips.json.
type ListEntry struct {
	UUID                string `json:"uuid,omitempty"`
	Name                string `json:"name,omitempty"`
	IP                  string `json:"ip,omitempty"`
	Level               int    `json:"level,omitempty"`
	BypassesPlayerLimit bool   `json:"bypassesPlayerLimit,omitempty"`
	Created             string `json:"created,omitempty"`
	Source              string `json:"source,omitempty"`
	Expires             string `json:"expires,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

var ListFiles = map[string]string{
	"whitelist": "whitelist.json",
	"ops":       "ops.json",
	"bans":      "banned-players.json",
	"ipbans":    "banned-ips.json",
}

func ReadList(serverDir, file string) ([]ListEntry, error) {
	b, err := os.ReadFile(filepath.Join(serverDir, file))
	if err != nil {
		if os.IsNotExist(err) {
			return []ListEntry{}, nil
		}
		return nil, err
	}
	var out []ListEntry
	if len(strings.TrimSpace(string(b))) == 0 {
		return []ListEntry{}, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []ListEntry{}
	}
	return out, nil
}

func WriteList(serverDir, file string, entries []ListEntry) error {
	if entries == nil {
		entries = []ListEntry{}
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(serverDir, file+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(serverDir, file))
}

var validName = regexp.MustCompile(`^[A-Za-z0-9_.*-]{1,32}$`)

// ValidPlayerName checks a player name (also allows Bedrock/Floodgate prefixes).
func ValidPlayerName(n string) bool { return validName.MatchString(n) }

func mcDate(t time.Time) string { return t.Format("2006-01-02 15:04:05 -0700") }

// LookupUUID resolves a player name to a UUID via the Mojang API (online mode).
func LookupUUID(ctx context.Context, http *httpx.Client, name string) (string, string, error) {
	var resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	err := http.GetJSON(ctx, "https://api.mojang.com/users/profiles/minecraft/"+name, time.Hour, &resp)
	if err != nil {
		if httpx.IsNotFound(err) {
			return "", "", errors.New("Spieler " + name + " existiert nicht")
		}
		return "", "", err
	}
	return NormalizeUUID(resp.ID), resp.Name, nil
}

// EditListOffline applies a list action directly to the JSON files (server stopped).
func EditListOffline(serverDir, action string, e ListEntry) error {
	file := ""
	switch action {
	case "whitelist_add", "whitelist_remove":
		file = ListFiles["whitelist"]
	case "op", "deop":
		file = ListFiles["ops"]
	case "ban", "pardon":
		file = ListFiles["bans"]
	case "ban_ip", "pardon_ip":
		file = ListFiles["ipbans"]
	default:
		return errors.New("Aktion ist nur bei laufendem Server möglich")
	}
	list, err := ReadList(serverDir, file)
	if err != nil {
		return err
	}
	match := func(x ListEntry) bool {
		if e.IP != "" {
			return x.IP == e.IP
		}
		return (e.UUID != "" && strings.EqualFold(x.UUID, e.UUID)) || (e.Name != "" && strings.EqualFold(x.Name, e.Name))
	}
	var out []ListEntry
	for _, x := range list {
		if !match(x) {
			out = append(out, x)
		}
	}
	now := mcDate(time.Now())
	switch action {
	case "whitelist_add":
		out = append(out, ListEntry{UUID: e.UUID, Name: e.Name})
	case "op":
		out = append(out, ListEntry{UUID: e.UUID, Name: e.Name, Level: 4})
	case "ban":
		reason := e.Reason
		if reason == "" {
			reason = "Banned by an operator."
		}
		out = append(out, ListEntry{UUID: e.UUID, Name: e.Name, Created: now, Source: "Hearth", Expires: "forever", Reason: reason})
	case "ban_ip":
		reason := e.Reason
		if reason == "" {
			reason = "Banned by an operator."
		}
		out = append(out, ListEntry{IP: e.IP, Created: now, Source: "Hearth", Expires: "forever", Reason: reason})
	}
	return WriteList(serverDir, file, out)
}
