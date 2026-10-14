package minecraft

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"hearth/internal/httpx"
)

type Flavor struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"` // vanilla, plugins, mods, custom
}

var Flavors = []Flavor{
	{"paper", "Paper", "Schnell und stabil, unterstützt Bukkit-, Spigot- und Paper-Plugins.", "plugins"},
	{"purpur", "Purpur", "Paper-Fork mit vielen zusätzlichen Gameplay-Optionen.", "plugins"},
	{"vanilla", "Vanilla", "Der offizielle Server von Mojang, ohne Plugins oder Mods.", "vanilla"},
	{"fabric", "Fabric", "Leichtgewichtiger Mod-Loader, ideal für Performance-Mods.", "mods"},
	{"neoforge", "NeoForge", "Moderner Mod-Loader für große Modpacks.", "mods"},
	{"forge", "Forge", "Klassischer Mod-Loader, viele ältere Modpacks.", "mods"},
	{"custom", "Eigene JAR", "Beliebige Server-JAR (z. B. Spigot, Mohist) selbst hochladen.", "custom"},
}

func FlavorByID(id string) (Flavor, bool) {
	for _, f := range Flavors {
		if f.ID == id {
			return f, true
		}
	}
	return Flavor{}, false
}

// ModDir returns where plugins/mods of a flavor live ("" if not supported).
func ModDir(flavor string) string {
	switch flavor {
	case "paper", "purpur":
		return "plugins"
	case "fabric", "forge", "neoforge":
		return "mods"
	case "custom":
		return "plugins"
	}
	return ""
}

// ModrinthLoaders returns the Modrinth loader tags compatible with a flavor.
func ModrinthLoaders(flavor string) []string {
	switch flavor {
	case "paper":
		return []string{"paper", "spigot", "bukkit"}
	case "purpur":
		return []string{"purpur", "paper", "spigot", "bukkit"}
	case "fabric":
		return []string{"fabric"}
	case "forge":
		return []string{"forge"}
	case "neoforge":
		return []string{"neoforge"}
	case "custom":
		return []string{"paper", "spigot", "bukkit"}
	}
	return nil
}

type VersionInfo struct {
	ID     string `json:"id"`
	Type   string `json:"type"` // release, snapshot
	Java   int    `json:"java,omitempty"`
	Stable bool   `json:"stable"`
}

type BuildInfo struct {
	ID      string `json:"id"`
	Stable  bool   `json:"stable"`
	Time    string `json:"time,omitempty"`
	Channel string `json:"channel,omitempty"`
}

// Resolved is a concrete download for a server installation.
type Resolved struct {
	URL       string
	Checksum  *httpx.Checksum
	Build     string
	JavaMajor int
	Installer bool // Forge/NeoForge: the download is an installer that must be run
}

type Versions struct {
	HTTP *httpx.Client
}

const (
	mojangManifest = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	paperAPI       = "https://fill.papermc.io/v3/projects/paper"
	purpurAPI      = "https://api.purpurmc.org/v2/purpur"
	fabricMeta     = "https://meta.fabricmc.net/v2"
	forgePromos    = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	forgeMaven     = "https://maven.minecraftforge.net/net/minecraftforge/forge"
	neoforgeMaven  = "https://maven.neoforged.net/releases/net/neoforged/neoforge"
)

const listTTL = 20 * time.Minute

type mojangManifestJSON struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"versions"`
}

type mojangVersionJSON struct {
	Downloads struct {
		Server *struct {
			SHA1 string `json:"sha1"`
			Size int64  `json:"size"`
			URL  string `json:"url"`
		} `json:"server"`
	} `json:"downloads"`
	JavaVersion struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
}

func (v *Versions) mojang(ctx context.Context) (*mojangManifestJSON, error) {
	var m mojangManifestJSON
	if err := v.HTTP.GetJSON(ctx, mojangManifest, listTTL, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (v *Versions) mojangVersion(ctx context.Context, id string) (*mojangVersionJSON, error) {
	m, err := v.mojang(ctx)
	if err != nil {
		return nil, err
	}
	for _, ver := range m.Versions {
		if ver.ID == id {
			var vj mojangVersionJSON
			if err := v.HTTP.GetJSON(ctx, ver.URL, 24*time.Hour, &vj); err != nil {
				return nil, err
			}
			return &vj, nil
		}
	}
	return nil, fmt.Errorf("Minecraft-Version %s nicht gefunden", id)
}

// JavaFor returns the Java major version needed for a Minecraft version.
func (v *Versions) JavaFor(ctx context.Context, mcVersion string) int {
	if vj, err := v.mojangVersion(ctx, mcVersion); err == nil && vj.JavaVersion.MajorVersion > 0 {
		return normalizeJava(vj.JavaVersion.MajorVersion)
	}
	return GuessJava(mcVersion)
}

// normalizeJava maps versions without long-term Adoptium/Docker builds to the next LTS.
func normalizeJava(major int) int {
	switch {
	case major <= 8:
		return 8
	case major <= 11:
		return 11
	case major <= 17:
		return 17
	case major <= 21:
		return 21
	case major <= 25:
		return 25
	}
	return major
}

// GuessJava estimates the required Java version from the version number.
func GuessJava(mc string) int {
	parts := versionNums(mc)
	if len(parts) == 0 {
		return 21
	}
	if parts[0] >= 26 { // year based versions (26.1+)
		return 25
	}
	if parts[0] != 1 || len(parts) < 2 {
		return 21
	}
	minor := parts[1]
	patch := 0
	if len(parts) > 2 {
		patch = parts[2]
	}
	switch {
	case minor > 20 || (minor == 20 && patch >= 5):
		return 21
	case minor >= 18:
		return 17
	case minor == 17:
		return 17
	default:
		return 8
	}
}

func versionNums(s string) []int {
	s = strings.SplitN(s, "-", 2)[0]
	var out []int
	for _, p := range strings.Split(s, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// CompareVersions compares dotted versions; pre-release suffixes sort lower.
func CompareVersions(a, b string) int {
	na, nb := versionNums(a), versionNums(b)
	for i := 0; i < max(len(na), len(nb)); i++ {
		var x, y int
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	pa, pb := strings.Contains(a, "-"), strings.Contains(b, "-")
	switch {
	case pa && !pb:
		return -1
	case !pa && pb:
		return 1
	}
	return strings.Compare(a, b)
}

func sortVersionsDesc(list []VersionInfo) {
	sort.SliceStable(list, func(i, j int) bool { return CompareVersions(list[i].ID, list[j].ID) > 0 })
}

// List returns the available Minecraft versions for a flavor (newest first).
func (v *Versions) List(ctx context.Context, flavor string, snapshots bool) ([]VersionInfo, error) {
	switch flavor {
	case "vanilla", "custom":
		m, err := v.mojang(ctx)
		if err != nil {
			return nil, err
		}
		var out []VersionInfo
		for _, ver := range m.Versions {
			if ver.Type == "release" || (snapshots && ver.Type == "snapshot") {
				out = append(out, VersionInfo{ID: ver.ID, Type: ver.Type, Stable: ver.Type == "release"})
			}
		}
		return out, nil // manifest is already newest first
	case "paper":
		var resp struct {
			Versions []struct {
				Version struct {
					ID      string `json:"id"`
					Support struct {
						Status string `json:"status"`
					} `json:"support"`
					Java struct {
						Version struct {
							Minimum int `json:"minimum"`
						} `json:"version"`
					} `json:"java"`
				} `json:"version"`
				Builds []int `json:"builds"`
			} `json:"versions"`
		}
		if err := v.HTTP.GetJSON(ctx, paperAPI+"/versions", listTTL, &resp); err != nil {
			return nil, err
		}
		var out []VersionInfo
		for _, ver := range resp.Versions {
			if len(ver.Builds) == 0 {
				continue
			}
			pre := strings.Contains(ver.Version.ID, "-")
			if pre && !snapshots {
				continue
			}
			typ := "release"
			if pre {
				typ = "snapshot"
			}
			out = append(out, VersionInfo{ID: ver.Version.ID, Type: typ, Java: normalizeJava(ver.Version.Java.Version.Minimum),
				Stable: !pre && ver.Version.Support.Status != "UNSUPPORTED"})
		}
		sortVersionsDesc(out)
		return out, nil
	case "purpur":
		var resp struct {
			Versions []string `json:"versions"`
		}
		if err := v.HTTP.GetJSON(ctx, purpurAPI, listTTL, &resp); err != nil {
			return nil, err
		}
		out := make([]VersionInfo, 0, len(resp.Versions))
		for _, id := range resp.Versions {
			out = append(out, VersionInfo{ID: id, Type: "release", Stable: true})
		}
		sortVersionsDesc(out)
		return out, nil
	case "fabric":
		var resp []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := v.HTTP.GetJSON(ctx, fabricMeta+"/versions/game", listTTL, &resp); err != nil {
			return nil, err
		}
		var out []VersionInfo
		for _, g := range resp {
			if !g.Stable && !snapshots {
				continue
			}
			typ := "release"
			if !g.Stable {
				typ = "snapshot"
			}
			out = append(out, VersionInfo{ID: g.Version, Type: typ, Stable: g.Stable})
		}
		return out, nil
	case "forge":
		promos, err := v.forgePromos(ctx)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		var out []VersionInfo
		for k := range promos {
			mc := strings.TrimSuffix(strings.TrimSuffix(k, "-latest"), "-recommended")
			if !seen[mc] {
				seen[mc] = true
				out = append(out, VersionInfo{ID: mc, Type: "release", Stable: true})
			}
		}
		sortVersionsDesc(out)
		return out, nil
	case "neoforge":
		groups, err := v.neoforgeVersions(ctx)
		if err != nil {
			return nil, err
		}
		var out []VersionInfo
		for mc, builds := range groups {
			stable := false
			for _, b := range builds {
				if b.Stable {
					stable = true
				}
			}
			if !stable && !snapshots {
				continue
			}
			out = append(out, VersionInfo{ID: mc, Type: "release", Stable: stable})
		}
		sortVersionsDesc(out)
		return out, nil
	}
	return nil, fmt.Errorf("unbekannte Server-Art %q", flavor)
}

// Builds lists builds (Paper/Purpur) or loader versions (Fabric/Forge/NeoForge) for a Minecraft version.
func (v *Versions) Builds(ctx context.Context, flavor, mc string) ([]BuildInfo, error) {
	switch flavor {
	case "paper":
		var builds []struct {
			ID      int    `json:"id"`
			Time    string `json:"time"`
			Channel string `json:"channel"`
		}
		if err := v.HTTP.GetJSON(ctx, paperAPI+"/versions/"+mc+"/builds", 5*time.Minute, &builds); err != nil {
			return nil, err
		}
		out := make([]BuildInfo, 0, len(builds))
		for _, b := range builds {
			out = append(out, BuildInfo{ID: strconv.Itoa(b.ID), Time: b.Time, Channel: b.Channel,
				Stable: b.Channel == "STABLE" || b.Channel == "RECOMMENDED"})
		}
		sort.Slice(out, func(i, j int) bool { a, _ := strconv.Atoi(out[i].ID); b, _ := strconv.Atoi(out[j].ID); return a > b })
		return out, nil
	case "purpur":
		var resp struct {
			Builds struct {
				All    []string `json:"all"`
				Latest string   `json:"latest"`
			} `json:"builds"`
		}
		if err := v.HTTP.GetJSON(ctx, purpurAPI+"/"+mc, 5*time.Minute, &resp); err != nil {
			return nil, err
		}
		out := make([]BuildInfo, 0, len(resp.Builds.All))
		for i := len(resp.Builds.All) - 1; i >= 0; i-- {
			out = append(out, BuildInfo{ID: resp.Builds.All[i], Stable: true})
		}
		return out, nil
	case "fabric":
		var resp []struct {
			Loader struct {
				Version string `json:"version"`
				Stable  bool   `json:"stable"`
			} `json:"loader"`
		}
		if err := v.HTTP.GetJSON(ctx, fabricMeta+"/versions/loader/"+mc, listTTL, &resp); err != nil {
			return nil, err
		}
		out := make([]BuildInfo, 0, len(resp))
		for _, l := range resp {
			out = append(out, BuildInfo{ID: l.Loader.Version, Stable: l.Loader.Stable})
		}
		return out, nil
	case "forge":
		meta, err := v.mavenVersions(ctx, forgeMaven+"/maven-metadata.xml")
		if err != nil {
			return nil, err
		}
		promos, _ := v.forgePromos(ctx)
		rec := promos[mc+"-recommended"]
		var out []BuildInfo
		for _, full := range meta {
			if fv, ok := strings.CutPrefix(full, mc+"-"); ok {
				out = append(out, BuildInfo{ID: fv, Stable: fv == rec})
			}
		}
		sort.Slice(out, func(i, j int) bool { return CompareVersions(out[i].ID, out[j].ID) > 0 })
		return out, nil
	case "neoforge":
		groups, err := v.neoforgeVersions(ctx)
		if err != nil {
			return nil, err
		}
		return groups[mc], nil
	}
	return []BuildInfo{}, nil
}

func (v *Versions) forgePromos(ctx context.Context) (map[string]string, error) {
	var resp struct {
		Promos map[string]string `json:"promos"`
	}
	if err := v.HTTP.GetJSON(ctx, forgePromos, listTTL, &resp); err != nil {
		return nil, err
	}
	return resp.Promos, nil
}

func (v *Versions) mavenVersions(ctx context.Context, url string) ([]string, error) {
	b, err := v.HTTP.Get(ctx, url, listTTL)
	if err != nil {
		return nil, err
	}
	var meta struct {
		Versioning struct {
			Versions []string `xml:"versions>version"`
		} `xml:"versioning"`
	}
	if err := xml.Unmarshal(b, &meta); err != nil {
		return nil, err
	}
	return meta.Versioning.Versions, nil
}

// NeoForgeMC derives the Minecraft version from a NeoForge version
// (21.1.77 -> 1.21.1, 21.0.5 -> 1.21, 26.1.0.15-beta -> 26.1).
func NeoForgeMC(nf string) string {
	base := strings.SplitN(nf, "-", 2)[0]
	p := strings.Split(base, ".")
	if len(p) == 4 { // year based
		if p[2] == "0" {
			return p[0] + "." + p[1]
		}
		return p[0] + "." + p[1] + "." + p[2]
	}
	if len(p) < 2 {
		return ""
	}
	if p[1] == "0" {
		return "1." + p[0]
	}
	return "1." + p[0] + "." + p[1]
}

func (v *Versions) neoforgeVersions(ctx context.Context) (map[string][]BuildInfo, error) {
	list, err := v.mavenVersions(ctx, neoforgeMaven+"/maven-metadata.xml")
	if err != nil {
		return nil, err
	}
	groups := map[string][]BuildInfo{}
	for _, nf := range list {
		suffix := ""
		if i := strings.Index(nf, "-"); i >= 0 {
			suffix = nf[i+1:]
		}
		if suffix != "" && suffix != "beta" {
			continue // alphas, pre-releases
		}
		mc := NeoForgeMC(nf)
		if mc == "" {
			continue
		}
		groups[mc] = append(groups[mc], BuildInfo{ID: nf, Stable: suffix == ""})
	}
	for mc := range groups {
		b := groups[mc]
		sort.Slice(b, func(i, j int) bool { return CompareVersions(b[i].ID, b[j].ID) > 0 })
	}
	return groups, nil
}

func pickBuild(builds []BuildInfo, want string) (string, error) {
	if len(builds) == 0 {
		return "", errors.New("keine Builds für diese Version verfügbar")
	}
	if want != "" && want != "latest" {
		for _, b := range builds {
			if b.ID == want {
				return b.ID, nil
			}
		}
		return "", fmt.Errorf("Build %s nicht gefunden", want)
	}
	for _, b := range builds {
		if b.Stable {
			return b.ID, nil
		}
	}
	return builds[0].ID, nil
}

// Resolve determines the download for a flavor/version/build ("" or "latest" = newest stable).
func (v *Versions) Resolve(ctx context.Context, flavor, mc, build string) (*Resolved, error) {
	switch flavor {
	case "vanilla":
		vj, err := v.mojangVersion(ctx, mc)
		if err != nil {
			return nil, err
		}
		if vj.Downloads.Server == nil {
			return nil, fmt.Errorf("für %s gibt es keinen Server-Download", mc)
		}
		return &Resolved{URL: vj.Downloads.Server.URL, Checksum: &httpx.Checksum{Algo: "sha1", Hex: vj.Downloads.Server.SHA1},
			Build: mc, JavaMajor: normalizeJava(vj.JavaVersion.MajorVersion)}, nil
	case "paper":
		builds, err := v.Builds(ctx, "paper", mc)
		if err != nil {
			return nil, err
		}
		b, err := pickBuild(builds, build)
		if err != nil {
			return nil, err
		}
		var resp struct {
			Downloads map[string]struct {
				Name      string `json:"name"`
				Checksums struct {
					SHA256 string `json:"sha256"`
				} `json:"checksums"`
				URL string `json:"url"`
			} `json:"downloads"`
		}
		if err := v.HTTP.GetJSON(ctx, paperAPI+"/versions/"+mc+"/builds/"+b, time.Hour, &resp); err != nil {
			return nil, err
		}
		dl, ok := resp.Downloads["server:default"]
		if !ok {
			return nil, errors.New("Paper-Build hat keinen Server-Download")
		}
		java := v.JavaFor(ctx, mc)
		if list, err := v.List(ctx, "paper", true); err == nil {
			for _, ver := range list {
				if ver.ID == mc && ver.Java > 0 {
					java = max(java, ver.Java)
				}
			}
		}
		return &Resolved{URL: dl.URL, Checksum: &httpx.Checksum{Algo: "sha256", Hex: dl.Checksums.SHA256}, Build: b, JavaMajor: java}, nil
	case "purpur":
		builds, err := v.Builds(ctx, "purpur", mc)
		if err != nil {
			return nil, err
		}
		b, err := pickBuild(builds, build)
		if err != nil {
			return nil, err
		}
		return &Resolved{URL: purpurAPI + "/" + mc + "/" + b + "/download", Build: b, JavaMajor: v.JavaFor(ctx, mc)}, nil
	case "fabric":
		loaders, err := v.Builds(ctx, "fabric", mc)
		if err != nil {
			return nil, err
		}
		loader, err := pickBuild(loaders, build)
		if err != nil {
			return nil, err
		}
		var inst []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := v.HTTP.GetJSON(ctx, fabricMeta+"/versions/installer", listTTL, &inst); err != nil {
			return nil, err
		}
		installer := ""
		for _, i := range inst {
			if i.Stable {
				installer = i.Version
				break
			}
		}
		if installer == "" && len(inst) > 0 {
			installer = inst[0].Version
		}
		if installer == "" {
			return nil, errors.New("kein Fabric-Installer gefunden")
		}
		return &Resolved{URL: fmt.Sprintf("%s/versions/loader/%s/%s/%s/server/jar", fabricMeta, mc, loader, installer),
			Build: loader, JavaMajor: v.JavaFor(ctx, mc)}, nil
	case "forge":
		fv := build
		if fv == "" || fv == "latest" {
			promos, err := v.forgePromos(ctx)
			if err != nil {
				return nil, err
			}
			fv = promos[mc+"-recommended"]
			if fv == "" {
				fv = promos[mc+"-latest"]
			}
			if fv == "" {
				return nil, fmt.Errorf("keine Forge-Version für %s gefunden", mc)
			}
		}
		full := mc + "-" + fv
		return &Resolved{URL: fmt.Sprintf("%s/%s/forge-%s-installer.jar", forgeMaven, full, full), Build: fv,
			JavaMajor: v.JavaFor(ctx, mc), Installer: true}, nil
	case "neoforge":
		builds, err := v.Builds(ctx, "neoforge", mc)
		if err != nil {
			return nil, err
		}
		b, err := pickBuild(builds, build)
		if err != nil {
			return nil, err
		}
		return &Resolved{URL: fmt.Sprintf("%s/%s/neoforge-%s-installer.jar", neoforgeMaven, b, b), Build: b,
			JavaMajor: v.JavaFor(ctx, mc), Installer: true}, nil
	}
	return nil, fmt.Errorf("Server-Art %q kann nicht automatisch installiert werden", flavor)
}
