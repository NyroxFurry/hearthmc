package minecraft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hearth/internal/httpx"
)

// modrinthAPI is a variable so tests can point it at a local server.
var modrinthAPI = "https://api.modrinth.com/v2"

type Modrinth struct {
	HTTP *httpx.Client
}

type MRHit struct {
	ProjectID    string   `json:"project_id"`
	Slug         string   `json:"slug"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	IconURL      string   `json:"icon_url"`
	Author       string   `json:"author"`
	Downloads    int64    `json:"downloads"`
	Follows      int64    `json:"follows"`
	Categories   []string `json:"categories"`
	Versions     []string `json:"versions"`
	ProjectType  string   `json:"project_type"`
	DateModified string   `json:"date_modified"`
	ServerSide   string   `json:"server_side"`
	ClientSide   string   `json:"client_side"`
}

type MRFile struct {
	URL      string            `json:"url"`
	Filename string            `json:"filename"`
	Primary  bool              `json:"primary"`
	Size     int64             `json:"size"`
	Hashes   map[string]string `json:"hashes"`
}

type MRDependency struct {
	ProjectID      string `json:"project_id"`
	VersionID      string `json:"version_id"`
	DependencyType string `json:"dependency_type"`
}

type MRVersion struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"project_id"`
	Name          string         `json:"name"`
	VersionNumber string         `json:"version_number"`
	VersionType   string         `json:"version_type"`
	GameVersions  []string       `json:"game_versions"`
	Loaders       []string       `json:"loaders"`
	DatePublished string         `json:"date_published"`
	Downloads     int64          `json:"downloads"`
	Files         []MRFile       `json:"files"`
	Dependencies  []MRDependency `json:"dependencies"`
}

func (v *MRVersion) PrimaryFile() *MRFile {
	for i := range v.Files {
		if v.Files[i].Primary {
			return &v.Files[i]
		}
	}
	if len(v.Files) > 0 {
		return &v.Files[0]
	}
	return nil
}

type MRProject struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IconURL     string `json:"icon_url"`
	ProjectType string `json:"project_type"`
}

func jsonParam(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Search queries Modrinth for plugins/mods compatible with the given loaders and version.
func (m *Modrinth) Search(ctx context.Context, query string, loaders []string, mcVersion string, mods bool, sort string, offset, limit int) ([]MRHit, int, error) {
	var facets [][]string
	var cats []string
	for _, l := range loaders {
		cats = append(cats, "categories:"+l)
	}
	if len(cats) > 0 {
		facets = append(facets, cats)
	}
	if mcVersion != "" {
		facets = append(facets, []string{"versions:" + mcVersion})
	}
	if mods {
		facets = append(facets, []string{"project_type:mod"}, []string{"server_side:required", "server_side:optional"})
	} else {
		facets = append(facets, []string{"project_type:plugin", "project_type:mod"})
	}
	switch sort {
	case "downloads", "follows", "newest", "updated", "relevance":
	default:
		sort = "relevance"
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q := url.Values{
		"query":  {query},
		"facets": {jsonParam(facets)},
		"index":  {sort},
		"offset": {strconv.Itoa(offset)},
		"limit":  {strconv.Itoa(limit)},
	}
	var resp struct {
		Hits      []MRHit `json:"hits"`
		TotalHits int     `json:"total_hits"`
	}
	if err := m.HTTP.GetJSON(ctx, modrinthAPI+"/search?"+q.Encode(), 2*time.Minute, &resp); err != nil {
		return nil, 0, err
	}
	if resp.Hits == nil {
		resp.Hits = []MRHit{}
	}
	return resp.Hits, resp.TotalHits, nil
}

// ProjectVersions lists versions of a project compatible with loaders/version (newest first).
func (m *Modrinth) ProjectVersions(ctx context.Context, project string, loaders []string, mcVersion string) ([]MRVersion, error) {
	q := url.Values{}
	if len(loaders) > 0 {
		q.Set("loaders", jsonParam(loaders))
	}
	if mcVersion != "" {
		q.Set("game_versions", jsonParam([]string{mcVersion}))
	}
	var out []MRVersion
	if err := m.HTTP.GetJSON(ctx, modrinthAPI+"/project/"+url.PathEscape(project)+"/version?"+q.Encode(), time.Minute, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *Modrinth) Version(ctx context.Context, id string) (*MRVersion, error) {
	var v MRVersion
	if err := m.HTTP.GetJSON(ctx, modrinthAPI+"/version/"+url.PathEscape(id), time.Minute, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (m *Modrinth) Project(ctx context.Context, id string) (*MRProject, error) {
	var p MRProject
	if err := m.HTTP.GetJSON(ctx, modrinthAPI+"/project/"+url.PathEscape(id), 10*time.Minute, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (m *Modrinth) Projects(ctx context.Context, ids []string) ([]MRProject, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out []MRProject
	err := m.HTTP.GetJSON(ctx, modrinthAPI+"/projects?ids="+url.QueryEscape(jsonParam(ids)), 10*time.Minute, &out)
	return out, err
}

// VersionsByHash maps sha1 hashes of installed files to their Modrinth versions.
func (m *Modrinth) VersionsByHash(ctx context.Context, hashes []string) (map[string]MRVersion, error) {
	out := map[string]MRVersion{}
	if len(hashes) == 0 {
		return out, nil
	}
	err := m.HTTP.PostJSON(ctx, modrinthAPI+"/version_files", map[string]any{"hashes": hashes, "algorithm": "sha1"}, &out)
	return out, err
}

// LatestByHash returns the newest compatible version for each installed file hash.
func (m *Modrinth) LatestByHash(ctx context.Context, hashes, loaders []string, mcVersion string) (map[string]MRVersion, error) {
	out := map[string]MRVersion{}
	if len(hashes) == 0 {
		return out, nil
	}
	body := map[string]any{"hashes": hashes, "algorithm": "sha1", "loaders": loaders}
	if mcVersion != "" {
		body["game_versions"] = []string{mcVersion}
	}
	err := m.HTTP.PostJSON(ctx, modrinthAPI+"/version_files/update", body, &out)
	return out, err
}

// BestVersion picks the newest release (or newest of any type) from a list.
func BestVersion(list []MRVersion) (*MRVersion, error) {
	if len(list) == 0 {
		return nil, errors.New("keine kompatible Version gefunden")
	}
	for i := range list {
		if list[i].VersionType == "release" {
			return &list[i], nil
		}
	}
	return &list[0], nil
}

// BestFile picks the file of a version that fits the loaders. Most projects
// publish one jar per version; some ship one per platform in the same version.
func BestFile(v *MRVersion, loaders []string) *MRFile {
	var jars []*MRFile
	for i := range v.Files {
		if strings.HasSuffix(strings.ToLower(v.Files[i].Filename), ".jar") {
			jars = append(jars, &v.Files[i])
		}
	}
	if len(jars) > 1 {
		for _, l := range loaders {
			for _, f := range jars {
				if strings.Contains(strings.ToLower(f.Filename), l) {
					return f
				}
			}
		}
	}
	if p := v.PrimaryFile(); p != nil {
		return p
	}
	if len(jars) > 0 {
		return jars[0]
	}
	return nil
}

// ValidJarName reports whether name is a plain plugin/mod file name.
func ValidJarName(n string) bool {
	return n != "" && !strings.ContainsAny(n, `/\`) && !strings.HasPrefix(n, ".") &&
		(strings.HasSuffix(n, ".jar") || strings.HasSuffix(n, ".jar.disabled"))
}

// InstallRequest describes a Modrinth installation into a plugins/ or mods/ folder.
type InstallRequest struct {
	Dir          string
	Project      string
	VersionID    string // empty: newest compatible version
	Loaders      []string
	GameVersion  string
	Dependencies bool
	Log          func(string)
}

// InstallProject downloads a project (and its required dependencies) into a
// plugins/ or mods/ folder and returns the written file names. Dependencies
// that are already installed (in any version) are skipped.
func (g *Game) InstallProject(ctx context.Context, req InstallRequest) ([]string, error) {
	logf := func(format string, a ...any) {
		if req.Log != nil {
			req.Log(fmt.Sprintf(format, a...))
		}
	}
	if err := os.MkdirAll(req.Dir, 0o755); err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	var hashes []string
	for _, p := range ListPlugins(req.Dir) {
		existing[strings.TrimSuffix(p.File, ".disabled")] = true
		if sum, err := httpx.FileHash(filepath.Join(req.Dir, p.File), "sha1"); err == nil {
			hashes = append(hashes, sum)
		}
	}
	// projects that are already present, to avoid a second copy of a dependency
	haveProject := map[string]bool{}
	if req.Dependencies && len(hashes) > 0 {
		if known, err := g.Modrinth.VersionsByHash(ctx, hashes); err == nil {
			for _, v := range known {
				haveProject[v.ProjectID] = true
			}
		}
	}
	installed := []string{}
	var install func(project, versionID string, depth int) error
	install = func(project, versionID string, depth int) error {
		if depth > 5 {
			return nil
		}
		var v *MRVersion
		var err error
		if versionID != "" {
			v, err = g.Modrinth.Version(ctx, versionID)
		} else {
			var list []MRVersion
			list, err = g.Modrinth.ProjectVersions(ctx, project, req.Loaders, req.GameVersion)
			if err == nil && len(list) == 0 && req.GameVersion != "" {
				// nothing for this exact version (plugins often lag behind new
				// patch releases): take the newest one for the same version line
				if all, e := g.Modrinth.ProjectVersions(ctx, project, req.Loaders, ""); e == nil {
					for i := range all {
						if all[i].VersionType != "release" {
							continue
						}
						for _, gv := range all[i].GameVersions {
							if sameVersionLine(gv, req.GameVersion) {
								list = append(list, all[i])
								break
							}
						}
					}
					if len(list) > 0 {
						logf("Keine Version ausdrücklich für Minecraft %s gefunden, nehme %s (für %s).", req.GameVersion, list[0].VersionNumber, strings.Join(list[0].GameVersions, ", "))
					}
				}
			}
			if err == nil {
				v, err = BestVersion(list)
			}
		}
		if err != nil {
			return fmt.Errorf("%s: %w", project, err)
		}
		if depth > 0 && haveProject[v.ProjectID] {
			return nil
		}
		f := BestFile(v, req.Loaders)
		if f == nil {
			return errors.New("Version enthält keine Datei")
		}
		name := filepath.Base(f.Filename)
		if !ValidJarName(name) {
			return errors.New("Unerwarteter Dateiname: " + name)
		}
		if !existing[name] || depth == 0 {
			var sum *httpx.Checksum
			if h512 := f.Hashes["sha512"]; h512 != "" {
				sum = &httpx.Checksum{Algo: "sha512", Hex: h512}
			} else if h1 := f.Hashes["sha1"]; h1 != "" {
				sum = &httpx.Checksum{Algo: "sha1", Hex: h1}
			}
			logf("Lade %s herunter …", name)
			if err := g.HTTP.Download(ctx, f.URL, filepath.Join(req.Dir, name), sum, nil); err != nil {
				return err
			}
			existing[name] = true
			haveProject[v.ProjectID] = true
			installed = append(installed, name)
		}
		if req.Dependencies {
			for _, d := range v.Dependencies {
				if d.DependencyType != "required" || (d.ProjectID == "" && d.VersionID == "") {
					continue
				}
				if d.ProjectID != "" && haveProject[d.ProjectID] {
					continue
				}
				if err := install(d.ProjectID, d.VersionID, depth+1); err != nil {
					logf("Abhängigkeit konnte nicht installiert werden: %v", err)
				}
			}
		}
		return nil
	}
	if err := install(req.Project, req.VersionID, 0); err != nil {
		return installed, err
	}
	return installed, nil
}

// sameVersionLine reports whether two Minecraft versions belong to the same
// line: 1.21.4 and 1.21.10 (1.21), 26.1 and 26.1.2 (26.1).
func sameVersionLine(a, b string) bool {
	line := func(v string) string {
		parts := strings.SplitN(v, ".", 3)
		if len(parts) < 2 {
			return v
		}
		return parts[0] + "." + parts[1]
	}
	return a != "" && b != "" && line(a) == line(b)
}
