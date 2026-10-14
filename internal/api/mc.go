package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"hearth/internal/httpx"
	"hearth/internal/mapr"
	"hearth/internal/minecraft"
	"hearth/internal/perms"
	"hearth/internal/servers"
	"hearth/internal/store"
)

type mcHandler func(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error

// mc wraps a handler for Minecraft-only endpoints.
func (a *API) mc(perm string, fn mcHandler) http.HandlerFunc {
	return a.srv(perm, func(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
		h, ok := inst.Handler().(*minecraft.Handler)
		if !ok {
			return badRequest("Diese Funktion gibt es nur für Minecraft-Server")
		}
		return fn(w, r, u, inst, ps, h)
	})
}

// GET /api/servers/{id}/mc/status
func (a *API) mcStatus(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	out := map[string]any{"info": h.Info()}
	if inst.Running() {
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		if p, err := h.Ping(ctx); err == nil {
			out["ping"] = p
		} else {
			out["pingError"] = err.Error()
		}
	}
	out["world"] = h.World()
	ok(w, out)
	return nil
}

// GET /api/servers/{id}/mc/properties
func (a *API) mcProperties(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	props, err := h.Properties()
	if err != nil {
		return err
	}
	ok(w, map[string]any{"values": props, "known": minecraft.KnownProperties})
	return nil
}

// PUT /api/servers/{id}/mc/properties
func (a *API) mcSetProperties(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	var req struct {
		Values map[string]string `json:"values"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if err := h.SetProperties(req.Values); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "mc.properties", map[string]any{"keys": keys(req.Values)})
	props, _ := h.Properties()
	ok(w, map[string]any{"values": props, "restartRequired": inst.State() != "offline"})
	return nil
}

// GET /api/servers/{id}/mc/players
func (a *API) mcPlayers(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	q := r.URL.Query()
	list, total, err := a.Store.ListMCPlayers(r.Context(), inst.ID(), store.MCPlayerQuery{
		Sort: q.Get("sort"), Asc: q.Get("dir") == "asc", Search: q.Get("q"),
		Limit: queryInt(r, "limit", 50), Offset: queryInt(r, "offset", 0),
	})
	if err != nil {
		return err
	}
	online := map[string]bool{}
	for _, p := range h.OnlinePlayers() {
		online[p.UUID] = true
	}
	ops := map[string]bool{}
	if l, err := minecraft.ReadList(inst.Dir(), "ops.json"); err == nil {
		for _, e := range l {
			ops[strings.ToLower(e.UUID)] = true
		}
	}
	type view struct {
		*store.MCPlayer
		Online      bool  `json:"online"`
		Op          bool  `json:"op"`
		PlaySeconds int64 `json:"playSeconds"`
	}
	out := make([]view, 0, len(list))
	for _, p := range list {
		out = append(out, view{MCPlayer: p, Online: online[p.UUID], Op: ops[strings.ToLower(p.UUID)], PlaySeconds: p.PlaySeconds()})
	}
	totals, _ := a.Store.MCTotals(r.Context(), inst.ID())
	ok(w, map[string]any{"players": out, "total": total, "online": h.OnlinePlayers(), "totals": totals})
	return nil
}

// playerDetail assembles the profile of one player (shared with the public page).
func (a *API) playerDetail(ctx context.Context, serverID, uuid string, withChat bool) (map[string]any, error) {
	p, err := a.Store.GetMCPlayer(ctx, serverID, uuid)
	if err != nil {
		return nil, err
	}
	types := []string{"death", "advancement"}
	if withChat {
		types = append(types, "chat")
	}
	events, _ := a.Store.ListMCEvents(ctx, serverID, store.MCEventQuery{UUID: uuid, Types: types, Limit: 50})
	sessions, _ := a.Store.MCRecentSessions(ctx, serverID, uuid, 20)
	causes, _ := a.Store.MCDeathCauses(ctx, serverID, uuid, 0)
	since := time.Now().AddDate(0, 0, -30)
	daily := dailyPlaytime(a.mustSessions(ctx, serverID, uuid, since.Unix()), since)
	detail := map[string]any{
		"player": p, "playSeconds": p.PlaySeconds(), "events": events, "sessions": sessions,
		"deathCauses": causes, "causeLabels": minecraft.DeathCauses, "daily": daily,
	}
	if len(p.Stats) > 0 {
		detail["stats"] = summarizeStats(p.Stats)
	}
	return detail, nil
}

func (a *API) mustSessions(ctx context.Context, serverID, uuid string, since int64) []store.MCSession {
	s, _ := a.Store.MCSessionsSince(ctx, serverID, uuid, since)
	return s
}

// dailyPlaytime distributes sessions over calendar days (seconds per day).
func dailyPlaytime(sessions []store.MCSession, since time.Time) []map[string]any {
	days := map[string]int64{}
	now := time.Now()
	for _, s := range sessions {
		start := time.Unix(s.JoinedAt, 0)
		end := now
		if s.LeftAt > 0 {
			end = time.Unix(s.LeftAt, 0)
		}
		if start.Before(since) {
			start = since
		}
		for start.Before(end) {
			dayEnd := time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 0, 0, start.Location())
			seg := end
			if dayEnd.Before(end) {
				seg = dayEnd
			}
			days[start.Format("2006-01-02")] += int64(seg.Sub(start).Seconds())
			start = seg
		}
	}
	out := []map[string]any{}
	for d := since; !d.After(now); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		out = append(out, map[string]any{"day": k, "seconds": days[k]})
	}
	return out
}

// summarizeStats converts the raw stats file into top lists for the profile page.
func summarizeStats(raw json.RawMessage) map[string]any {
	var sf struct {
		Stats map[string]map[string]int64 `json:"stats"`
	}
	if json.Unmarshal(raw, &sf) != nil {
		return nil
	}
	top := func(cat string, n int) []map[string]any {
		type kv struct {
			k string
			v int64
		}
		var list []kv
		for k, v := range sf.Stats[cat] {
			if v > 0 {
				list = append(list, kv{strings.TrimPrefix(k, "minecraft:"), v})
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
		out := []map[string]any{}
		for i := 0; i < len(list) && i < n; i++ {
			out = append(out, map[string]any{"id": list[i].k, "value": list[i].v})
		}
		return out
	}
	custom := map[string]int64{}
	for k, v := range sf.Stats["minecraft:custom"] {
		custom[strings.TrimPrefix(k, "minecraft:")] = v
	}
	return map[string]any{
		"custom": custom, "mined": top("minecraft:mined", 10), "killed": top("minecraft:killed", 10),
		"killedBy": top("minecraft:killed_by", 10), "crafted": top("minecraft:crafted", 10),
		"used": top("minecraft:used", 10), "broken": top("minecraft:broken", 5), "pickedUp": top("minecraft:picked_up", 10),
	}
}

// GET /api/servers/{id}/mc/players/{uuid}
func (a *API) mcPlayer(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	uuid := minecraft.NormalizeUUID(r.PathValue("uuid"))
	if uuid == "" {
		return errNotFound
	}
	d, err := a.playerDetail(r.Context(), inst.ID(), uuid, true)
	if err != nil {
		return err
	}
	for _, p := range h.OnlinePlayers() {
		if p.UUID == uuid {
			d["online"] = true
			d["onlineSince"] = p.Since
		}
	}
	ok(w, d)
	return nil
}

// GET /api/servers/{id}/mc/lists
func (a *API) mcLists(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	out := map[string]any{}
	for key, file := range minecraft.ListFiles {
		l, err := minecraft.ReadList(inst.Dir(), file)
		if err != nil {
			l = []minecraft.ListEntry{}
		}
		out[key] = l
	}
	props, _ := h.Properties()
	out["whitelistEnabled"] = props["white-list"] == "true"
	out["onlineMode"] = props["online-mode"] != "false"
	ok(w, out)
	return nil
}

// POST /api/servers/{id}/mc/action {action, name, uuid, ip, reason}
func (a *API) mcAction(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	var req struct {
		Action string `json:"action"`
		Name   string `json:"name"`
		UUID   string `json:"uuid"`
		IP     string `json:"ip"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Reason = strings.ReplaceAll(strings.TrimSpace(req.Reason), "\n", " ")
	if len(req.Reason) > 200 {
		req.Reason = req.Reason[:200]
	}
	needsName := map[string]bool{"kick": true, "ban": true, "pardon": true, "op": true, "deop": true,
		"whitelist_add": true, "whitelist_remove": true}
	if needsName[req.Action] && !minecraft.ValidPlayerName(req.Name) {
		return badRequest("Ungültiger Spielername")
	}
	if (req.Action == "ban_ip" || req.Action == "pardon_ip") && (req.IP == "" || strings.ContainsAny(req.IP, " \n;")) {
		return badRequest("Ungültige IP-Adresse")
	}
	var cmd string
	switch req.Action {
	case "kick":
		cmd = "kick " + req.Name
		if req.Reason != "" {
			cmd += " " + req.Reason
		}
	case "ban":
		cmd = "ban " + req.Name
		if req.Reason != "" {
			cmd += " " + req.Reason
		}
	case "pardon":
		cmd = "pardon " + req.Name
	case "op":
		cmd = "op " + req.Name
	case "deop":
		cmd = "deop " + req.Name
	case "whitelist_add":
		cmd = "whitelist add " + req.Name
	case "whitelist_remove":
		cmd = "whitelist remove " + req.Name
	case "ban_ip":
		cmd = "ban-ip " + req.IP
		if req.Reason != "" {
			cmd += " " + req.Reason
		}
	case "pardon_ip":
		cmd = "pardon-ip " + req.IP
	case "whitelist_on":
		cmd = "whitelist on"
	case "whitelist_off":
		cmd = "whitelist off"
	default:
		return badRequest("Unbekannte Aktion")
	}
	if inst.Running() {
		if out, err := h.RCONExec(cmd); err == nil {
			a.audit(r, inst.ID(), "mc.action", map[string]any{"action": req.Action, "player": req.Name, "ip": req.IP})
			ok(w, map[string]any{"result": out})
			return nil
		}
		if err := inst.Command(cmd, u.Username); err != nil {
			return err
		}
		a.audit(r, inst.ID(), "mc.action", map[string]any{"action": req.Action, "player": req.Name, "ip": req.IP})
		ok(w, map[string]any{"result": "Befehl gesendet"})
		return nil
	}
	// server offline: edit the JSON files directly
	switch req.Action {
	case "kick":
		return badRequest("Der Server ist offline")
	case "whitelist_on", "whitelist_off":
		if err := h.SetProperties(map[string]string{"white-list": strconv.FormatBool(req.Action == "whitelist_on")}); err != nil {
			return err
		}
	default:
		entry := minecraft.ListEntry{Name: req.Name, UUID: minecraft.NormalizeUUID(req.UUID), IP: req.IP, Reason: req.Reason}
		if req.Name != "" && entry.UUID == "" {
			entry.UUID = a.resolveUUID(r.Context(), inst, h, req.Name)
			if entry.UUID == "" {
				return badRequest("Spieler " + req.Name + " wurde nicht gefunden")
			}
		}
		if err := minecraft.EditListOffline(inst.Dir(), req.Action, entry); err != nil {
			return err
		}
	}
	a.audit(r, inst.ID(), "mc.action", map[string]any{"action": req.Action, "player": req.Name, "ip": req.IP, "offline": true})
	ok(w, map[string]any{"result": "Gespeichert (wird beim nächsten Start aktiv)"})
	return nil
}

func (a *API) resolveUUID(ctx context.Context, inst *servers.Instance, h *minecraft.Handler, name string) string {
	if p, err := a.Store.GetMCPlayerByName(ctx, inst.ID(), name); err == nil {
		return p.UUID
	}
	for uuid, n := range minecraft.ReadUsercache(inst.Dir()) {
		if strings.EqualFold(n, name) {
			return uuid
		}
	}
	props, _ := h.Properties()
	if props["online-mode"] == "false" {
		return minecraft.OfflineUUID(name)
	}
	uuid, _, err := minecraft.LookupUUID(ctx, a.MC.HTTP, name)
	if err != nil {
		return ""
	}
	return uuid
}

// GET /api/servers/{id}/mc/events?types=death,advancement&before=&limit=
func (a *API) mcEvents(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	q := store.MCEventQuery{Limit: queryInt(r, "limit", 50), UUID: minecraft.NormalizeUUID(r.URL.Query().Get("uuid"))}
	if t := r.URL.Query().Get("types"); t != "" {
		q.Types = strings.Split(t, ",")
	}
	q.Before, _ = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	list, err := a.Store.ListMCEvents(r.Context(), inst.ID(), q)
	if err != nil {
		return err
	}
	ok(w, map[string]any{"events": list, "causeLabels": minecraft.DeathCauses})
	return nil
}

// POST /api/servers/{id}/mc/stats/refresh
func (a *API) mcRefreshStats(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	if inst.Running() {
		h.RCONExec("save-all")
		time.Sleep(1500 * time.Millisecond)
	}
	n, err := h.ImportStats(r.Context())
	if err != nil {
		return err
	}
	ok(w, map[string]any{"updated": n})
	return nil
}

// ---- version change ----

// POST /api/servers/{id}/mc/version {flavor, version, build}
func (a *API) mcChangeVersion(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	var req struct {
		Flavor  string `json:"flavor"`
		Version string `json:"version"`
		Build   string `json:"build"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if inst.State() != "offline" {
		return errOffline
	}
	if _, ok := minecraft.FlavorByID(req.Flavor); !ok {
		return badRequest("Unbekannte Server-Art")
	}
	if req.Flavor != "custom" && req.Version == "" {
		return badRequest("Bitte eine Version wählen")
	}
	err := inst.UpdateSettings(func(raw json.RawMessage) (json.RawMessage, error) {
		s := minecraft.DecodeSettings(raw)
		s.Flavor, s.Version, s.Build = req.Flavor, req.Version, req.Build
		if req.Flavor == "custom" {
			s.LoaderArgsFile = ""
		}
		return s.Encode(), nil
	})
	if err != nil {
		return err
	}
	if err := inst.Install(u.Username, false); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "mc.version", map[string]any{"flavor": req.Flavor, "version": req.Version, "build": req.Build})
	ok(w, nil)
	return nil
}

// ---- server icon ----

// POST /api/servers/{id}/mc/icon (raw image body) — converts to 64×64 server-icon.png
func (a *API) mcIcon(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	if r.Method == http.MethodDelete {
		os.Remove(filepath.Join(inst.Dir(), "server-icon.png"))
		ok(w, nil)
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return badRequest("Bild konnte nicht gelesen werden (PNG, JPEG oder GIF)")
	}
	out := resizeSquare(img, 64)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(inst.Dir(), "server-icon.png"), buf.Bytes(), 0o644); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "mc.icon", nil)
	ok(w, nil)
	return nil
}

// resizeSquare center-crops and box-filters an image to size×size.
func resizeSquare(src image.Image, size int) *image.NRGBA {
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	ox, oy := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		y0, y1 := oy+y*side/size, oy+(y+1)*side/size
		for x := 0; x < size; x++ {
			x0, x1 := ox+x*side/size, ox+(x+1)*side/size
			var r, g, bl, al, n uint64
			for sy := y0; sy < max(y1, y0+1); sy++ {
				for sx := x0; sx < max(x1, x0+1); sx++ {
					c := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
					r += uint64(c.R) * uint64(c.A)
					g += uint64(c.G) * uint64(c.A)
					bl += uint64(c.B) * uint64(c.A)
					al += uint64(c.A)
					n++
				}
			}
			if al > 0 {
				dst.SetNRGBA(x, y, color.NRGBA{uint8(r / al), uint8(g / al), uint8(bl / al), uint8(al / n)})
			}
		}
	}
	return dst
}

// serveIcon writes the server-icon.png (or a 404).
func serveIcon(w http.ResponseWriter, r *http.Request, dir string) {
	f, err := os.Open(filepath.Join(dir, "server-icon.png"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, "icon.png", st.ModTime(), f)
}

// GET /api/servers/{id}/icon.png
func (a *API) serverIcon(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	serveIcon(w, r, inst.Dir())
	return nil
}

// ---- worlds ----

// GET /api/servers/{id}/mc/worlds
func (a *API) mcWorlds(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	world := h.World()
	type dimView struct {
		minecraft.Dimension
		Size int64 `json:"size"`
	}
	var dims []dimView
	for _, d := range world.Dimensions {
		dims = append(dims, dimView{Dimension: d, Size: dirSize(filepath.Dir(d.RegionDir))})
	}
	ok(w, map[string]any{"world": world, "dimensions": dims, "size": dirSize(world.Root)})
	return nil
}

func dirSize(p string) int64 {
	var total int64
	filepath.WalkDir(p, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if i, err := d.Info(); err == nil {
				total += i.Size()
			}
		}
		return nil
	})
	return total
}

// POST /api/servers/{id}/mc/worlds/reset {dimension: "" | key, seed}
func (a *API) mcResetWorld(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	if inst.State() != "offline" {
		return errOffline
	}
	var req struct {
		Dimension string `json:"dimension"`
		Seed      string `json:"seed"`
		Confirm   string `json:"confirm"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	world := h.World()
	if req.Confirm != world.Name {
		return badRequest("Zur Bestätigung den Weltnamen eingeben")
	}
	if req.Dimension == "" {
		os.RemoveAll(world.Root)
		os.RemoveAll(filepath.Join(inst.Dir(), world.Name+"_nether"))
		os.RemoveAll(filepath.Join(inst.Dir(), world.Name+"_the_end"))
		if strings.TrimSpace(req.Seed) != "" {
			h.SetProperties(map[string]string{"level-seed": strings.TrimSpace(req.Seed)})
		}
		a.Maps.Clear(inst.ID())
	} else {
		d := world.Dimension(req.Dimension)
		if d == nil {
			return badRequest("Dimension nicht gefunden")
		}
		if d.ID == "minecraft:overworld" {
			return badRequest("Die Oberwelt kann nur zusammen mit der ganzen Welt zurückgesetzt werden")
		}
		base := filepath.Dir(d.RegionDir)
		for _, sub := range []string{"region", "entities", "poi"} {
			os.RemoveAll(filepath.Join(base, sub))
		}
		os.RemoveAll(filepath.Join(a.Cfg.MapsDir(), inst.ID(), d.Key))
	}
	inst.RefreshDisk()
	a.audit(r, inst.ID(), "mc.world_reset", map[string]any{"dimension": req.Dimension})
	ok(w, nil)
	return nil
}

// ---- plugins & mods ----

func (a *API) pluginDir(inst *servers.Instance, h *minecraft.Handler) (string, error) {
	d := minecraft.ModDir(h.Settings().Flavor)
	if d == "" {
		return "", badRequest("Vanilla-Server unterstützen keine Plugins oder Mods")
	}
	return filepath.Join(inst.Dir(), d), nil
}

// GET /api/servers/{id}/mc/plugins
func (a *API) mcPlugins(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	dir, err := a.pluginDir(inst, h)
	if err != nil {
		ok(w, map[string]any{"supported": false, "plugins": []any{}})
		return nil
	}
	list := minecraft.ListPlugins(dir)
	a.enrichPlugins(r.Context(), dir, list, false, h)
	s := h.Settings()
	ok(w, map[string]any{"supported": true, "dir": minecraft.ModDir(s.Flavor), "plugins": list,
		"kind": map[bool]string{true: "mods", false: "plugins"}[minecraft.ModDir(s.Flavor) == "mods"]})
	return nil
}

// enrichPlugins adds Modrinth project info (and optionally update availability).
func (a *API) enrichPlugins(ctx context.Context, dir string, list []minecraft.InstalledPlugin, checkUpdates bool, h *minecraft.Handler) error {
	if len(list) == 0 {
		return nil
	}
	hashes := []string{}
	byHash := map[string]int{}
	for i := range list {
		sum, err := httpx.FileHash(filepath.Join(dir, list[i].File), "sha1")
		if err != nil {
			continue
		}
		list[i].SHA1 = sum
		hashes = append(hashes, sum)
		byHash[sum] = i
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	versions, err := a.MC.Modrinth.VersionsByHash(ctx, hashes)
	if err != nil {
		return err
	}
	var ids []string
	for hash, v := range versions {
		if i, ok := byHash[hash]; ok {
			list[i].ProjectID, list[i].VersionID = v.ProjectID, v.ID
			ids = append(ids, v.ProjectID)
		}
	}
	if projects, err := a.MC.Modrinth.Projects(ctx, ids); err == nil {
		pm := map[string]minecraft.MRProject{}
		for _, p := range projects {
			pm[p.ID] = p
		}
		for i := range list {
			if p, ok := pm[list[i].ProjectID]; ok {
				list[i].IconURL, list[i].Slug = p.IconURL, p.Slug
				if list[i].Name == "" || list[i].Name == strings.TrimSuffix(list[i].File, ".jar") {
					list[i].Name = p.Title
				}
			}
		}
	}
	if checkUpdates {
		s := h.Settings()
		latest, err := a.MC.Modrinth.LatestByHash(ctx, hashes, minecraft.ModrinthLoaders(s.Flavor), s.Version)
		if err != nil {
			return err
		}
		for hash, v := range latest {
			i, ok := byHash[hash]
			if !ok || v.ID == list[i].VersionID {
				continue
			}
			list[i].Update = &struct {
				VersionID     string `json:"versionId"`
				VersionNumber string `json:"versionNumber"`
			}{v.ID, v.VersionNumber}
		}
	}
	return nil
}

// POST /api/servers/{id}/mc/plugins/check-updates
func (a *API) mcCheckUpdates(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	dir, err := a.pluginDir(inst, h)
	if err != nil {
		return err
	}
	list := minecraft.ListPlugins(dir)
	if err := a.enrichPlugins(r.Context(), dir, list, true, h); err != nil {
		return badRequest("Modrinth nicht erreichbar: " + err.Error())
	}
	ok(w, map[string]any{"plugins": list})
	return nil
}

func validJarName(n string) bool {
	return n != "" && !strings.ContainsAny(n, `/\`) && !strings.HasPrefix(n, ".") &&
		(strings.HasSuffix(n, ".jar") || strings.HasSuffix(n, ".jar.disabled"))
}

// POST /api/servers/{id}/mc/plugins/toggle {file}
func (a *API) mcTogglePlugin(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	dir, err := a.pluginDir(inst, h)
	if err != nil {
		return err
	}
	var req struct {
		File string `json:"file"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if !validJarName(req.File) {
		return badRequest("Ungültiger Dateiname")
	}
	from := filepath.Join(dir, req.File)
	to := strings.TrimSuffix(from, ".disabled")
	if !strings.HasSuffix(req.File, ".disabled") {
		to = from + ".disabled"
	}
	if err := os.Rename(from, to); err != nil {
		return fsErr(err)
	}
	a.audit(r, inst.ID(), "mc.plugin_toggle", map[string]any{"file": req.File})
	ok(w, map[string]any{"file": filepath.Base(to)})
	return nil
}

// DELETE /api/servers/{id}/mc/plugins?file=
func (a *API) mcDeletePlugin(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	dir, err := a.pluginDir(inst, h)
	if err != nil {
		return err
	}
	file := r.URL.Query().Get("file")
	if !validJarName(file) {
		return badRequest("Ungültiger Dateiname")
	}
	if err := os.Remove(filepath.Join(dir, file)); err != nil {
		return fsErr(err)
	}
	a.audit(r, inst.ID(), "mc.plugin_delete", map[string]any{"file": file})
	ok(w, nil)
	return nil
}

// GET /api/servers/{id}/mc/modrinth/search?q=&sort=&offset=
func (a *API) mcModrinthSearch(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	s := h.Settings()
	if minecraft.ModDir(s.Flavor) == "" {
		return badRequest("Vanilla-Server unterstützen keine Plugins oder Mods")
	}
	mods := minecraft.ModDir(s.Flavor) == "mods"
	version := s.Version
	if r.URL.Query().Get("anyVersion") == "1" {
		version = ""
	}
	hits, total, err := a.MC.Modrinth.Search(r.Context(), r.URL.Query().Get("q"), minecraft.ModrinthLoaders(s.Flavor), version, mods,
		r.URL.Query().Get("sort"), queryInt(r, "offset", 0), queryInt(r, "limit", 20))
	if err != nil {
		return badRequest("Modrinth nicht erreichbar: " + err.Error())
	}
	ok(w, map[string]any{"hits": hits, "total": total, "kind": map[bool]string{true: "mods", false: "plugins"}[mods]})
	return nil
}

// GET /api/servers/{id}/mc/modrinth/versions?project=
func (a *API) mcModrinthVersions(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	s := h.Settings()
	project := r.URL.Query().Get("project")
	if project == "" {
		return badRequest("Projekt fehlt")
	}
	version := s.Version
	if r.URL.Query().Get("anyVersion") == "1" {
		version = ""
	}
	list, err := a.MC.Modrinth.ProjectVersions(r.Context(), project, minecraft.ModrinthLoaders(s.Flavor), version)
	if err != nil {
		return badRequest("Modrinth nicht erreichbar: " + err.Error())
	}
	ok(w, list)
	return nil
}

// POST /api/servers/{id}/mc/modrinth/install {project, versionId, dependencies, replace}
func (a *API) mcModrinthInstall(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	var req struct {
		Project      string `json:"project"`
		VersionID    string `json:"versionId"`
		Dependencies bool   `json:"dependencies"`
		Replace      string `json:"replace"` // file to remove after installing (updates)
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	dir, err := a.pluginDir(inst, h)
	if err != nil {
		return err
	}
	s := h.Settings()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	installed, err := a.MC.InstallProject(ctx, minecraft.InstallRequest{
		Dir: dir, Project: req.Project, VersionID: req.VersionID, Loaders: minecraft.ModrinthLoaders(s.Flavor),
		GameVersion: s.Version, Dependencies: req.Dependencies, Log: inst.Console,
	})
	if err != nil && len(installed) == 0 {
		return badRequest(err.Error())
	}
	if req.Replace != "" && validJarName(req.Replace) {
		keep := false
		for _, n := range installed {
			if n == req.Replace {
				keep = true
			}
		}
		if !keep {
			os.Remove(filepath.Join(dir, req.Replace))
		}
	}
	a.audit(r, inst.ID(), "mc.plugin_install", map[string]any{"project": req.Project, "files": installed})
	ok(w, map[string]any{"installed": installed, "restartRequired": inst.State() != "offline"})
	return nil
}

// ---- map ----

func (a *API) dimSpecs(h *minecraft.Handler, only []string) []mapr.DimSpec {
	s := h.Settings()
	want := map[string]bool{}
	for _, d := range s.Map.Dimensions {
		want[d] = true
	}
	filter := map[string]bool{}
	for _, d := range only {
		filter[d] = true
	}
	var out []mapr.DimSpec
	for _, d := range h.World().Dimensions {
		if len(want) > 0 && !want[d.Key] {
			continue
		}
		if len(filter) > 0 && !filter[d.Key] {
			continue
		}
		out = append(out, mapr.DimSpec{Key: d.Key, RegionDir: d.RegionDir, Nether: d.ID == "minecraft:the_nether"})
	}
	return out
}

func (a *API) mapInfo(r *http.Request, inst *servers.Instance, h *minecraft.Handler) map[string]any {
	serverID := inst.ID()
	s := h.Settings()
	world := h.World()
	dims := []map[string]any{}
	found := []map[string]any{}
	for _, d := range world.Dimensions {
		found = append(found, map[string]any{"key": d.Key, "id": d.ID, "label": d.Label, "regions": d.Regions})
		info := a.Maps.Info(serverID, d.Key)
		if info.Regions == 0 {
			continue
		}
		dims = append(dims, map[string]any{"key": d.Key, "id": d.ID, "label": d.Label, "info": info})
	}
	spawn := worldSpawn(world)
	out := map[string]any{
		"settings": s.Map, "dimensions": dims, "render": a.Maps.Status(serverID), "spawn": spawn,
		// what the renderer finds on disk, for diagnostics in the UI
		"world":   map[string]any{"name": world.Name, "exists": world.Exists, "layout": world.Layout, "dimensions": found},
		"running": inst.Running(),
	}
	if pv := a.mapPluginView(r, inst, h); pv != nil {
		out["plugin"] = pv
	}
	out["mapPort"] = a.mapPortInfo(r)
	return out
}

// GET /api/servers/{id}/mc/map
func (a *API) mcMap(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	ok(w, a.mapInfo(r, inst, h))
	return nil
}

// POST /api/servers/{id}/mc/map/render {full, dims}
func (a *API) mcMapRender(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	var req struct {
		Full bool     `json:"full"`
		Dims []string `json:"dims"`
	}
	readJSON(r, &req)
	if inst.Running() {
		// write loaded chunks first, so the map shows the current state
		fctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		h.FlushWorld(fctx)
		cancel()
	}
	specs := a.dimSpecs(h, req.Dims)
	if len(specs) == 0 {
		if inst.Running() {
			return badRequest("Die Welt wurde noch nicht gespeichert. Versuch es in einer Minute noch einmal.")
		}
		return badRequest("Keine Weltdaten gefunden. Starte den Server einmal, damit eine Welt entsteht.")
	}
	if err := a.Maps.Render(inst.ID(), specs, req.Full); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "map.render", map[string]any{"full": req.Full})
	ok(w, a.Maps.Status(inst.ID()))
	return nil
}

// PUT /api/servers/{id}/mc/map/settings
func (a *API) mcMapSettings(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	var req minecraft.MapSettings
	if err := readJSON(r, &req); err != nil {
		return err
	}
	srv := inst.Server()
	s := minecraft.DecodeSettings(srv.Settings)
	req.Plugin, req.PluginPort = s.Map.Plugin, s.Map.PluginPort // managed via /map/plugin
	s.Map = req
	srv.Settings = s.Encode()
	if err := a.Servers.Update(r.Context(), srv); err != nil {
		return badRequest(err.Error())
	}
	a.audit(r, inst.ID(), "map.settings", nil)
	ok(w, a.mapInfo(r, inst, h))
	return nil
}

// GET /api/servers/{id}/mc/map/tiles/{dim}/{z}/{x}/{y}
func (a *API) mcMapTile(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	a.serveTile(w, r, inst.ID())
	return nil
}

func (a *API) serveTile(w http.ResponseWriter, r *http.Request, serverID string) {
	dim := r.PathValue("dim")
	z, e1 := strconv.Atoi(r.PathValue("z"))
	x, e2 := strconv.Atoi(r.PathValue("x"))
	y, e3 := strconv.Atoi(strings.TrimSuffix(r.PathValue("y"), ".png"))
	if e1 != nil || e2 != nil || e3 != nil || strings.ContainsAny(dim, `/\.`) || z > 0 || z < -10 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	f, err := os.Open(a.Maps.TilePath(serverID, dim, z, x, y))
	if err != nil {
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.Write(mapr.TransparentPNG)
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeContent(w, r, "tile.png", st.ModTime(), f)
}

// GET /api/servers/{id}/mc/map/players
func (a *API) mcMapPlayers(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set, h *minecraft.Handler) error {
	ok(w, h.Positions())
	return nil
}

// worldSpawn reads the spawn point from level.dat (best effort).
func worldSpawn(w *minecraft.WorldLayout) map[string]int {
	out := map[string]int{"x": 0, "z": 0}
	c, err := readLevelDat(filepath.Join(w.Root, "level.dat"))
	if err != nil {
		return out
	}
	data, _ := c["Data"].(map[string]any)
	if data == nil {
		return out
	}
	if x, ok := intOf(data["SpawnX"]); ok {
		out["x"] = int(x)
	}
	if z, ok := intOf(data["SpawnZ"]); ok {
		out["z"] = int(z)
	}
	// 26.1+: spawn stored as compound {pos: [x,y,z]}
	if sp, ok := data["spawn"].(map[string]any); ok {
		if pos, ok := sp["pos"].([]int32); ok && len(pos) == 3 {
			out["x"], out["z"] = int(pos[0]), int(pos[2])
		}
	}
	return out
}
