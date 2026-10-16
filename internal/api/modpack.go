package api

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hearth/internal/minecraft"
	"hearth/internal/perms"
	"hearth/internal/servers"
	"hearth/internal/store"
)

// Modpack export: the panel builds a Modrinth pack (.mrpack) from the mods of a
// server. The configuration and the newest pack live in <data>/modpacks/<id>/.

type modpackConfig struct {
	Name          string        `json:"name"`
	Summary       string        `json:"summary"`
	ClientMods    []string      `json:"clientMods"` // Modrinth project IDs
	BundleUnknown bool          `json:"bundleUnknown"`
	ServerList    bool          `json:"serverList"`
	Address       string        `json:"address"` // for the server list; "" = automatic
	Public        bool          `json:"public"`  // offer the pack on the public page
	Build         int           `json:"build"`
	Last          *modpackBuild `json:"last,omitempty"`
}

type modpackBuild struct {
	Version   string                `json:"version"`
	FileName  string                `json:"fileName"`
	Size      int64                 `json:"size"`
	CreatedAt int64                 `json:"createdAt"`
	By        string                `json:"by"`
	Report    *minecraft.PackReport `json:"report"`
}

const mrpackType = "application/x-modrinth-modpack+zip"

func (a *API) modpackDir(serverID string) string {
	return filepath.Join(a.Cfg.DataDir, "modpacks", serverID)
}

func (a *API) loadModpack(srv *store.Server) modpackConfig {
	c := modpackConfig{Name: srv.Name, BundleUnknown: true, ServerList: true, ClientMods: []string{}}
	if b, err := os.ReadFile(filepath.Join(a.modpackDir(srv.ID), "modpack.json")); err == nil {
		json.Unmarshal(b, &c)
	}
	if c.ClientMods == nil {
		c.ClientMods = []string{}
	}
	if c.Last != nil {
		if _, err := os.Stat(filepath.Join(a.modpackDir(srv.ID), "pack.mrpack")); err != nil {
			c.Last = nil
		}
	}
	return c
}

func (a *API) saveModpack(serverID string, c modpackConfig) error {
	dir := a.modpackDir(serverID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := filepath.Join(dir, "modpack.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "modpack.json"))
}

// modpackAddress is the address written into the server list of the pack.
func (a *API) modpackAddress(r *http.Request, srv *store.Server, c modpackConfig) string {
	if c.Address != "" {
		return c.Address
	}
	if srv.DisplayAddress != "" {
		return srv.DisplayAddress
	}
	host := r.Host
	if u := a.PublicURL(r); u != "" {
		if i := strings.Index(u, "://"); i >= 0 {
			host = u[i+3:]
		}
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if srv.Port == 25565 {
		return host
	}
	return host + ":" + strconv.Itoa(srv.Port)
}

func packFileName(name, version string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r == 'ä':
			b.WriteString("ae")
		case r == 'ö':
			b.WriteString("oe")
		case r == 'ü':
			b.WriteString("ue")
		case r == 'ß':
			b.WriteString("ss")
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "modpack"
	}
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	return s + "-" + version + ".mrpack"
}

// modpackTarget checks that a server can have a modpack and returns its
// mods folder, Minecraft version and loader version.
func (a *API) modpackTarget(inst *servers.Instance, h *minecraft.Handler) (dir, mc, loader string, err error) {
	s := h.Settings()
	if minecraft.PackLoaderKey(s.Flavor) == "" {
		return "", "", "", badRequest("Modpacks gibt es nur für Fabric-, Forge- und NeoForge-Server")
	}
	mc = s.InstalledVersion
	if mc == "" {
		mc = s.Version
	}
	loader = s.InstalledBuild
	if loader == "" {
		loader = minecraft.LoaderVersion(inst.Dir(), s.Flavor, mc)
	}
	return filepath.Join(inst.Dir(), "mods"), mc, loader, nil
}

// GET /api/servers/{id}/mc/modpack
func (a *API) mcModpack(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	srv := inst.Server()
	s := h.Settings()
	dir, mc, loader, err := a.modpackTarget(inst, h)
	if err != nil {
		ok(w, map[string]any{"supported": false, "flavor": s.Flavor})
		return nil
	}
	c := a.loadModpack(srv)
	out := map[string]any{
		"supported": true, "flavor": s.Flavor, "mcVersion": mc, "loaderVersion": loader, "config": c,
		"defaultAddress": a.modpackAddress(r, srv, modpackConfig{}), "publicPage": srv.PublicEnabled && srv.PublicSlug != "",
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	mods, err := a.MC.AnalyzeMods(ctx, dir)
	out["mods"] = mods
	if err != nil {
		out["modsError"] = err.Error()
	}
	clients := []minecraft.ClientMod{}
	if len(c.ClientMods) > 0 {
		if list, err := a.MC.Modrinth.Projects(ctx, c.ClientMods); err == nil {
			for _, p := range list {
				clients = append(clients, minecraft.ClientMod{ProjectID: p.ID, Slug: p.Slug, Title: p.Title, Description: p.Description, IconURL: p.IconURL})
			}
		}
	}
	out["clientMods"] = clients
	if srv.PublicEnabled && srv.PublicSlug != "" {
		out["publicUrl"] = a.PublicURL(r) + "/s/" + srv.PublicSlug
	}
	ok(w, out)
	return nil
}

// GET /api/servers/{id}/mc/modpack/client-mods?q=&offset=
func (a *API) mcModpackSearch(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	_, mc, _, err := a.modpackTarget(inst, h)
	if err != nil {
		return err
	}
	hits, total, err := a.MC.Modrinth.SearchClientMods(r.Context(), r.URL.Query().Get("q"), minecraft.ModrinthLoaders(h.Settings().Flavor), mc,
		queryInt(r, "offset", 0), queryInt(r, "limit", 24))
	if err != nil {
		return badRequest("Modrinth nicht erreichbar: " + err.Error())
	}
	ok(w, map[string]any{"hits": hits, "total": total})
	return nil
}

func readModpackConfig(r *http.Request, c *modpackConfig) error {
	var req struct {
		Name          *string   `json:"name"`
		Summary       *string   `json:"summary"`
		ClientMods    *[]string `json:"clientMods"`
		BundleUnknown *bool     `json:"bundleUnknown"`
		ServerList    *bool     `json:"serverList"`
		Address       *string   `json:"address"`
		Public        *bool     `json:"public"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" || len(n) > 80 {
			return badRequest("Bitte einen Namen (max. 80 Zeichen) angeben")
		}
		c.Name = n
	}
	if req.Summary != nil {
		if len(*req.Summary) > 300 {
			return badRequest("Die Beschreibung ist zu lang (max. 300 Zeichen)")
		}
		c.Summary = strings.TrimSpace(*req.Summary)
	}
	if req.ClientMods != nil {
		seen := map[string]bool{}
		c.ClientMods = []string{}
		for _, id := range *req.ClientMods {
			id = strings.TrimSpace(id)
			if id == "" || len(id) > 64 || strings.ContainsAny(id, "/?#&% ") || seen[id] {
				continue
			}
			seen[id] = true
			c.ClientMods = append(c.ClientMods, id)
		}
		if len(c.ClientMods) > 100 {
			return badRequest("Höchstens 100 Client-Mods")
		}
	}
	if req.BundleUnknown != nil {
		c.BundleUnknown = *req.BundleUnknown
	}
	if req.ServerList != nil {
		c.ServerList = *req.ServerList
	}
	if req.Address != nil {
		addr := strings.TrimSpace(*req.Address)
		if len(addr) > 255 || strings.ContainsAny(addr, " /\\") {
			return badRequest("Ungültige Serveradresse")
		}
		c.Address = addr
	}
	if req.Public != nil {
		c.Public = *req.Public
	}
	return nil
}

// PUT /api/servers/{id}/mc/modpack saves the settings without building.
func (a *API) mcModpackSave(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	if _, _, _, err := a.modpackTarget(inst, h); err != nil {
		return err
	}
	a.packMu.Lock()
	defer a.packMu.Unlock()
	c := a.loadModpack(inst.Server())
	if err := readModpackConfig(r, &c); err != nil {
		return err
	}
	if err := a.saveModpack(inst.ID(), c); err != nil {
		return err
	}
	ok(w, map[string]any{"config": c})
	return nil
}

// POST /api/servers/{id}/mc/modpack/build saves the settings and builds a new pack.
func (a *API) mcModpackBuild(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	dir, mc, loader, err := a.modpackTarget(inst, h)
	if err != nil {
		return err
	}
	a.packMu.Lock()
	defer a.packMu.Unlock()
	srv := inst.Server()
	c := a.loadModpack(srv)
	if err := readModpackConfig(r, &c); err != nil {
		return err
	}
	if loader == "" {
		return badRequest("Die Loader-Version des Servers ist unbekannt. Installiere den Server unter „Start & Version“ einmal neu.")
	}
	c.Build++
	version := strconv.Itoa(c.Build)
	req := minecraft.PackRequest{
		ModsDir: dir, Name: c.Name, Summary: c.Summary, VersionID: version, Flavor: h.Settings().Flavor,
		MCVersion: mc, LoaderVersion: loader, ClientMods: c.ClientMods, BundleUnknown: c.BundleUnknown, ServerName: c.Name,
	}
	if c.ServerList {
		req.ServerAddress = a.modpackAddress(r, srv, c)
	}
	pdir := a.modpackDir(srv.ID)
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(pdir, "pack-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	rep, err := a.MC.BuildPack(ctx, req, tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return badRequest(err.Error())
	}
	st, err := os.Stat(tmp.Name())
	if err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(pdir, "pack.mrpack")); err != nil {
		return err
	}
	c.Last = &modpackBuild{Version: version, FileName: packFileName(c.Name, version), Size: st.Size(),
		CreatedAt: time.Now().Unix(), By: u.Username, Report: rep}
	if err := a.saveModpack(srv.ID, c); err != nil {
		return err
	}
	a.audit(r, srv.ID, "mc.modpack_build", map[string]any{"version": version, "mods": rep.Mods, "clientMods": rep.ClientMods})
	ok(w, map[string]any{"config": c})
	return nil
}

// GET /api/servers/{id}/mc/modpack/download
func (a *API) mcModpackDownload(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	return a.serveModpack(w, r, inst.Server())
}

func (a *API) serveModpack(w http.ResponseWriter, r *http.Request, srv *store.Server) error {
	c := a.loadModpack(srv)
	if c.Last == nil {
		return errNotFound
	}
	f, err := os.Open(filepath.Join(a.modpackDir(srv.ID), "pack.mrpack"))
	if err != nil {
		return errNotFound
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", mrpackType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": c.Last.FileName}))
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "", st.ModTime(), f)
	return nil
}

// GET /api/public/{slug}/modpack.mrpack
func (a *API) publicModpack(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	if pc.mc == nil || !a.loadModpack(pc.srv).Public {
		return errNotFound
	}
	return a.serveModpack(w, r, pc.srv)
}

// publicModpackInfo describes the pack for the public page (nil = not offered).
func (a *API) publicModpackInfo(pc *publicCtx) map[string]any {
	if pc.mc == nil {
		return nil
	}
	c := a.loadModpack(pc.srv)
	if !c.Public || c.Last == nil {
		return nil
	}
	return map[string]any{
		"name": c.Name, "version": c.Last.Version, "size": c.Last.Size, "createdAt": c.Last.CreatedAt,
		"url": fmt.Sprintf("/api/public/%s/modpack.mrpack", pc.srv.PublicSlug),
	}
}
