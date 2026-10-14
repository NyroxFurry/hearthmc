package minecraft

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hearth/internal/store"
)

// Dimension is a world dimension with region files.
type Dimension struct {
	ID        string `json:"id"`  // minecraft:overworld
	Key       string `json:"key"` // overworld, the_nether, the_end, <ns>_<name>
	Label     string `json:"label"`
	RegionDir string `json:"-"`
	Regions   int    `json:"regions"`
}

// WorldLayout describes where Minecraft stores world data. 26.1 moved
// dimensions to <world>/dimensions/<ns>/<name>/ and player data to <world>/players/.
type WorldLayout struct {
	Name          string      `json:"name"`
	Root          string      `json:"-"`
	Layout        string      `json:"layout"` // modern (26.1+), vanilla, bukkit
	Dimensions    []Dimension `json:"dimensions"`
	StatsDir      string      `json:"-"`
	AdvDir        string      `json:"-"`
	PlayerDataDir string      `json:"-"`
	Exists        bool        `json:"exists"`
}

var dimLabels = map[string]string{
	"minecraft:overworld":  "Oberwelt",
	"minecraft:the_nether": "Nether",
	"minecraft:the_end":    "Ende",
}

func dimKey(id string) string {
	ns, name, ok := strings.Cut(id, ":")
	if !ok {
		return id
	}
	if ns == "minecraft" {
		return name
	}
	return ns + "_" + strings.ReplaceAll(name, "/", "_")
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func countRegions(dir string) int {
	m, _ := filepath.Glob(filepath.Join(dir, "r.*.*.mca"))
	return len(m)
}

// DetectWorld inspects a server directory for the world called levelName.
func DetectWorld(serverDir, levelName string) *WorldLayout {
	if levelName == "" {
		levelName = "world"
	}
	root := filepath.Join(serverDir, filepath.FromSlash(levelName))
	w := &WorldLayout{Name: levelName, Root: root, Exists: isDir(root)}
	add := func(id, dir string) {
		if !isDir(dir) {
			return
		}
		label := dimLabels[id]
		if label == "" {
			label = id
		}
		w.Dimensions = append(w.Dimensions, Dimension{ID: id, Key: dimKey(id), Label: label, RegionDir: dir, Regions: countRegions(dir)})
	}
	if isDir(filepath.Join(root, "dimensions", "minecraft", "overworld")) {
		w.Layout = "modern"
		w.StatsDir = filepath.Join(root, "players", "stats")
		w.AdvDir = filepath.Join(root, "players", "advancements")
		w.PlayerDataDir = filepath.Join(root, "players", "data")
	} else {
		w.Layout = "vanilla"
		w.StatsDir = filepath.Join(root, "stats")
		w.AdvDir = filepath.Join(root, "advancements")
		w.PlayerDataDir = filepath.Join(root, "playerdata")
		add("minecraft:overworld", filepath.Join(root, "region"))
		if isDir(filepath.Join(root, "DIM-1", "region")) {
			add("minecraft:the_nether", filepath.Join(root, "DIM-1", "region"))
		} else if isDir(filepath.Join(serverDir, levelName+"_nether", "DIM-1", "region")) {
			w.Layout = "bukkit"
			add("minecraft:the_nether", filepath.Join(serverDir, levelName+"_nether", "DIM-1", "region"))
		}
		if isDir(filepath.Join(root, "DIM1", "region")) {
			add("minecraft:the_end", filepath.Join(root, "DIM1", "region"))
		} else if isDir(filepath.Join(serverDir, levelName+"_the_end", "DIM1", "region")) {
			w.Layout = "bukkit"
			add("minecraft:the_end", filepath.Join(serverDir, levelName+"_the_end", "DIM1", "region"))
		}
	}
	// namespaced dimensions (26.1+ for all, earlier for datapack dimensions)
	nsDirs, _ := os.ReadDir(filepath.Join(root, "dimensions"))
	for _, ns := range nsDirs {
		if !ns.IsDir() {
			continue
		}
		names, _ := os.ReadDir(filepath.Join(root, "dimensions", ns.Name()))
		for _, n := range names {
			id := ns.Name() + ":" + n.Name()
			if w.HasDimension(id) {
				continue
			}
			add(id, filepath.Join(root, "dimensions", ns.Name(), n.Name(), "region"))
		}
	}
	order := map[string]int{"minecraft:overworld": 0, "minecraft:the_nether": 1, "minecraft:the_end": 2}
	sort.SliceStable(w.Dimensions, func(i, j int) bool {
		oi, ok1 := order[w.Dimensions[i].ID]
		oj, ok2 := order[w.Dimensions[j].ID]
		if !ok1 {
			oi = 99
		}
		if !ok2 {
			oj = 99
		}
		if oi != oj {
			return oi < oj
		}
		return w.Dimensions[i].ID < w.Dimensions[j].ID
	})
	return w
}

func (w *WorldLayout) HasDimension(id string) bool {
	for _, d := range w.Dimensions {
		if d.ID == id {
			return true
		}
	}
	return false
}

func (w *WorldLayout) Dimension(key string) *Dimension {
	for i := range w.Dimensions {
		if w.Dimensions[i].Key == key || w.Dimensions[i].ID == key {
			return &w.Dimensions[i]
		}
	}
	return nil
}

// ---- usercache / UUIDs ----

type usercacheEntry struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

// ReadUsercache returns uuid -> name from usercache.json.
func ReadUsercache(serverDir string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(filepath.Join(serverDir, "usercache.json"))
	if err != nil {
		return out
	}
	var list []usercacheEntry
	if json.Unmarshal(b, &list) != nil {
		return out
	}
	for _, e := range list {
		out[strings.ToLower(e.UUID)] = e.Name
	}
	return out
}

// OfflineUUID computes the UUID Minecraft assigns in offline mode
// (UUID v3 of "OfflinePlayer:<name>").
func OfflineUUID(name string) string {
	h := md5.Sum([]byte("OfflinePlayer:" + name))
	h[6] = h[6]&0x0f | 0x30
	h[8] = h[8]&0x3f | 0x80
	s := hex.EncodeToString(h[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// NormalizeUUID returns the dashed lowercase form of a UUID (accepts undashed).
func NormalizeUUID(u string) string {
	u = strings.ToLower(strings.ReplaceAll(u, "-", ""))
	if len(u) != 32 {
		return ""
	}
	if _, err := hex.DecodeString(u); err != nil {
		return ""
	}
	return u[0:8] + "-" + u[8:12] + "-" + u[12:16] + "-" + u[16:20] + "-" + u[20:32]
}

// ---- stats ----

type statsFile struct {
	Stats map[string]map[string]int64 `json:"stats"`
}

func sumMap(m map[string]int64) int64 {
	var t int64
	for _, v := range m {
		t += v
	}
	return t
}

// ParseStats converts a stats/<uuid>.json file into leaderboard values.
func ParseStats(uuid string, data []byte) (*store.MCStatsUpdate, error) {
	var sf statsFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	c := sf.Stats["minecraft:custom"]
	u := &store.MCStatsUpdate{UUID: uuid, Stats: string(data)}
	u.PlayTicks = c["minecraft:play_time"]
	if u.PlayTicks == 0 {
		u.PlayTicks = c["minecraft:play_one_minute"] // < 1.17 (ticks despite the name)
	}
	u.Deaths = c["minecraft:deaths"]
	u.MobKills = c["minecraft:mob_kills"]
	u.PlayerKills = c["minecraft:player_kills"]
	u.Jumps = c["minecraft:jump"]
	u.DamageDealt = c["minecraft:damage_dealt"]
	u.DamageTaken = c["minecraft:damage_taken"]
	u.FishCaught = c["minecraft:fish_caught"]
	for k, v := range c {
		if strings.HasSuffix(k, "_one_cm") && k != "minecraft:fall_one_cm" {
			u.DistanceCM += v
		}
	}
	u.BlocksMined = sumMap(sf.Stats["minecraft:mined"])
	u.ItemsCrafted = sumMap(sf.Stats["minecraft:crafted"])
	return u, nil
}

// CountAdvancements counts completed advancements (recipes excluded).
func CountAdvancements(data []byte) int64 {
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return 0
	}
	var n int64
	for k, v := range m {
		if k == "DataVersion" || strings.Contains(k, ":recipes/") {
			continue
		}
		var a struct {
			Done bool `json:"done"`
		}
		if json.Unmarshal(v, &a) == nil && a.Done {
			n++
		}
	}
	return n
}

type statFileInfo struct {
	UUID  string
	Path  string
	MTime time.Time
}

func listUUIDFiles(dir string) []statFileInfo {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []statFileInfo
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		u := NormalizeUUID(strings.TrimSuffix(name, ".json"))
		if u == "" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, statFileInfo{UUID: u, Path: filepath.Join(dir, name), MTime: info.ModTime()})
	}
	return out
}
