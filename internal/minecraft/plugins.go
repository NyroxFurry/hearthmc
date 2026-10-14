package minecraft

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// InstalledPlugin is a plugin or mod jar in the plugins/ or mods/ folder.
type InstalledPlugin struct {
	File        string   `json:"file"`
	Enabled     bool     `json:"enabled"`
	Size        int64    `json:"size"`
	ModTime     int64    `json:"modTime"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Authors     []string `json:"authors"`
	Website     string   `json:"website"`
	Loader      string   `json:"loader"` // bukkit, paper, fabric, quilt, forge, neoforge
	SHA1        string   `json:"sha1,omitempty"`
	// filled from Modrinth when known
	ProjectID string `json:"projectId,omitempty"`
	Slug      string `json:"slug,omitempty"`
	IconURL   string `json:"iconUrl,omitempty"`
	VersionID string `json:"versionId,omitempty"`
	Update    *struct {
		VersionID     string `json:"versionId"`
		VersionNumber string `json:"versionNumber"`
	} `json:"update,omitempty"`
}

// ListPlugins scans dir for *.jar and *.jar.disabled files.
func ListPlugins(dir string) []InstalledPlugin {
	ents, _ := os.ReadDir(dir)
	out := []InstalledPlugin{}
	for _, e := range ents {
		name := e.Name()
		enabled := strings.HasSuffix(name, ".jar")
		if e.IsDir() || !(enabled || strings.HasSuffix(name, ".jar.disabled")) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := InstalledPlugin{File: name, Enabled: enabled, Size: info.Size(), ModTime: info.ModTime().Unix(), Authors: []string{}}
		readJarMeta(filepath.Join(dir, name), &p)
		if p.Name == "" {
			p.Name = strings.TrimSuffix(strings.TrimSuffix(name, ".disabled"), ".jar")
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func readZipFile(zr *zip.Reader, name string) []byte {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil
			}
			defer rc.Close()
			b, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
			return b
		}
	}
	return nil
}

func readJarMeta(path string, p *InstalledPlugin) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return
	}
	zr, err := zip.NewReader(f, st.Size())
	if err != nil {
		return
	}
	if b := readZipFile(zr, "fabric.mod.json"); b != nil {
		var m struct {
			ID          string            `json:"id"`
			Name        string            `json:"name"`
			Version     string            `json:"version"`
			Description string            `json:"description"`
			Authors     []json.RawMessage `json:"authors"`
			Contact     map[string]string `json:"contact"`
		}
		if json.Unmarshal(b, &m) == nil {
			p.Loader, p.ID, p.Name, p.Version, p.Description = "fabric", m.ID, m.Name, m.Version, m.Description
			for _, a := range m.Authors {
				var s string
				var o struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(a, &s) == nil {
					p.Authors = append(p.Authors, s)
				} else if json.Unmarshal(a, &o) == nil && o.Name != "" {
					p.Authors = append(p.Authors, o.Name)
				}
			}
			p.Website = m.Contact["homepage"]
			return
		}
	}
	if b := readZipFile(zr, "quilt.mod.json"); b != nil {
		var m struct {
			QuiltLoader struct {
				ID       string `json:"id"`
				Version  string `json:"version"`
				Metadata struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"metadata"`
			} `json:"quilt_loader"`
		}
		if json.Unmarshal(b, &m) == nil {
			p.Loader, p.ID, p.Version = "quilt", m.QuiltLoader.ID, m.QuiltLoader.Version
			p.Name, p.Description = m.QuiltLoader.Metadata.Name, m.QuiltLoader.Metadata.Description
			return
		}
	}
	for _, tomlName := range []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"} {
		if b := readZipFile(zr, tomlName); b != nil {
			kv := parseModsToml(string(b))
			p.Loader = "forge"
			if strings.Contains(tomlName, "neoforge") {
				p.Loader = "neoforge"
			}
			p.ID, p.Name, p.Description = kv["modId"], kv["displayName"], kv["description"]
			p.Version = kv["version"]
			if strings.Contains(p.Version, "${") {
				p.Version = manifestVersion(zr)
			}
			if a := kv["authors"]; a != "" {
				p.Authors = append(p.Authors, a)
			}
			p.Website = kv["displayURL"]
			return
		}
	}
	for _, yml := range []string{"paper-plugin.yml", "plugin.yml"} {
		if b := readZipFile(zr, yml); b != nil {
			kv, lists := parseSimpleYAML(string(b))
			p.Loader = "bukkit"
			if yml == "paper-plugin.yml" {
				p.Loader = "paper"
			}
			p.Name, p.Version, p.Description, p.Website = kv["name"], kv["version"], kv["description"], kv["website"]
			p.ID = strings.ToLower(p.Name)
			if a := kv["author"]; a != "" {
				p.Authors = append(p.Authors, a)
			}
			p.Authors = append(p.Authors, lists["authors"]...)
			return
		}
	}
	if v := manifestVersion(zr); v != "" {
		p.Version = v
	}
}

func manifestVersion(zr *zip.Reader) string {
	b := readZipFile(zr, "META-INF/MANIFEST.MF")
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "Implementation-Version:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseSimpleYAML reads top-level scalar keys and simple lists of plugin.yml.
func parseSimpleYAML(s string) (map[string]string, map[string][]string) {
	kv := map[string]string{}
	lists := map[string][]string{}
	var lastKey string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "- ") && lastKey != "" {
				lists[lastKey] = append(lists[lastKey], unquoteYAML(strings.TrimPrefix(t, "- ")))
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		lastKey = k
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			for _, item := range strings.Split(strings.Trim(v, "[]"), ",") {
				if item = unquoteYAML(strings.TrimSpace(item)); item != "" {
					lists[k] = append(lists[k], item)
				}
			}
			continue
		}
		if v == ">" || v == "|" || v == ">-" || v == "|-" {
			continue
		}
		kv[k] = unquoteYAML(v)
	}
	return kv, lists
}

func unquoteYAML(v string) string {
	if i := strings.Index(v, " #"); i >= 0 && !strings.HasPrefix(v, `"`) && !strings.HasPrefix(v, "'") {
		v = v[:i]
	}
	v = strings.TrimSpace(v)
	if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
		v = v[1 : len(v)-1]
	}
	return v
}

// parseModsToml extracts the first [[mods]] entry of a (Neo)Forge mods.toml.
func parseModsToml(s string) map[string]string {
	kv := map[string]string{}
	inMods := false
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "[[") || strings.HasPrefix(line, "[") {
			if inMods && line != "[[mods]]" {
				break
			}
			inMods = line == "[[mods]]"
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, `'''`) || strings.HasPrefix(v, `"""`) {
			q := v[:3]
			body := strings.TrimPrefix(v, q)
			for !strings.Contains(body, q) && i+1 < len(lines) {
				i++
				body += "\n" + lines[i]
			}
			v = strings.TrimSpace(body[:strings.Index(body, q)])
		} else {
			if j := strings.Index(v, "#"); j > 0 && !strings.HasPrefix(v, `"`) {
				v = strings.TrimSpace(v[:j])
			}
			v = strings.Trim(v, `"'`)
		}
		if inMods {
			kv[k] = v
		} else if k == "authors" || k == "displayURL" {
			if _, exists := kv[k]; !exists {
				kv[k] = v
			}
		}
	}
	return kv
}
