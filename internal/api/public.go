package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hearth/internal/minecraft"
	"hearth/internal/nbt"
	"hearth/internal/perms"
	"hearth/internal/servers"
	"hearth/internal/settings"
	"hearth/internal/store"
)

type PublicLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// PublicConfig controls the public status page of a server.
type PublicConfig struct {
	Title            string       `json:"title"`
	Description      string       `json:"description"`
	Accent           string       `json:"accent"`
	Address          string       `json:"address"`
	ShowAddress      bool         `json:"showAddress"`
	ShowOnline       bool         `json:"showOnline"`
	ShowLeaderboard  bool         `json:"showLeaderboard"`
	ShowDeaths       bool         `json:"showDeaths"`
	ShowAdvancements bool         `json:"showAdvancements"`
	ShowChat         bool         `json:"showChat"`
	ShowMap          bool         `json:"showMap"`
	ShowMapPlayers   bool         `json:"showMapPlayers"`
	ShowActivity     bool         `json:"showActivity"`
	ShowProfiles     bool         `json:"showProfiles"`
	ShowVersion      bool         `json:"showVersion"`
	HiddenPlayers    []string     `json:"hiddenPlayers"`
	Links            []PublicLink `json:"links"`
}

func defaultPublicConfig() PublicConfig {
	return PublicConfig{
		ShowAddress: true, ShowOnline: true, ShowLeaderboard: true, ShowDeaths: true, ShowAdvancements: true,
		ShowMap: true, ShowActivity: true, ShowProfiles: true, ShowVersion: true,
		HiddenPlayers: []string{}, Links: []PublicLink{},
	}
}

func decodePublic(raw json.RawMessage) PublicConfig {
	c := defaultPublicConfig()
	if len(raw) > 2 {
		json.Unmarshal(raw, &c)
	}
	if c.HiddenPlayers == nil {
		c.HiddenPlayers = []string{}
	}
	if c.Links == nil {
		c.Links = []PublicLink{}
	}
	return c
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}[a-z0-9]$`)

// GET /api/servers/{id}/public
func (a *API) getPublicConfig(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	srv := inst.Server()
	ok(w, map[string]any{"enabled": srv.PublicEnabled, "slug": srv.PublicSlug, "config": decodePublic(srv.PublicConfig),
		"url": a.PublicURL(r) + "/s/" + srv.PublicSlug, "globalEnabled": a.Settings.Bool(settings.PublicPagesEnabled)})
	return nil
}

// PUT /api/servers/{id}/public {enabled, slug, config}
func (a *API) setPublicConfig(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	var req struct {
		Enabled bool         `json:"enabled"`
		Slug    string       `json:"slug"`
		Config  PublicConfig `json:"config"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	srv := inst.Server()
	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if req.Enabled || slug != "" {
		if !slugRe.MatchString(slug) {
			return badRequest("Die Adresse darf 3–42 Zeichen haben: Kleinbuchstaben, Zahlen und Bindestriche")
		}
		if other, err := a.Store.GetServerBySlug(r.Context(), slug); err == nil && other.ID != srv.ID {
			return badRequest("Diese Adresse ist bereits vergeben")
		}
	}
	c := req.Config
	c.Title = truncate(strings.TrimSpace(c.Title), 80)
	c.Description = truncate(strings.TrimSpace(c.Description), 2000)
	c.Address = truncate(strings.TrimSpace(c.Address), 120)
	if c.Accent != "" && !colorRe.MatchString(c.Accent) {
		return badRequest("Ungültige Akzentfarbe")
	}
	var links []PublicLink
	for _, l := range c.Links {
		l.Label = truncate(strings.TrimSpace(l.Label), 40)
		l.URL = strings.TrimSpace(l.URL)
		if l.Label == "" || !(strings.HasPrefix(l.URL, "https://") || strings.HasPrefix(l.URL, "http://")) {
			continue
		}
		links = append(links, l)
		if len(links) >= 8 {
			break
		}
	}
	c.Links = links
	var hidden []string
	for _, h := range c.HiddenPlayers {
		if n := minecraft.NormalizeUUID(h); n != "" {
			hidden = append(hidden, n)
		}
	}
	c.HiddenPlayers = hidden
	srv.PublicEnabled = req.Enabled
	srv.PublicSlug = slug
	srv.PublicConfig, _ = json.Marshal(c)
	if err := a.Servers.Update(r.Context(), srv); err != nil {
		if store.IsUniqueViolation(err) {
			return badRequest("Diese Adresse ist bereits vergeben")
		}
		return err
	}
	a.audit(r, srv.ID, "public.update", map[string]any{"enabled": req.Enabled, "slug": slug})
	ok(w, map[string]any{"enabled": srv.PublicEnabled, "slug": srv.PublicSlug, "config": decodePublic(srv.PublicConfig),
		"url": a.PublicURL(r) + "/s/" + srv.PublicSlug})
	return nil
}

// ---- public (unauthenticated) endpoints ----

type publicCtx struct {
	inst *servers.Instance
	srv  *store.Server
	cfg  PublicConfig
	mc   *minecraft.Handler
}

func (a *API) loadPublic(r *http.Request) (*publicCtx, error) {
	if !a.Settings.Bool(settings.PublicPagesEnabled) {
		return nil, errNotFound
	}
	srv, err := a.Store.GetServerBySlug(r.Context(), strings.ToLower(r.PathValue("slug")))
	if err != nil || !srv.PublicEnabled {
		return nil, errNotFound
	}
	inst := a.Servers.Get(srv.ID)
	if inst == nil {
		return nil, errNotFound
	}
	pc := &publicCtx{inst: inst, srv: inst.Server(), cfg: decodePublic(srv.PublicConfig)}
	pc.mc, _ = inst.Handler().(*minecraft.Handler)
	return pc, nil
}

// flavor is the color of the page: its own choice, else the server's.
func (pc *publicCtx) flavor() flavor {
	if pc.cfg.Accent != "" {
		return flavorFor(pc.cfg.Accent, "")
	}
	return flavorFor(pc.srv.Color, pc.srv.ID)
}

func (pc *publicCtx) hidden(uuid string) bool {
	for _, h := range pc.cfg.HiddenPlayers {
		if strings.EqualFold(h, uuid) {
			return true
		}
	}
	return false
}

type publicFn func(w http.ResponseWriter, r *http.Request, pc *publicCtx) error

func (a *API) public(fn publicFn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pc, err := a.loadPublic(r)
		if err == nil {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			err = fn(w, r, pc)
		}
		if err != nil {
			a.fail(w, r, err)
		}
	}
}

// address is what players type into Minecraft. Without a configured address
// the host name of the request plus the game port is a good guess.
func (pc *publicCtx) address(r *http.Request) string {
	if pc.cfg.Address != "" {
		return pc.cfg.Address
	}
	if pc.srv.DisplayAddress != "" {
		return pc.srv.DisplayAddress
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") { // IPv6 literal
		host = "[" + host + "]"
	}
	if pc.srv.Port == 25565 {
		return host
	}
	return host + ":" + strconv.Itoa(pc.srv.Port)
}

// GET /api/public/{slug}
func (a *API) publicInfo(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	title := pc.cfg.Title
	if title == "" {
		title = pc.srv.Name
	}
	fl := pc.flavor()
	server := map[string]any{
		"name": title, "description": pc.cfg.Description, "accent": pc.cfg.Accent, "color": fl.Color, "links": pc.cfg.Links,
		"game": pc.srv.Game, "panelName": a.Settings.Get(settings.PanelName),
	}
	if pc.cfg.ShowAddress {
		server["address"] = pc.address(r)
	}
	if _, err := os.Stat(filepath.Join(pc.inst.Dir(), "server-icon.png")); err == nil {
		server["hasIcon"] = true
	}
	status := map[string]any{"online": pc.inst.Running(), "state": pc.inst.State()}
	out := map[string]any{"server": server, "status": status, "sections": pc.cfg}
	if pc.mc != nil {
		s := pc.mc.Settings()
		if pc.cfg.ShowVersion {
			server["version"] = s.Version
			server["flavor"] = s.Flavor
		}
		if pc.inst.Running() {
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			if p, err := pc.mc.Ping(ctx); err == nil {
				// the ping is cached for a few seconds; the tracked player list is newer
				tracked, _ := pc.mc.PlayerCount()
				status["motd"] = p.MOTD
				status["players"] = max(p.Players.Online, tracked)
				status["maxPlayers"] = p.Players.Max
				status["latencyMs"] = p.LatencyMS
				status["versionName"] = p.Version.Name
				status["hasIcon"] = p.Favicon != ""
			} else {
				online, maxP := pc.mc.PlayerCount()
				status["players"], status["maxPlayers"] = online, maxP
			}
			cancel()
			if pc.cfg.ShowOnline {
				list := []minecraft.OnlinePlayer{}
				for _, p := range pc.mc.OnlinePlayers() {
					if !pc.hidden(p.UUID) {
						list = append(list, p)
					}
				}
				out["online"] = list
			}
		}
		totals, _ := a.Store.MCTotals(r.Context(), pc.srv.ID)
		out["totals"] = totals
		if pc.cfg.ShowMap {
			s := pc.mc.Settings()
			m := map[string]any{"enabled": s.Map.Enabled, "externalUrl": s.Map.ExternalURL}
			if st := pc.mc.MapPlugin(); st != nil && st.File != "" && st.Enabled {
				if base := a.mapBaseURL(r); base != "" {
					m["pluginUrl"], m["pluginName"] = base+"/"+pc.srv.ID+"/", st.Name
				}
			}
			out["map"] = m
		}
	}
	out["startedAt"] = pc.inst.Status().StartedAt
	ok(w, out)
	return nil
}

// GET /api/public/{slug}/players
func (a *API) publicPlayers(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	if !pc.cfg.ShowLeaderboard || pc.mc == nil {
		return errNotFound
	}
	sort := r.URL.Query().Get("sort")
	if !store.ValidMCSort(sort) {
		sort = "playtime"
	}
	list, total, err := a.Store.ListMCPlayers(r.Context(), pc.srv.ID, store.MCPlayerQuery{
		Sort: sort, Asc: r.URL.Query().Get("dir") == "asc", Search: r.URL.Query().Get("q"),
		Limit: min(100, queryInt(r, "limit", 25)), Offset: queryInt(r, "offset", 0),
	})
	if err != nil {
		return err
	}
	online := map[string]bool{}
	for _, p := range pc.mc.OnlinePlayers() {
		online[p.UUID] = true
	}
	out := []map[string]any{}
	for _, p := range list {
		if pc.hidden(p.UUID) {
			continue
		}
		out = append(out, map[string]any{
			"uuid": p.UUID, "name": p.Name, "playSeconds": p.PlaySeconds(), "deaths": p.Deaths, "mobKills": p.MobKills,
			"playerKills": p.PlayerKills, "blocksMined": p.BlocksMined, "distanceCm": p.DistanceCM, "jumps": p.Jumps,
			"advancements": p.Advancements, "fishCaught": p.FishCaught, "itemsCrafted": p.ItemsCrafted,
			"damageDealt": p.DamageDealt, "lastSeen": p.LastSeen, "firstSeen": p.FirstSeen, "online": online[p.UUID],
		})
	}
	ok(w, map[string]any{"players": out, "total": total})
	return nil
}

// GET /api/public/{slug}/players/{uuid}
func (a *API) publicPlayer(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	if !pc.cfg.ShowProfiles || pc.mc == nil {
		return errNotFound
	}
	uuid := minecraft.NormalizeUUID(r.PathValue("uuid"))
	if uuid == "" || pc.hidden(uuid) {
		return errNotFound
	}
	d, err := a.playerDetail(r.Context(), pc.srv.ID, uuid, pc.cfg.ShowChat)
	if err != nil {
		return err
	}
	// filter event types by page settings
	if evs, ok := d["events"].([]*store.MCEvent); ok {
		var keep []*store.MCEvent
		for _, e := range evs {
			if (e.Type == "death" && pc.cfg.ShowDeaths) || (e.Type == "advancement" && pc.cfg.ShowAdvancements) || (e.Type == "chat" && pc.cfg.ShowChat) {
				keep = append(keep, e)
			}
		}
		if keep == nil {
			keep = []*store.MCEvent{}
		}
		d["events"] = keep
	}
	delete(d, "sessions")
	for _, p := range pc.mc.OnlinePlayers() {
		if p.UUID == uuid {
			d["online"] = true
			d["onlineSince"] = p.Since
		}
	}
	ok(w, d)
	return nil
}

// GET /api/public/{slug}/events?types=death,advancement
func (a *API) publicEvents(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	allowed := map[string]bool{"death": pc.cfg.ShowDeaths, "advancement": pc.cfg.ShowAdvancements, "chat": pc.cfg.ShowChat}
	var types []string
	for _, t := range strings.Split(r.URL.Query().Get("types"), ",") {
		if allowed[t] {
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		ok(w, map[string]any{"events": []any{}})
		return nil
	}
	q := store.MCEventQuery{Types: types, Limit: min(100, queryInt(r, "limit", 30))}
	q.Before, _ = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	list, err := a.Store.ListMCEvents(r.Context(), pc.srv.ID, q)
	if err != nil {
		return err
	}
	out := []*store.MCEvent{}
	for _, e := range list {
		if !pc.hidden(e.UUID) {
			out = append(out, e)
		}
	}
	causes, _ := a.Store.MCDeathCauses(r.Context(), pc.srv.ID, "", time.Now().AddDate(0, 0, -30).Unix())
	ok(w, map[string]any{"events": out, "causeLabels": minecraft.DeathCauses, "deathCauses30d": causes})
	return nil
}

// GET /api/public/{slug}/activity?range=24h|7d|30d
func (a *API) publicActivity(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	if !pc.cfg.ShowActivity {
		return errNotFound
	}
	from, bucket := metricRange(r.URL.Query().Get("range"))
	pts, err := a.Store.Metrics(r.Context(), pc.srv.ID, from, bucket)
	if err != nil {
		return err
	}
	out := make([]map[string]any, 0, len(pts))
	for _, p := range pts {
		out = append(out, map[string]any{"t": p.At, "players": max(p.Players, 0)})
	}
	ok(w, map[string]any{"points": out, "bucket": bucket})
	return nil
}

// GET /api/public/{slug}/map
func (a *API) publicMap(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	if !pc.cfg.ShowMap || pc.mc == nil {
		return errNotFound
	}
	full := a.mapInfo(r, pc.inst, pc.mc)
	ms := pc.mc.Settings().Map
	info := map[string]any{
		"dimensions": full["dimensions"], "spawn": full["spawn"], "showPlayers": pc.cfg.ShowMapPlayers,
		"settings": map[string]any{"enabled": ms.Enabled, "livePlayers": ms.LivePlayers, "externalUrl": ms.ExternalURL},
	}
	// only what visitors need: no ports, files or folders
	if pv, ok := full["plugin"].(map[string]any); ok && pv["url"] != nil && pv["file"] != "" {
		info["plugin"] = map[string]any{"id": pv["id"], "name": pv["name"], "url": pv["url"], "listening": pv["listening"] == true}
	}
	ok(w, info)
	return nil
}

// GET /api/public/{slug}/map/tiles/{dim}/{z}/{x}/{y}
func (a *API) publicTile(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	if !pc.cfg.ShowMap {
		return errNotFound
	}
	a.serveTile(w, r, pc.srv.ID)
	return nil
}

// GET /api/public/{slug}/map/players
func (a *API) publicMapPlayers(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	if !pc.cfg.ShowMap || !pc.cfg.ShowMapPlayers || pc.mc == nil {
		ok(w, []any{})
		return nil
	}
	out := []minecraft.Position{}
	for _, p := range pc.mc.Positions() {
		if !pc.hidden(p.UUID) {
			out = append(out, p)
		}
	}
	ok(w, out)
	return nil
}

// GET /api/public/{slug}/icon.png
func (a *API) publicIcon(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	serveIcon(w, r, pc.inst.Dir())
	return nil
}

// GET /api/public/{slug}/badge.svg — status badge for websites and READMEs.
func (a *API) publicBadge(w http.ResponseWriter, r *http.Request, pc *publicCtx) error {
	label := pc.cfg.Title
	if label == "" {
		label = pc.srv.Name
	}
	fl := pc.flavor()
	value, col, ink := "offline", "#5b6168", "#ffffff"
	if pc.inst.Running() && pc.mc != nil {
		online, maxP := pc.mc.PlayerCount()
		value, col, ink = fmt.Sprintf("%d/%d online", online, maxP), fl.Color, "#101214"
	} else if pc.inst.Running() {
		value, col, ink = "online", fl.Color, "#101214"
	}
	textW := func(s string) int { return 7*len([]rune(s)) + 14 }
	lw, vw := textW(label), textW(value)
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">
<clipPath id="r"><rect width="%d" height="20" rx="10" fill="#fff"/></clipPath>
<g clip-path="url(#r)"><rect width="%d" height="20" fill="%s"/><rect x="%d" width="%d" height="20" fill="%s"/></g>
<g text-anchor="middle" font-family="Verdana,DejaVu Sans,sans-serif" font-size="11" font-weight="bold">
<text x="%d" y="14" fill="#ffffff">%s</text><text x="%d" y="14" fill="%s">%s</text></g></svg>`,
		lw+vw, html.EscapeString(label), value, lw+vw, lw, fl.BG, lw, vw, col,
		lw/2, html.EscapeString(label), lw+vw/2, ink, html.EscapeString(value))
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Write([]byte(svg))
	return nil
}

// GET /api/heads/{uuid}?size=
func (a *API) head(w http.ResponseWriter, r *http.Request) {
	uuid := strings.TrimSuffix(r.PathValue("uuid"), ".png")
	size := queryInt(r, "size", 64)
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	b, err := a.MC.Heads.PNG(ctx, uuid, size)
	if err != nil {
		http.Error(w, "error", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(b)
}

// PublicPageMeta renders Open Graph tags for /s/{slug} (link previews in Discord etc.).
func (a *API) PublicPageMeta(r *http.Request, slug string) string {
	if !a.Settings.Bool(settings.PublicPagesEnabled) {
		return ""
	}
	srv, err := a.Store.GetServerBySlug(r.Context(), strings.ToLower(slug))
	if err != nil || !srv.PublicEnabled {
		return ""
	}
	cfg := decodePublic(srv.PublicConfig)
	title := cfg.Title
	if title == "" {
		title = srv.Name
	}
	desc := cfg.Description
	inst := a.Servers.Get(srv.ID)
	if desc == "" && inst != nil {
		desc = "Minecraft-Server"
		if h, ok := inst.Handler().(*minecraft.Handler); ok && inst.Running() {
			online, maxP := h.PlayerCount()
			desc = fmt.Sprintf("Online mit %d von %d Spielern", online, maxP)
		}
	}
	if len([]rune(desc)) > 200 {
		desc = string([]rune(desc)[:200]) + "…"
	}
	base := a.PublicURL(r)
	var fl flavor
	if cfg.Accent != "" {
		fl = flavorFor(cfg.Accent, "")
	} else {
		fl = flavorFor(srv.Color, srv.ID)
	}
	accent := fl.BG
	e := html.EscapeString
	return fmt.Sprintf(`<title>%s</title>
<meta name="description" content="%s">
<meta property="og:type" content="website">
<meta property="og:title" content="%s">
<meta property="og:description" content="%s">
<meta property="og:url" content="%s">
<meta property="og:image" content="%s">
<meta name="twitter:card" content="summary">
<meta name="theme-color" content="%s">`,
		e(title), e(desc), e(title), e(desc), e(base+"/s/"+srv.PublicSlug), e(base+"/api/public/"+srv.PublicSlug+"/icon.png"), e(accent))
}

// ---- helpers ----

func readLevelDat(path string) (map[string]any, error) {
	return nbt.ReadFile(path, map[string]bool{"Player": true, "WorldGenSettings": true, "DragonFight": true,
		"CustomBossEvents": true, "DataPacks": true, "GameRules": true, "ScheduledEvents": true, "ServerBrands": true})
}

func intOf(v any) (int64, bool) { return nbt.Int(v) }
