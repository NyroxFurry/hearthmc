package minecraft

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Karten-Plugins, die das Panel selbst installieren und einrichten kann. Ihr
// Webserver lauscht nur intern (127.0.0.1, im Container auf allen Adressen
// mit Weiterleitung nur nach 127.0.0.1); das Panel reicht die Karte auf seinem
// Kartenport an Browser durch.

type MapPluginInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Project     string   `json:"project"` // Modrinth slug
	Description string   `json:"description"`
	Flavors     []string `json:"flavors"`
	// ids in plugin.yml / fabric.mod.json / mods.toml to recognise an installed jar
	jarIDs []string
}

var MapPlugins = []MapPluginInfo{
	{
		ID: "bluemap", Name: "BlueMap", Project: "bluemap",
		Description: "3D-Karte, in der man frei herumfliegen kann, mit Live-Spielern. Lädt einmalig die Minecraft-Texturen von Mojang und braucht beim ersten Rendern etwas Rechenzeit.",
		Flavors:     []string{"paper", "purpur", "custom", "fabric", "forge", "neoforge"},
		jarIDs:      []string{"bluemap"},
	},
	{
		ID: "squaremap", Name: "squaremap", Project: "squaremap",
		Description: "Schnelle, schlanke 2D-Karte von oben mit Live-Spielern. Schont die Leistung des Servers.",
		Flavors:     []string{"paper", "purpur", "custom", "fabric", "neoforge"},
		jarIDs:      []string{"squaremap"},
	},
	{
		ID: "dynmap", Name: "Dynmap", Project: "dynmap",
		Description: "Der Klassiker mit Draufsicht und Schrägansicht, vielen Erweiterungen und Live-Spielern.",
		Flavors:     []string{"paper", "purpur", "custom", "fabric", "forge"},
		jarIDs:      []string{"dynmap"},
	},
}

// MapPluginByID returns a known map plugin.
func MapPluginByID(id string) (MapPluginInfo, bool) {
	for _, p := range MapPlugins {
		if p.ID == id {
			return p, true
		}
	}
	return MapPluginInfo{}, false
}

// Supports reports whether the plugin is available for a server type.
func (p MapPluginInfo) Supports(flavor string) bool {
	for _, f := range p.Flavors {
		if f == flavor {
			return true
		}
	}
	return false
}

// FindMapPluginJar returns the installed jar of a map plugin ("" if missing)
// and whether it is enabled. Add-ons such as "BlueMap-Towny" are not mistaken
// for the plugin itself.
func FindMapPluginJar(serverDir, flavor, id string) (string, bool) {
	p, ok := MapPluginByID(id)
	dir := ModDir(flavor)
	if !ok || dir == "" {
		return "", false
	}
	found, enabled, best := "", false, 0
	for _, jar := range ListPlugins(filepath.Join(serverDir, dir)) {
		file := strings.ToLower(strings.TrimSuffix(jar.File, ".disabled"))
		score := 0
		for _, want := range p.jarIDs {
			switch {
			case strings.EqualFold(jar.ID, want), strings.EqualFold(jar.Name, want):
				score = max(score, 3)
			case jar.ID != "":
				// the jar names itself, and it is something else (e.g. an add-on)
			case file == want+".jar", strings.HasPrefix(file, want+"-"):
				score = max(score, 2)
			}
		}
		if score == 0 {
			continue
		}
		if score > best || (score == best && jar.Enabled && !enabled) {
			found, enabled, best = jar.File, jar.Enabled, score
		}
	}
	return found, enabled
}

// MapPluginBind is the address the plugin's web server should listen on.
func MapPluginBind(runtimeName string) string {
	if runtimeName == "docker" {
		// inside the container; Docker publishes the port only on 127.0.0.1
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

// ConfigureMapPlugin writes or updates the configuration of a map plugin so
// that its web server listens on bind:port. Other settings stay untouched.
func ConfigureMapPlugin(serverDir, flavor, id string, port int, bind string) error {
	if port <= 0 {
		return errors.New("kein Port für die Karte vergeben")
	}
	plugins := ModDir(flavor) == "plugins"
	switch id {
	case "bluemap":
		dir := filepath.Join(serverDir, "config", "bluemap")
		if plugins {
			dir = filepath.Join(serverDir, "plugins", "BlueMap")
		}
		return configureBlueMap(dir, port, bind)
	case "squaremap":
		dir := filepath.Join(serverDir, "squaremap")
		if plugins {
			dir = filepath.Join(serverDir, "plugins", "squaremap")
		}
		return configureSquaremap(filepath.Join(dir, "config.yml"), port, bind)
	case "dynmap":
		dir := filepath.Join(serverDir, "dynmap")
		if plugins {
			dir = filepath.Join(serverDir, "plugins", "dynmap")
		}
		jar := ""
		if f, _ := FindMapPluginJar(serverDir, flavor, id); f != "" {
			jar = filepath.Join(serverDir, ModDir(flavor), f)
		}
		return configureDynmap(filepath.Join(dir, "configuration.txt"), jar, port, bind)
	}
	return fmt.Errorf("unbekanntes Karten-Plugin %q", id)
}

func writeConfig(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".hearth-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// setLine replaces the first line matching re with line, or appends it.
func setLine(text string, re *regexp.Regexp, line string) string {
	if loc := re.FindStringIndex(text); loc != nil {
		return text[:loc[0]] + line + text[loc[1]:]
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + line + "\n"
}

// ---- BlueMap (HOCON) ----

const blueMapCore = `# Angelegt von Hearth. Alle Einstellungen: https://bluemap.bluecolored.de/wiki/
#
# Mit accept-download: true bestätigst du die Minecraft-EULA (https://aka.ms/MinecraftEULA).
# BlueMap lädt dann einmalig die Minecraft-Client-Datei von Mojang, um Blöcke zu texturieren.
accept-download: true

# Ordner für Renderdaten (relativ zum Serververzeichnis)
data: "bluemap"

# Anzahl der Render-Threads; mehr ist schneller, belastet aber den Server
render-thread-count: 1
`

func configureBlueMap(dir string, port int, bind string) error {
	core := filepath.Join(dir, "core.conf")
	if b, err := os.ReadFile(core); err == nil {
		// the panel asked for consent when the plugin was chosen
		text := setLine(string(b), regexp.MustCompile(`(?m)^accept-download\s*[:=].*$`), "accept-download: true")
		if text != string(b) {
			if err := writeConfig(core, []byte(text)); err != nil {
				return err
			}
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := writeConfig(core, []byte(blueMapCore)); err != nil {
			return err
		}
	} else {
		return err
	}
	web := filepath.Join(dir, "webserver.conf")
	b, err := os.ReadFile(web)
	if errors.Is(err, os.ErrNotExist) {
		b = []byte("# Angelegt von Hearth. Das Panel stellt die Karte über seinen Kartenport bereit.\nenabled: true\nwebroot: \"bluemap/web\"\n")
	} else if err != nil {
		return err
	}
	text := string(b)
	text = setLine(text, regexp.MustCompile(`(?m)^enabled\s*[:=].*$`), "enabled: true")
	text = setLine(text, regexp.MustCompile(`(?m)^#?\s*ip\s*[:=].*$`), fmt.Sprintf("ip: %q", bind))
	text = setLine(text, regexp.MustCompile(`(?m)^port\s*[:=].*$`), "port: "+strconv.Itoa(port))
	if text == string(b) {
		return nil
	}
	return writeConfig(web, []byte(text))
}

// ---- squaremap (YAML) ----

func configureSquaremap(path string, port int, bind string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		text := fmt.Sprintf("# Angelegt von Hearth. Weitere Einstellungen ergänzt squaremap beim ersten Start.\nsettings:\n  internal-webserver:\n    enabled: true\n    bind: %s\n    port: %d\n", bind, port)
		return writeConfig(path, []byte(text))
	} else if err != nil {
		return err
	}
	text, err := setYAMLKeys(string(b), []string{"settings", "internal-webserver"}, [][2]string{{"enabled", "true"}, {"bind", bind}, {"port", strconv.Itoa(port)}})
	if err != nil {
		return err
	}
	if text == string(b) {
		return nil
	}
	return writeConfig(path, []byte(text))
}

// setYAMLKeys sets scalar keys inside a nested block of a simple YAML file
// (block style, as written by Configurate), creating missing blocks.
func setYAMLKeys(text string, path []string, kv [][2]string) (string, error) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	indentOf := func(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }
	isBlank := func(l string) bool {
		t := strings.TrimSpace(l)
		return t == "" || strings.HasPrefix(t, "#")
	}
	keyOf := func(l string) string {
		t := strings.TrimSpace(l)
		k, _, ok := strings.Cut(t, ":")
		if !ok {
			return ""
		}
		return strings.Trim(k, `"'`)
	}
	// locate (or create) the block for path
	start, end, indent := 0, len(lines), -2
	for _, seg := range path {
		found := -1
		for i := start; i < end; i++ {
			l := lines[i]
			if isBlank(l) || indentOf(l) != indent+2 {
				continue
			}
			if keyOf(l) == seg {
				found = i
				break
			}
		}
		if found < 0 {
			ins := strings.Repeat(" ", indent+2) + seg + ":"
			lines = append(lines[:end], append([]string{ins}, lines[end:]...)...)
			found = end
		}
		indent += 2
		start = found + 1
		end = start
		for end < len(lines) && (isBlank(lines[end]) || indentOf(lines[end]) > indent) {
			end++
		}
		// do not swallow trailing comments/blank lines that belong to the next block
		for end > start && isBlank(lines[end-1]) {
			end--
		}
	}
	for _, p := range kv {
		key, val := p[0], p[1]
		set := false
		for i := start; i < end; i++ {
			l := lines[i]
			if isBlank(l) || indentOf(l) != indent+2 {
				continue
			}
			if keyOf(l) == key {
				lines[i] = strings.Repeat(" ", indent+2) + key + ": " + val
				set = true
				break
			}
		}
		if !set {
			ins := strings.Repeat(" ", indent+2) + key + ": " + val
			lines = append(lines[:end], append([]string{ins}, lines[end:]...)...)
			end++
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// ---- Dynmap ----

func configureDynmap(path, jar string, port int, bind string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// start from the template inside the jar, so Dynmap gets its full default setup
		if jar == "" {
			return errors.New("Dynmap-Datei nicht gefunden")
		}
		b, err = readJarEntry(jar, "configuration.txt")
		if err != nil {
			return fmt.Errorf("Dynmap-Vorlage: %w", err)
		}
		text := string(b)
		// web chat would let visitors write into the game; the panel only passes GET requests anyway
		text = regexp.MustCompile(`(?m)^(\s+)allowwebchat:\s*true`).ReplaceAllString(text, "${1}allowwebchat: false")
		b = []byte(text)
		if err := writeConfig(path, b); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	text := string(b)
	text = setLine(text, regexp.MustCompile(`(?m)^#?webserver-bindaddress:.*$`), "webserver-bindaddress: "+bind)
	text = setLine(text, regexp.MustCompile(`(?m)^webserver-port:.*$`), "webserver-port: "+strconv.Itoa(port))
	text = setLine(text, regexp.MustCompile(`(?m)^disable-webserver:.*$`), "disable-webserver: false")
	if text == string(b) {
		return nil
	}
	return writeConfig(path, []byte(text))
}

func readJarEntry(jar, name string) ([]byte, error) {
	zr, err := zip.OpenReader(jar)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 4<<20))
		}
	}
	return nil, fmt.Errorf("%s nicht in %s", name, filepath.Base(jar))
}
