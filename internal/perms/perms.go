// Package perms defines the per-server permission model.
//
// Global admins have every permission on every server. The owner of a server
// has every permission on that server. Other users ("members") get an explicit
// list of permissions per server.
package perms

import "slices"

const (
	ConsoleRead    = "console.read"
	ConsoleCommand = "console.command"
	PowerStart     = "power.start"
	PowerStop      = "power.stop" // stop + restart
	PowerKill      = "power.kill"
	FilesRead      = "files.read"
	FilesWrite     = "files.write"
	FilesDelete    = "files.delete"
	BackupsRead    = "backups.read"
	BackupsCreate  = "backups.create"
	BackupsRestore = "backups.restore"
	BackupsDelete  = "backups.delete"
	SchedulesRead  = "schedules.read"
	SchedulesEdit  = "schedules.edit"
	PlayersRead    = "players.read"
	PlayersManage  = "players.manage" // kick, ban, op, whitelist
	PluginsManage  = "plugins.manage"
	SettingsRead   = "settings.read"
	SettingsEdit   = "settings.edit" // startup flags, server.properties, version
	MapManage      = "map.manage"
	PublicManage   = "public.manage"
	WebhooksManage = "webhooks.manage"
	MembersManage  = "members.manage"
	ActivityRead   = "activity.read"
)

type Info struct {
	Key         string `json:"key"`
	Group       string `json:"group"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// All lists every permission with German UI labels, grouped for the permission matrix.
var All = []Info{
	{ConsoleRead, "Konsole", "Konsole ansehen", "Live-Konsole und Logs lesen"},
	{ConsoleCommand, "Konsole", "Befehle senden", "Befehle in die Server-Konsole schicken"},
	{PowerStart, "Steuerung", "Starten", "Server starten"},
	{PowerStop, "Steuerung", "Stoppen & Neustarten", "Server stoppen und neu starten"},
	{PowerKill, "Steuerung", "Beenden erzwingen", "Prozess hart beenden (Kill)"},
	{FilesRead, "Dateien", "Dateien ansehen", "Dateien durchsuchen, lesen und herunterladen"},
	{FilesWrite, "Dateien", "Dateien bearbeiten", "Dateien hochladen, bearbeiten, umbenennen, entpacken"},
	{FilesDelete, "Dateien", "Dateien löschen", "Dateien und Ordner löschen"},
	{BackupsRead, "Backups", "Backups ansehen", "Backups auflisten und herunterladen"},
	{BackupsCreate, "Backups", "Backups erstellen", "Neue Backups anlegen"},
	{BackupsRestore, "Backups", "Backups wiederherstellen", "Server aus einem Backup zurücksetzen"},
	{BackupsDelete, "Backups", "Backups löschen", "Backups löschen und sperren"},
	{SchedulesRead, "Zeitpläne", "Zeitpläne ansehen", "Geplante Aufgaben ansehen"},
	{SchedulesEdit, "Zeitpläne", "Zeitpläne bearbeiten", "Geplante Aufgaben anlegen, ändern, ausführen"},
	{PlayersRead, "Spieler", "Spieler ansehen", "Spielerliste, Statistiken und Ereignisse ansehen"},
	{PlayersManage, "Spieler", "Spieler verwalten", "Kicken, bannen, OP vergeben, Whitelist pflegen"},
	{PluginsManage, "Erweiterungen", "Plugins & Mods", "Plugins/Mods installieren, aktualisieren, entfernen"},
	{SettingsRead, "Einstellungen", "Einstellungen ansehen", "Startparameter und Serverkonfiguration ansehen"},
	{SettingsEdit, "Einstellungen", "Einstellungen ändern", "Startparameter, server.properties und Version ändern"},
	{MapManage, "Karte", "Karte verwalten", "Karte rendern und Karteneinstellungen ändern"},
	{PublicManage, "Öffentliche Seite", "Öffentliche Seite", "Öffentliche Statusseite konfigurieren"},
	{WebhooksManage, "Benachrichtigungen", "Webhooks", "Discord-/Webhook-Benachrichtigungen verwalten"},
	{MembersManage, "Zugriff", "Mitglieder verwalten", "Anderen Benutzern Zugriff auf diesen Server geben"},
	{ActivityRead, "Zugriff", "Aktivitätsprotokoll", "Protokoll aller Aktionen auf diesem Server ansehen"},
}

var known = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range All {
		m[p.Key] = true
	}
	return m
}()

func Valid(p string) bool { return known[p] }

// Sanitize removes unknown and duplicate permissions.
func Sanitize(ps []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, p := range ps {
		if known[p] && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// AllKeys returns every permission key.
func AllKeys() []string {
	out := make([]string, len(All))
	for i, p := range All {
		out[i] = p.Key
	}
	return out
}

type Preset struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

var Presets = []Preset{
	{"viewer", "Zuschauer", "Kann Konsole, Spieler und Statistiken ansehen",
		[]string{ConsoleRead, PlayersRead, SettingsRead, BackupsRead, SchedulesRead}},
	{"moderator", "Moderator", "Kann Befehle senden, Spieler verwalten und den Server neu starten",
		[]string{ConsoleRead, ConsoleCommand, PowerStart, PowerStop, PlayersRead, PlayersManage, FilesRead,
			BackupsRead, BackupsCreate, SchedulesRead, SettingsRead, ActivityRead}},
	{"admin", "Co-Admin", "Fast alles außer Mitgliederverwaltung",
		func() []string {
			var out []string
			for _, p := range All {
				if p.Key != MembersManage {
					out = append(out, p.Key)
				}
			}
			return out
		}()},
}

// Set is a resolved permission set for a user on a server.
type Set struct {
	All   bool // admin or owner
	Owner bool
	perms map[string]bool
}

func Full(owner bool) *Set { return &Set{All: true, Owner: owner} }

func FromList(ps []string) *Set {
	s := &Set{perms: map[string]bool{}}
	for _, p := range ps {
		s.perms[p] = true
	}
	return s
}

func (s *Set) Has(p string) bool {
	if s == nil {
		return false
	}
	return s.All || s.perms[p]
}

// List returns the effective permission keys.
func (s *Set) List() []string {
	if s == nil {
		return []string{}
	}
	if s.All {
		return AllKeys()
	}
	out := []string{}
	for _, p := range All {
		if s.perms[p.Key] {
			out = append(out, p.Key)
		}
	}
	return out
}

// Subset reports whether every permission in ps is held by s
// (a member may only grant permissions they have themselves).
func (s *Set) Subset(ps []string) bool {
	for _, p := range ps {
		if !s.Has(p) {
			return false
		}
	}
	return true
}
