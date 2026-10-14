package minecraft

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// Properties is an order and comment preserving editor for server.properties.
type Properties struct {
	lines []propLine
	index map[string]int
}

type propLine struct {
	raw   string // original line for comments/blank lines
	key   string
	value string
}

func ParseProperties(data []byte) *Properties {
	p := &Properties{index: map[string]int{}}
	// MC writes UTF-8 nowadays; older versions ISO-8859-1. Escapes cover both.
	if !utf8.Valid(data) {
		var b strings.Builder
		for _, c := range data {
			b.WriteRune(rune(c))
		}
		data = []byte(b.String())
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			p.lines = append(p.lines, propLine{raw: line})
			continue
		}
		key, value := splitProp(trimmed)
		if _, dup := p.index[key]; dup {
			p.lines[p.index[key]].value = value
			continue
		}
		p.index[key] = len(p.lines)
		p.lines = append(p.lines, propLine{key: key, value: value})
	}
	return p
}

func LoadProperties(path string) (*Properties, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ParseProperties(nil), nil
		}
		return nil, err
	}
	return ParseProperties(b), nil
}

func splitProp(s string) (string, string) {
	var key strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			key.WriteByte(s[i+1])
			i += 2
			continue
		}
		if c == '=' || c == ':' || c == ' ' || c == '\t' {
			break
		}
		key.WriteByte(c)
		i++
	}
	rest := strings.TrimLeft(s[i:], " \t")
	if len(rest) > 0 && (rest[0] == '=' || rest[0] == ':') {
		rest = strings.TrimLeft(rest[1:], " \t")
	}
	return key.String(), unescapeProp(rest)
}

func unescapeProp(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if i+4 < len(s) {
				var r rune
				if _, err := fmt.Sscanf(s[i+1:i+5], "%04x", &r); err == nil {
					b.WriteRune(r)
					i += 4
					continue
				}
			}
			b.WriteByte('u')
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func escapeProp(s string, isKey bool) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case (r == '=' || r == ':') && isKey:
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == ' ' && (isKey || i == 0):
			b.WriteString(`\ `)
		case r == '#' || r == '!':
			if i == 0 {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		case r > 0x7e || r < 0x20:
			if r > 0xffff {
				for _, u := range utf16Pair(r) {
					fmt.Fprintf(&b, `\u%04x`, u)
				}
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func utf16Pair(r rune) [2]rune {
	r -= 0x10000
	return [2]rune{0xd800 + (r>>10)&0x3ff, 0xdc00 + r&0x3ff}
}

func (p *Properties) Get(key string) (string, bool) {
	i, ok := p.index[key]
	if !ok {
		return "", false
	}
	return p.lines[i].value, true
}

func (p *Properties) GetDefault(key, def string) string {
	if v, ok := p.Get(key); ok {
		return v
	}
	return def
}

func (p *Properties) Set(key, value string) {
	if i, ok := p.index[key]; ok {
		p.lines[i].value = value
		return
	}
	p.index[key] = len(p.lines)
	p.lines = append(p.lines, propLine{key: key, value: value})
}

func (p *Properties) Delete(key string) {
	i, ok := p.index[key]
	if !ok {
		return
	}
	p.lines = append(p.lines[:i], p.lines[i+1:]...)
	p.index = map[string]int{}
	for j, l := range p.lines {
		if l.key != "" {
			p.index[l.key] = j
		}
	}
}

// Map returns all key/value pairs.
func (p *Properties) Map() map[string]string {
	out := make(map[string]string, len(p.index))
	for k, i := range p.index {
		out[k] = p.lines[i].value
	}
	return out
}

func (p *Properties) Bytes() []byte {
	var b bytes.Buffer
	for _, l := range p.lines {
		if l.key == "" {
			b.WriteString(l.raw)
		} else {
			b.WriteString(escapeProp(l.key, true))
			b.WriteByte('=')
			b.WriteString(escapeProp(l.value, false))
		}
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// PropInfo describes a known server.properties key for the settings form.
type PropInfo struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"` // bool, int, string, enum
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Group       string   `json:"group"`
	Options     []string `json:"options,omitempty"`
	Default     string   `json:"default"`
	Managed     bool     `json:"managed,omitempty"` // controlled by the panel
	Min         int      `json:"min,omitempty"`
	Max         int      `json:"max,omitempty"`
}

var KnownProperties = []PropInfo{
	{Key: "motd", Type: "string", Label: "MOTD", Description: "Nachricht in der Serverliste (§-Farbcodes erlaubt)", Group: "Allgemein", Default: "A Minecraft Server"},
	{Key: "max-players", Type: "int", Label: "Max. Spieler", Description: "Maximale Anzahl gleichzeitiger Spieler", Group: "Allgemein", Default: "20", Min: 1, Max: 10000},
	{Key: "difficulty", Type: "enum", Label: "Schwierigkeit", Options: []string{"peaceful", "easy", "normal", "hard"}, Group: "Gameplay", Default: "easy"},
	{Key: "gamemode", Type: "enum", Label: "Spielmodus", Options: []string{"survival", "creative", "adventure", "spectator"}, Group: "Gameplay", Default: "survival"},
	{Key: "force-gamemode", Type: "bool", Label: "Spielmodus erzwingen", Description: "Spieler bekommen beim Betreten immer den Standard-Spielmodus", Group: "Gameplay", Default: "false"},
	{Key: "hardcore", Type: "bool", Label: "Hardcore", Description: "Nach dem Tod Zuschauermodus", Group: "Gameplay", Default: "false"},
	{Key: "pvp", Type: "bool", Label: "PvP", Description: "Spieler können sich gegenseitig Schaden zufügen", Group: "Gameplay", Default: "true"},
	{Key: "allow-flight", Type: "bool", Label: "Fliegen erlauben", Description: "Verhindert Kicks durch Flug-Erkennung (z. B. für Mods)", Group: "Gameplay", Default: "false"},
	{Key: "spawn-protection", Type: "int", Label: "Spawn-Schutz", Description: "Radius um den Spawn, in dem nur OPs bauen dürfen (0 = aus)", Group: "Gameplay", Default: "16", Min: 0, Max: 1000},
	{Key: "enable-command-block", Type: "bool", Label: "Befehlsblöcke", Group: "Gameplay", Default: "false"},
	{Key: "allow-nether", Type: "bool", Label: "Nether erlauben", Group: "Welt", Default: "true"},
	{Key: "level-name", Type: "string", Label: "Weltordner", Description: "Name des Weltordners", Group: "Welt", Default: "world"},
	{Key: "level-seed", Type: "string", Label: "Seed", Description: "Nur bei neuer Weltgenerierung wirksam", Group: "Welt", Default: ""},
	{Key: "level-type", Type: "enum", Label: "Welttyp", Options: []string{"minecraft:normal", "minecraft:flat", "minecraft:large_biomes", "minecraft:amplified", "minecraft:single_biome_surface"}, Group: "Welt", Default: "minecraft:normal"},
	{Key: "generate-structures", Type: "bool", Label: "Strukturen generieren", Group: "Welt", Default: "true"},
	{Key: "max-world-size", Type: "int", Label: "Max. Weltgröße", Description: "Radius der Weltgrenze in Blöcken", Group: "Welt", Default: "29999984", Min: 1, Max: 29999984},
	{Key: "view-distance", Type: "int", Label: "Sichtweite", Description: "In Chunks (3–32)", Group: "Leistung", Default: "10", Min: 3, Max: 32},
	{Key: "simulation-distance", Type: "int", Label: "Simulationsweite", Description: "In Chunks (3–32)", Group: "Leistung", Default: "10", Min: 3, Max: 32},
	{Key: "entity-broadcast-range-percentage", Type: "int", Label: "Entity-Sichtweite %", Group: "Leistung", Default: "100", Min: 10, Max: 1000},
	{Key: "network-compression-threshold", Type: "int", Label: "Kompressionsschwelle", Description: "Paketgröße in Bytes ab der komprimiert wird (-1 = aus)", Group: "Leistung", Default: "256", Min: -1, Max: 65535},
	{Key: "pause-when-empty-seconds", Type: "int", Label: "Pausieren wenn leer", Description: "Sekunden ohne Spieler bis der Server pausiert (ab 1.21.2, 0 = nie)", Group: "Leistung", Default: "60", Min: 0, Max: 86400},
	{Key: "sync-chunk-writes", Type: "bool", Label: "Synchrones Chunk-Schreiben", Group: "Leistung", Default: "true"},
	{Key: "region-file-compression", Type: "enum", Label: "Region-Kompression", Options: []string{"deflate", "lz4", "none"}, Group: "Leistung", Default: "deflate"},
	{Key: "online-mode", Type: "bool", Label: "Online-Modus", Description: "Spieler über Mojang authentifizieren (aus = Cracked/Proxy)", Group: "Sicherheit", Default: "true"},
	{Key: "white-list", Type: "bool", Label: "Whitelist aktiv", Group: "Sicherheit", Default: "false"},
	{Key: "enforce-whitelist", Type: "bool", Label: "Whitelist erzwingen", Description: "Nicht gelistete Spieler werden beim Neuladen gekickt", Group: "Sicherheit", Default: "false"},
	{Key: "enforce-secure-profile", Type: "bool", Label: "Signierte Chats erzwingen", Group: "Sicherheit", Default: "true"},
	{Key: "prevent-proxy-connections", Type: "bool", Label: "Proxy-Verbindungen verhindern", Group: "Sicherheit", Default: "false"},
	{Key: "op-permission-level", Type: "int", Label: "OP-Rechtestufe", Group: "Sicherheit", Default: "4", Min: 1, Max: 4},
	{Key: "player-idle-timeout", Type: "int", Label: "AFK-Kick", Description: "Minuten bis inaktive Spieler gekickt werden (0 = aus)", Group: "Sicherheit", Default: "0", Min: 0, Max: 10000},
	{Key: "hide-online-players", Type: "bool", Label: "Online-Spieler verbergen", Description: "Spielerliste in der Serverliste ausblenden", Group: "Sicherheit", Default: "false"},
	{Key: "log-ips", Type: "bool", Label: "IPs protokollieren", Group: "Sicherheit", Default: "true"},
	{Key: "resource-pack", Type: "string", Label: "Ressourcenpaket-URL", Group: "Ressourcenpaket", Default: ""},
	{Key: "resource-pack-sha1", Type: "string", Label: "Ressourcenpaket SHA-1", Group: "Ressourcenpaket", Default: ""},
	{Key: "require-resource-pack", Type: "bool", Label: "Ressourcenpaket erzwingen", Group: "Ressourcenpaket", Default: "false"},
	{Key: "resource-pack-prompt", Type: "string", Label: "Ressourcenpaket-Hinweis", Group: "Ressourcenpaket", Default: ""},
	{Key: "enable-query", Type: "bool", Label: "GameSpy-Query", Group: "Netzwerk", Default: "false"},
	{Key: "query.port", Type: "int", Label: "Query-Port", Group: "Netzwerk", Default: "25565", Min: 1, Max: 65535},
	{Key: "accepts-transfers", Type: "bool", Label: "Transfers annehmen", Description: "Spieler dürfen per /transfer hierher verschoben werden", Group: "Netzwerk", Default: "false"},
	{Key: "server-ip", Type: "string", Label: "Bind-Adresse", Description: "Leer lassen für alle Netzwerkschnittstellen", Group: "Netzwerk", Default: ""},
	{Key: "server-port", Type: "int", Label: "Port", Group: "Netzwerk", Default: "25565", Managed: true},
	{Key: "enable-rcon", Type: "bool", Label: "RCON", Group: "Netzwerk", Default: "false", Managed: true},
	{Key: "rcon.port", Type: "int", Label: "RCON-Port", Group: "Netzwerk", Default: "25575", Managed: true},
	{Key: "rcon.password", Type: "string", Label: "RCON-Passwort", Group: "Netzwerk", Default: "", Managed: true},
	{Key: "broadcast-rcon-to-ops", Type: "bool", Label: "RCON an OPs senden", Group: "Netzwerk", Default: "true", Managed: true},
}

// managedKeys are written by the panel before every start when RCON management is on.
var managedKeys = map[string]bool{"server-port": true, "enable-rcon": true, "rcon.port": true, "rcon.password": true, "broadcast-rcon-to-ops": true}

// IsManagedKey reports whether the panel controls this server.properties key.
func IsManagedKey(k string) bool { return managedKeys[k] }
