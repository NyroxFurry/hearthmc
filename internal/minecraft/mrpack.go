package minecraft

// Modpack export in the Modrinth format (.mrpack): a zip with
// modrinth.index.json, which lists every mod with its download link on
// Modrinth, plus folders with files that are copied into the instance.
// Spec: https://support.modrinth.com/en/articles/8802351-modrinth-modpack-format-mrpack

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// PackMod is a mod of the server as it ends up in the modpack.
type PackMod struct {
	File      string `json:"file"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	IconURL   string `json:"iconUrl,omitempty"`
	ProjectID string `json:"projectId,omitempty"`
	Slug      string `json:"slug,omitempty"`
	Size      int64  `json:"size"`
	// Source: "modrinth" (the launcher downloads it from Modrinth) or "file"
	// (not on Modrinth, the jar itself goes into the pack).
	Source string `json:"source"`
	// Client is whether players need the mod: required, optional or unsupported
	// (server-only mods are listed but not installed on clients).
	Client string `json:"client"`

	path, sha1, sha512, url, env string
}

// ClientMod is a mod that only players install (it is not on the server).
type ClientMod struct {
	ProjectID   string `json:"projectId"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	IconURL     string `json:"iconUrl,omitempty"`
}

// PackRequest describes a modpack to build from a server.
type PackRequest struct {
	ModsDir       string
	Name          string
	Summary       string
	VersionID     string
	Flavor        string // fabric, forge, neoforge
	MCVersion     string
	LoaderVersion string
	// ClientMods are Modrinth project IDs (or slugs) added for players only.
	ClientMods []string
	// BundleUnknown packs mods that are not on Modrinth into the file;
	// otherwise they are left out (and listed in the report).
	BundleUnknown bool
	// ServerAddress adds the server to the multiplayer list (servers.dat).
	ServerName    string
	ServerAddress string
}

// PackReport summarizes a built modpack.
type PackReport struct {
	Mods       int      `json:"mods"`       // server mods in the pack
	Linked     int      `json:"linked"`     // downloaded from Modrinth by the launcher
	Bundled    int      `json:"bundled"`    // packed into the file
	ServerOnly int      `json:"serverOnly"` // in the pack, but not installed on clients
	ClientMods int      `json:"clientMods"` // client-only mods incl. their dependencies
	Skipped    []string `json:"skipped"`    // not on Modrinth and not bundled
	Warnings   []string `json:"warnings"`
}

type packEnv struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

type packFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       *packEnv          `json:"env,omitempty"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

type packIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary,omitempty"`
	Files         []packFile        `json:"files"`
	Dependencies  map[string]string `json:"dependencies"`
}

// PackLoaderKey is the dependency key of a flavor in modrinth.index.json.
func PackLoaderKey(flavor string) string {
	switch flavor {
	case "fabric":
		return "fabric-loader"
	case "forge":
		return "forge"
	case "neoforge":
		return "neoforge"
	}
	return ""
}

type fileSums struct {
	size     int64
	mtime    time.Time
	s1, s512 string
}

// sumCache keeps the hashes of mod jars, so opening the modpack page does not
// read every jar again. Entries are valid while size and mtime match.
var sumCache = struct {
	sync.Mutex
	m map[string]fileSums
}{m: map[string]fileSums{}}

func hashFile(path string) (sum1, sum512 string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", "", 0, err
	}
	sumCache.Lock()
	c, ok := sumCache.m[path]
	sumCache.Unlock()
	if ok && c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		return c.s1, c.s512, c.size, nil
	}
	h1, h512 := sha1.New(), sha512.New()
	size, err = io.Copy(io.MultiWriter(h1, h512), f)
	if err != nil {
		return "", "", 0, err
	}
	sum1, sum512 = hex.EncodeToString(h1.Sum(nil)), hex.EncodeToString(h512.Sum(nil))
	sumCache.Lock()
	if len(sumCache.m) > 5000 {
		sumCache.m = map[string]fileSums{}
	}
	sumCache.m[path] = fileSums{size: size, mtime: st.ModTime(), s1: sum1, s512: sum512}
	sumCache.Unlock()
	return sum1, sum512, size, nil
}

func side(v string) string {
	switch v {
	case "required", "optional", "unsupported":
		return v
	}
	return "required" // unknown: better installed than missing
}

// AnalyzeMods lists the enabled mods of a server and looks them up on
// Modrinth by hash. Without Modrinth (offline) every mod counts as "file".
func (g *Game) AnalyzeMods(ctx context.Context, modsDir string) ([]PackMod, error) {
	out := []PackMod{}
	byHash := map[string]int{}
	var hashes []string
	for _, p := range ListPlugins(modsDir) {
		if !p.Enabled {
			continue
		}
		path := filepath.Join(modsDir, p.File)
		s1, s512, size, err := hashFile(path)
		if err != nil {
			continue
		}
		m := PackMod{File: p.File, Name: p.Name, Version: p.Version, Size: size, Source: "file", Client: "required",
			path: path, sha1: s1, sha512: s512, env: p.Env}
		if p.Env == "server" {
			m.Client = "unsupported"
		}
		byHash[s1] = len(out)
		hashes = append(hashes, s1)
		out = append(out, m)
	}
	if len(hashes) == 0 {
		return out, nil
	}
	versions, err := g.Modrinth.VersionsByHash(ctx, hashes)
	if err != nil {
		return out, fmt.Errorf("Modrinth nicht erreichbar: %w", err)
	}
	var ids []string
	for hash, v := range versions {
		i, ok := byHash[hash]
		if !ok {
			continue
		}
		for _, f := range v.Files {
			if strings.EqualFold(f.Hashes["sha1"], hash) && strings.HasPrefix(f.URL, "https://") {
				out[i].Source, out[i].url, out[i].ProjectID = "modrinth", f.URL, v.ProjectID
				ids = append(ids, v.ProjectID)
				if out[i].Version == "" {
					out[i].Version = v.VersionNumber
				}
				break
			}
		}
	}
	if len(ids) > 0 {
		projects, err := g.Modrinth.Projects(ctx, ids)
		if err != nil {
			return out, fmt.Errorf("Modrinth nicht erreichbar: %w", err)
		}
		pm := map[string]MRProject{}
		for _, p := range projects {
			pm[p.ID] = p
		}
		for i := range out {
			p, ok := pm[out[i].ProjectID]
			if !ok {
				continue
			}
			out[i].Slug, out[i].IconURL, out[i].Client = p.Slug, p.IconURL, side(p.ClientSide)
			if out[i].Name == "" || out[i].Name == strings.TrimSuffix(out[i].File, ".jar") {
				out[i].Name = p.Title
			}
		}
	}
	return out, nil
}

// SearchClientMods finds mods for players: needed on the client, not needed
// on the server (shaders, minimaps, performance and comfort mods).
func (m *Modrinth) SearchClientMods(ctx context.Context, query string, loaders []string, mcVersion string, offset, limit int) ([]MRHit, int, error) {
	var cats []string
	for _, l := range loaders {
		cats = append(cats, "categories:"+l)
	}
	facets := [][]string{{"project_type:mod"}, {"client_side:required"}, {"server_side:optional", "server_side:unsupported"}}
	if len(cats) > 0 {
		facets = append(facets, cats)
	}
	if mcVersion != "" {
		facets = append(facets, []string{"versions:" + mcVersion})
	}
	index := "downloads"
	if strings.TrimSpace(query) != "" {
		index = "relevance"
	}
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	q := url.Values{
		"query":  {query},
		"facets": {jsonParam(facets)},
		"index":  {index},
		"offset": {strconv.Itoa(max(0, offset))},
		"limit":  {strconv.Itoa(limit)},
	}
	var resp struct {
		Hits      []MRHit `json:"hits"`
		TotalHits int     `json:"total_hits"`
	}
	if err := m.HTTP.GetJSON(ctx, modrinthAPI+"/search?"+q.Encode(), 10*time.Minute, &resp); err != nil {
		return nil, 0, err
	}
	if resp.Hits == nil {
		resp.Hits = []MRHit{}
	}
	return resp.Hits, resp.TotalHits, nil
}

// BuildPack writes a .mrpack for the server's mods (plus the chosen client
// mods) to w.
func (g *Game) BuildPack(ctx context.Context, req PackRequest, w io.Writer) (*PackReport, error) {
	key := PackLoaderKey(req.Flavor)
	if key == "" {
		return nil, errors.New("Modpacks gibt es nur für Fabric-, Forge- und NeoForge-Server")
	}
	if req.MCVersion == "" || req.LoaderVersion == "" {
		return nil, errors.New("Minecraft- oder Loader-Version unbekannt. Installiere den Server unter „Start & Version“ einmal neu.")
	}
	rep := &PackReport{Skipped: []string{}, Warnings: []string{}}
	mods, err := g.AnalyzeMods(ctx, req.ModsDir)
	if err != nil {
		// without Modrinth every mod counts as unknown (and is bundled)
		rep.Warnings = append(rep.Warnings, err.Error())
	}
	idx := packIndex{
		FormatVersion: 1, Game: "minecraft", VersionID: req.VersionID, Name: req.Name, Summary: req.Summary,
		Files:        []packFile{},
		Dependencies: map[string]string{"minecraft": req.MCVersion, key: req.LoaderVersion},
	}
	type bundled struct{ zipPath, src string }
	var files []bundled
	have := map[string]bool{} // Modrinth projects already in the pack
	usedPaths := map[string]bool{}
	for _, m := range mods {
		rep.Mods++
		if m.Client == "unsupported" {
			rep.ServerOnly++
		}
		if m.Source == "modrinth" {
			have[m.ProjectID] = true
			usedPaths["mods/"+m.File] = true
			idx.Files = append(idx.Files, packFile{
				Path: "mods/" + m.File, Hashes: map[string]string{"sha1": m.sha1, "sha512": m.sha512},
				Env: &packEnv{Client: m.Client, Server: "required"}, Downloads: []string{m.url}, FileSize: m.Size,
			})
			rep.Linked++
			continue
		}
		if !req.BundleUnknown {
			rep.Skipped = append(rep.Skipped, m.File)
			rep.Mods--
			continue
		}
		// mods without Modrinth entry: overrides apply to clients and servers,
		// server-/client-overrides only to one side
		dir := "overrides"
		switch m.env {
		case "server":
			dir = "server-overrides"
		case "client":
			dir = "client-overrides"
		}
		files = append(files, bundled{dir + "/mods/" + m.File, m.path})
		usedPaths["mods/"+m.File] = true
		rep.Bundled++
	}

	// client mods and their required dependencies
	loaders := ModrinthLoaders(req.Flavor)
	var addClient func(project string, depth int) error
	addClient = func(project string, depth int) error {
		if depth > 4 || project == "" || have[project] {
			return nil
		}
		v, _, err := g.Modrinth.Resolve(ctx, project, "", loaders, req.MCVersion)
		if err != nil {
			return err
		}
		if have[v.ProjectID] {
			return nil
		}
		have[project], have[v.ProjectID] = true, true
		f := BestFile(v, loaders)
		if f == nil || f.Hashes["sha1"] == "" || f.Hashes["sha512"] == "" || !strings.HasPrefix(f.URL, "https://") {
			return fmt.Errorf("%s: keine passende Datei", project)
		}
		name := filepath.Base(f.Filename)
		if !ValidJarName(name) {
			return fmt.Errorf("%s: unerwarteter Dateiname %s", project, name)
		}
		if usedPaths["mods/"+name] {
			return nil
		}
		usedPaths["mods/"+name] = true
		idx.Files = append(idx.Files, packFile{
			Path: "mods/" + name, Hashes: map[string]string{"sha1": f.Hashes["sha1"], "sha512": f.Hashes["sha512"]},
			Env: &packEnv{Client: "required", Server: "unsupported"}, Downloads: []string{f.URL}, FileSize: f.Size,
		})
		rep.ClientMods++
		for _, d := range v.Dependencies {
			if d.DependencyType != "required" || d.ProjectID == "" {
				continue
			}
			if err := addClient(d.ProjectID, depth+1); err != nil {
				rep.Warnings = append(rep.Warnings, "Abhängigkeit von "+name+": "+err.Error())
			}
		}
		return nil
	}
	for _, p := range req.ClientMods {
		if err := addClient(strings.TrimSpace(p), 0); err != nil {
			rep.Warnings = append(rep.Warnings, "Client-Mod "+err.Error())
		}
	}

	sort.Slice(idx.Files, func(i, j int) bool { return idx.Files[i].Path < idx.Files[j].Path })
	zw := zip.NewWriter(w)
	index, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeZipEntry(zw, "modrinth.index.json", bytes.NewReader(index)); err != nil {
		return nil, err
	}
	for _, f := range files {
		src, err := os.Open(f.src)
		if err != nil {
			return nil, err
		}
		err = writeZipEntry(zw, f.zipPath, src)
		src.Close()
		if err != nil {
			return nil, err
		}
	}
	if req.ServerAddress != "" {
		name := req.ServerName
		if name == "" {
			name = req.Name
		}
		if err := writeZipEntry(zw, "client-overrides/servers.dat", bytes.NewReader(serversDat(name, req.ServerAddress))); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return rep, nil
}

func writeZipEntry(zw *zip.Writer, name string, r io.Reader) error {
	method := zip.Deflate
	if strings.HasSuffix(name, ".jar") {
		method = zip.Store // already compressed
	}
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

// serversDat encodes the multiplayer server list of the client (uncompressed
// NBT) with one entry.
func serversDat(name, address string) []byte {
	var b bytes.Buffer
	str := func(s string) {
		if len(s) > 32767 || !utf8.ValidString(s) {
			s = ""
		}
		binary.Write(&b, binary.BigEndian, uint16(len(s)))
		b.WriteString(s)
	}
	tag := func(t byte, name string) {
		b.WriteByte(t)
		str(name)
	}
	tag(10, "") // root compound
	tag(9, "servers")
	b.WriteByte(10)                              // list of compounds
	binary.Write(&b, binary.BigEndian, int32(1)) // one entry
	tag(8, "name")
	str(name)
	tag(8, "ip")
	str(address)
	b.WriteByte(0) // end of entry
	b.WriteByte(0) // end of root
	return b.Bytes()
}

// LoaderVersion finds the installed mod loader version from the libraries of
// a server, for servers installed before the panel recorded it.
func LoaderVersion(serverDir, flavor, mcVersion string) string {
	var dir string
	switch flavor {
	case "fabric":
		dir = "libraries/net/fabricmc/fabric-loader"
	case "forge":
		dir = "libraries/net/minecraftforge/forge"
	case "neoforge":
		dir = "libraries/net/neoforged/neoforge"
	default:
		return ""
	}
	ents, err := os.ReadDir(filepath.Join(serverDir, filepath.FromSlash(dir)))
	if err != nil {
		return ""
	}
	best := ""
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		v := e.Name()
		if flavor == "forge" {
			// Forge folders are named <minecraft>-<forge>
			p, ok := strings.CutPrefix(v, mcVersion+"-")
			if !ok {
				continue
			}
			v = p
		}
		if best == "" || compareVersions(v, best) > 0 {
			best = v
		}
	}
	return best
}

// compareVersions compares dotted version strings numerically where possible.
func compareVersions(a, b string) int {
	pa, pb := strings.FieldsFunc(a, isVersionSep), strings.FieldsFunc(b, isVersionSep)
	for i := 0; i < max(len(pa), len(pb)); i++ {
		if i >= len(pa) {
			return -1
		}
		if i >= len(pb) {
			return 1
		}
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		switch {
		case ea == nil && eb == nil && na != nb:
			if na < nb {
				return -1
			}
			return 1
		case (ea != nil || eb != nil) && pa[i] != pb[i]:
			return strings.Compare(pa[i], pb[i])
		}
	}
	return 0
}

func isVersionSep(r rune) bool { return r == '.' || r == '-' || r == '+' }
