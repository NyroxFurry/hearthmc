// Package minecraft implements the Minecraft: Java Edition integration:
// installation of all common server types, Java management, RCON, live
// player tracking from the console, statistics import, player list
// management, plugin/mod installation via Modrinth and world maps.
package minecraft

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"hearth/internal/auth"
	"hearth/internal/game"
	"hearth/internal/httpx"
	"hearth/internal/javart"
	"hearth/internal/store"
)

// Settings is the game specific configuration stored in servers.settings.
type Settings struct {
	Flavor          string      `json:"flavor"`
	Version         string      `json:"version"`
	Build           string      `json:"build"`
	Jar             string      `json:"jar"`
	Java            string      `json:"java"` // auto | system | /path/to/java
	JavaMajor       int         `json:"javaMajor"`
	FlagsPreset     string      `json:"flagsPreset"` // aikar | none
	JVMArgs         string      `json:"jvmArgs"`
	ServerArgs      string      `json:"serverArgs"`
	MinMemoryMB     int         `json:"minMemoryMb"`
	RCONPort        int         `json:"rconPort"`
	RCONPassword    string      `json:"rconPassword"`
	ManageRCON      bool        `json:"manageRcon"`
	EULA            bool        `json:"eula"`
	TrackChat       bool        `json:"trackChat"`
	StartupOverride string      `json:"startupOverride"`
	Map             MapSettings `json:"map"`
	BackupIgnore    *[]string   `json:"backupIgnore,omitempty"`

	InstalledVersion string `json:"installedVersion"`
	InstalledBuild   string `json:"installedBuild"`
	LoaderArgsFile   string `json:"loaderArgsFile"`
}

type MapSettings struct {
	Enabled         bool     `json:"enabled"`
	Dimensions      []string `json:"dimensions"`      // overworld, the_nether, the_end
	IntervalMinutes int      `json:"intervalMinutes"` // auto render while running (0 = manual only)
	ExternalURL     string   `json:"externalUrl"`     // BlueMap / Dynmap / squaremap
	LivePlayers     bool     `json:"livePlayers"`     // poll player positions via RCON
	// Plugin is a map plugin installed and configured by the panel (bluemap,
	// squaremap, dynmap); its web server listens on PluginPort (internal).
	Plugin     string `json:"plugin,omitempty"`
	PluginPort int    `json:"pluginPort,omitempty"`
}

func DecodeSettings(raw json.RawMessage) Settings {
	var s Settings
	if len(raw) > 0 {
		json.Unmarshal(raw, &s)
	}
	if s.Java == "" {
		s.Java = "auto"
	}
	if s.FlagsPreset == "" {
		s.FlagsPreset = "aikar"
	}
	if s.Jar == "" {
		s.Jar = "server.jar"
	}
	return s
}

func (s Settings) Encode() json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

type Game struct {
	Versions *Versions
	Java     *javart.Manager
	HTTP     *httpx.Client
	Modrinth *Modrinth
	Heads    *Heads
}

func (g *Game) ID() string   { return "minecraft" }
func (g *Game) Name() string { return "Minecraft: Java Edition" }

func (g *Game) NewHandler(h game.Host) game.Handler { return newHandler(g, h) }

// ValidateSettings normalizes Minecraft settings; on creation it fills defaults
// (RCON password, map settings).
func (g *Game) ValidateSettings(srv *store.Server, isNew bool) error {
	s := DecodeSettings(srv.Settings)
	if _, ok := FlavorByID(s.Flavor); !ok {
		return fmt.Errorf("unbekannte Server-Art %q", s.Flavor)
	}
	if s.Flavor != "custom" && strings.TrimSpace(s.Version) == "" {
		return errors.New("Bitte eine Minecraft-Version wählen")
	}
	if s.Java != "auto" && s.Java != "system" && !strings.ContainsAny(s.Java, `/\`) {
		return errors.New("Java muss „auto“, „system“ oder ein absoluter Pfad sein")
	}
	switch s.FlagsPreset {
	case "aikar", "none":
	default:
		return errors.New("unbekanntes Flag-Preset")
	}
	if strings.ContainsAny(s.JVMArgs+s.ServerArgs, "\r\n") {
		return errors.New("Startparameter dürfen keine Zeilenumbrüche enthalten")
	}
	if strings.Contains(s.Jar, "..") || strings.HasPrefix(s.Jar, "/") {
		return errors.New("ungültiger JAR-Pfad")
	}
	if s.MinMemoryMB < 0 || (srv.MemoryMB > 0 && s.MinMemoryMB > srv.MemoryMB) {
		return errors.New("Startspeicher (Xms) muss kleiner als der maximale Speicher sein")
	}
	if isNew {
		if !s.EULA {
			return errors.New("Die Minecraft-EULA muss akzeptiert werden")
		}
		if s.RCONPassword == "" {
			s.RCONPassword = auth.RandomToken(24)
		}
		s.ManageRCON = true
		if len(s.Map.Dimensions) == 0 {
			plugin, port := s.Map.Plugin, s.Map.PluginPort
			s.Map = MapSettings{Enabled: true, Dimensions: []string{"overworld", "the_nether", "the_end"}, IntervalMinutes: 30, LivePlayers: true}
			s.Map.Plugin, s.Map.PluginPort = plugin, port
		}
	}
	if s.Map.Plugin != "" {
		p, ok := MapPluginByID(s.Map.Plugin)
		if !ok {
			return fmt.Errorf("unbekanntes Karten-Plugin %q", s.Map.Plugin)
		}
		if !p.Supports(s.Flavor) {
			return fmt.Errorf("%s gibt es nicht für diese Server-Art", p.Name)
		}
	}
	for _, d := range s.Map.Dimensions {
		if strings.ContainsAny(d, `/\.`) {
			return errors.New("ungültige Dimension")
		}
	}
	if s.Map.IntervalMinutes < 0 || s.Map.IntervalMinutes > 1440 {
		return errors.New("Render-Intervall muss zwischen 0 und 1440 Minuten liegen")
	}
	if u := s.Map.ExternalURL; u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return errors.New("Externe Karten-URL muss mit http:// oder https:// beginnen")
	}
	srv.Settings = s.Encode()
	return nil
}

// aikarFlags are the widely used G1 tuning flags (https://mcflags.emc.gs).
func aikarFlags(memMB int) []string {
	newSize, maxNew, region, reserve, ihop := "30", "40", "8M", "20", "15"
	if memMB >= 12*1024 {
		newSize, maxNew, region, reserve, ihop = "40", "50", "16M", "15", "20"
	}
	return []string{
		"-XX:+IgnoreUnrecognizedVMOptions",
		"-XX:+UseG1GC", "-XX:+ParallelRefProcEnabled", "-XX:MaxGCPauseMillis=200",
		"-XX:+UnlockExperimentalVMOptions", "-XX:+DisableExplicitGC", "-XX:+AlwaysPreTouch",
		"-XX:G1NewSizePercent=" + newSize, "-XX:G1MaxNewSizePercent=" + maxNew,
		"-XX:G1HeapRegionSize=" + region, "-XX:G1ReservePercent=" + reserve,
		"-XX:G1HeapWastePercent=5", "-XX:G1MixedGCCountTarget=4",
		"-XX:InitiatingHeapOccupancyPercent=" + ihop, "-XX:G1MixedGCLiveThresholdPercent=90",
		"-XX:G1RSetUpdatingPauseTimePercent=5", "-XX:SurvivorRatio=32", "-XX:+PerfDisableSharedMem",
		"-XX:MaxTenuringThreshold=1", "-Dusing.aikars.flags=https://mcflags.emc.gs", "-Daikars.new.flags=true",
	}
}

// SplitArgs splits a command line into arguments, honouring single and double quotes.
func SplitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inArg := false
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inArg = true
		case r == ' ' || r == '\t':
			if inArg {
				out = append(out, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if inArg {
		out = append(out, cur.String())
	}
	return out
}
