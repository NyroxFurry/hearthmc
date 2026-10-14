package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hearth/internal/events"
	"hearth/internal/minecraft"
	"hearth/internal/notify"
	"hearth/internal/perms"
	"hearth/internal/servers"
	"hearth/internal/settings"
	"hearth/internal/store"
)

func eventTask(kind, msg string) events.Event {
	return events.Event{Type: events.Task, Data: map[string]string{"kind": kind, "message": msg}}
}

// serverView is the JSON representation of a server for the UI.
type serverView struct {
	*store.Server
	Status      servers.Status `json:"status"`
	Permissions []string       `json:"permissions"`
	IsOwner     bool           `json:"isOwner"`
	OwnerName   string         `json:"ownerName"`
	GameName    string         `json:"gameName"`
	PublicURL   string         `json:"publicUrl,omitempty"`
	HasIcon     bool           `json:"hasIcon"`
}

// sanitizeSettings removes secrets from game settings.
func sanitizeSettings(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	delete(m, "rconPassword")
	b, _ := json.Marshal(m)
	return b
}

func (a *API) view(r *http.Request, inst *servers.Instance, ps *perms.Set, names map[int64]string) serverView {
	srv := inst.Server()
	srv.Settings = sanitizeSettings(srv.Settings)
	v := serverView{Server: srv, Status: inst.Status(), Permissions: ps.List(), IsOwner: ps.Owner}
	if names != nil {
		v.OwnerName = names[srv.OwnerID]
	}
	if g, ok := a.Servers.Game(srv.Game); ok {
		v.GameName = g.Name()
	}
	if srv.PublicEnabled && srv.PublicSlug != "" {
		v.PublicURL = a.PublicURL(r) + "/s/" + srv.PublicSlug
	}
	if _, err := os.Stat(filepath.Join(inst.Dir(), "server-icon.png")); err == nil {
		v.HasIcon = true
	}
	return v
}

// GET /api/servers
func (a *API) listServers(w http.ResponseWriter, r *http.Request, u *store.User) error {
	names, _ := a.Store.UserNames(r.Context())
	member, _ := a.Store.MemberServerPerms(r.Context(), u.ID)
	out := []serverView{}
	for _, inst := range a.Servers.List() {
		srv := inst.Server()
		var ps *perms.Set
		switch {
		case u.IsAdmin:
			ps = perms.Full(srv.OwnerID == u.ID)
		case srv.OwnerID == u.ID:
			ps = perms.Full(true)
		default:
			p, ok := member[srv.ID]
			if !ok {
				continue
			}
			ps = perms.FromList(p)
		}
		out = append(out, a.view(r, inst, ps, names))
	}
	ok(w, out)
	return nil
}

// GET /api/servers/{id}
func (a *API) getServer(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	names, _ := a.Store.UserNames(r.Context())
	ok(w, a.view(r, inst, ps, names))
	return nil
}

type createReq struct {
	Name              string           `json:"name"`
	Description       string           `json:"description"`
	Game              string           `json:"game"`
	Runtime           string           `json:"runtime"`
	MemoryMB          int              `json:"memoryMb"`
	CPULimit          float64          `json:"cpuLimit"`
	DiskLimitMB       int              `json:"diskLimitMb"`
	Port              int              `json:"port"`
	OwnerID           int64            `json:"ownerId"`
	Autostart         bool             `json:"autostart"`
	AutoRestart       *bool            `json:"autoRestart"`
	DockerImage       string           `json:"dockerImage"`
	Color             string           `json:"color"`
	DisplayAddress    string           `json:"displayAddress"`
	Settings          json.RawMessage  `json:"settings"`
	ExtraPorts        []store.PortSpec `json:"extraPorts"`
	StartAfterInstall bool             `json:"startAfterInstall"`
}

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// POST /api/servers
func (a *API) createServer(w http.ResponseWriter, r *http.Request, u *store.User) error {
	var req createReq
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if !u.IsAdmin && !u.CanCreateServers {
		return errForbidden
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 60 {
		return badRequest("Bitte einen Namen mit 1–60 Zeichen angeben")
	}
	if _, ok := a.Servers.Game(req.Game); !ok {
		return badRequest("Unbekannter Spieltyp")
	}
	if req.Game != "minecraft" && !u.IsAdmin {
		return badRequest("Nur Administratoren können Server mit eigenem Startbefehl anlegen")
	}
	if req.Runtime == "" {
		req.Runtime = a.Settings.Get(settings.DefaultRuntime)
	}
	if req.Runtime != "native" && req.Runtime != "docker" {
		return badRequest("Laufzeit muss native oder docker sein")
	}
	if req.Runtime == "docker" {
		if err := a.Servers.Docker().Check(r.Context()); err != nil {
			return badRequest(err.Error())
		}
	}
	if req.MemoryMB <= 0 {
		req.MemoryMB = a.Settings.Int(settings.DefaultMemoryMB)
	}
	if req.MemoryMB < 256 {
		return badRequest("Mindestens 256 MB Arbeitsspeicher")
	}
	owner := u.ID
	if u.IsAdmin && req.OwnerID != 0 {
		if _, err := a.Store.GetUser(r.Context(), req.OwnerID); err != nil {
			return badRequest("Besitzer nicht gefunden")
		}
		owner = req.OwnerID
	}
	if !u.IsAdmin {
		// quotas
		count, memSum := 0, 0
		for _, inst := range a.Servers.List() {
			s := inst.Server()
			if s.OwnerID == u.ID {
				count++
				memSum += s.MemoryMB
			}
		}
		if u.MaxServers > 0 && count >= u.MaxServers {
			return badRequest(fmt.Sprintf("Du kannst maximal %d Server besitzen", u.MaxServers))
		}
		if u.MaxMemoryMB > 0 && memSum+req.MemoryMB > u.MaxMemoryMB {
			return badRequest(fmt.Sprintf("Dein Speicherkontingent (%d MB) reicht nicht aus", u.MaxMemoryMB))
		}
		// non-admins cannot pick arbitrary images, limits or ports
		req.DockerImage, req.CPULimit, req.DiskLimitMB, req.ExtraPorts = "", 0, 0, nil
	}
	if req.Color != "" && !colorRe.MatchString(req.Color) {
		req.Color = ""
	}
	ctx := r.Context()
	port, err := a.Servers.AllocatePort(ctx, req.Port, false)
	if err != nil {
		return badRequest(err.Error())
	}
	if req.Port > 0 && port != req.Port {
		return badRequest(fmt.Sprintf("Port %d ist belegt", req.Port))
	}
	srv := &store.Server{
		Name: req.Name, Description: truncate(strings.TrimSpace(req.Description), 500), Game: req.Game,
		OwnerID: owner, Runtime: req.Runtime, MemoryMB: req.MemoryMB, CPULimit: req.CPULimit,
		DiskLimitMB: req.DiskLimitMB, Port: port, Autostart: req.Autostart, AutoRestart: true,
		DockerImage: strings.TrimSpace(req.DockerImage), Settings: req.Settings, Color: req.Color,
		DisplayAddress: truncate(strings.TrimSpace(req.DisplayAddress), 120), ExtraPorts: req.ExtraPorts,
	}
	if req.AutoRestart != nil {
		srv.AutoRestart = *req.AutoRestart
	}
	if srv.Settings == nil {
		srv.Settings = json.RawMessage("{}")
	}
	if srv.Game == "minecraft" {
		rcon, err := a.Servers.AllocatePort(ctx, 0, true, port)
		if err != nil {
			return badRequest(err.Error())
		}
		var s map[string]any
		json.Unmarshal(srv.Settings, &s)
		if s == nil {
			s = map[string]any{}
		}
		s["rconPort"] = rcon
		if m, ok := s["map"].(map[string]any); ok {
			delete(m, "pluginPort")
			if p, _ := m["plugin"].(string); p != "" {
				mp, err := a.Servers.AllocatePort(ctx, 0, true, port, rcon)
				if err != nil {
					return badRequest(err.Error())
				}
				m["pluginPort"] = mp
			}
		}
		if !u.IsAdmin {
			delete(s, "startupOverride")
			if j, _ := s["java"].(string); j != "" && j != "auto" && j != "system" {
				s["java"] = "auto"
			}
		}
		srv.Settings, _ = json.Marshal(s)
		srv.PublicConfig, _ = json.Marshal(defaultPublicConfig())
	}
	inst, err := a.Servers.Create(ctx, srv, req.StartAfterInstall)
	if err != nil {
		return badRequest(err.Error())
	}
	a.audit(r, srv.ID, "server.create", map[string]any{"name": srv.Name, "game": srv.Game, "settings": json.RawMessage(sanitizeSettings(srv.Settings))})
	names, _ := a.Store.UserNames(ctx)
	ok(w, a.view(r, inst, a.permsFor(ctx, u, inst.Server()), names))
	return nil
}

// PATCH /api/servers/{id}
func (a *API) updateServer(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	var req map[string]json.RawMessage
	if err := readJSON(r, &req); err != nil {
		return err
	}
	srv := inst.Server()
	changed := []string{}
	str := func(key string, max int, dst *string) error {
		raw, ok := req[key]
		if !ok {
			return nil
		}
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return badRequest("Ungültiger Wert für " + key)
		}
		v = strings.TrimSpace(v)
		if len([]rune(v)) > max {
			return badRequest(key + " ist zu lang")
		}
		*dst = v
		changed = append(changed, key)
		return nil
	}
	boolean := func(key string, dst *bool) error {
		raw, ok := req[key]
		if !ok {
			return nil
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return badRequest("Ungültiger Wert für " + key)
		}
		changed = append(changed, key)
		return nil
	}
	canEdit := ps.Has(perms.SettingsEdit)
	isAdmin := u.IsAdmin
	needs := func(key string, allowed bool) error {
		if _, ok := req[key]; ok && !allowed {
			return errForbidden
		}
		return nil
	}
	for _, k := range []string{"name", "description", "color", "displayAddress", "autostart", "autoRestart", "settings"} {
		if err := needs(k, canEdit); err != nil {
			return err
		}
	}
	ownerQuota := ps.Owner && u.CanCreateServers
	if err := needs("memoryMb", isAdmin || ownerQuota); err != nil {
		return err
	}
	for _, k := range []string{"cpuLimit", "diskLimitMb", "port", "extraPorts", "runtime", "dockerImage", "ownerId", "suspended"} {
		if err := needs(k, isAdmin); err != nil {
			return err
		}
	}
	if err := str("name", 60, &srv.Name); err != nil {
		return err
	}
	if srv.Name == "" {
		return badRequest("Name darf nicht leer sein")
	}
	if err := str("description", 500, &srv.Description); err != nil {
		return err
	}
	if err := str("color", 7, &srv.Color); err != nil {
		return err
	}
	if srv.Color != "" && !colorRe.MatchString(srv.Color) {
		return badRequest("Ungültige Farbe")
	}
	if err := str("displayAddress", 120, &srv.DisplayAddress); err != nil {
		return err
	}
	if err := boolean("autostart", &srv.Autostart); err != nil {
		return err
	}
	if err := boolean("autoRestart", &srv.AutoRestart); err != nil {
		return err
	}
	if err := boolean("suspended", &srv.Suspended); err != nil {
		return err
	}
	if raw, ok := req["memoryMb"]; ok {
		var mb int
		if json.Unmarshal(raw, &mb) != nil || mb < 256 {
			return badRequest("Mindestens 256 MB Arbeitsspeicher")
		}
		if !isAdmin && u.MaxMemoryMB > 0 {
			sum := 0
			for _, other := range a.Servers.List() {
				s := other.Server()
				if s.OwnerID == u.ID && s.ID != srv.ID {
					sum += s.MemoryMB
				}
			}
			if sum+mb > u.MaxMemoryMB {
				return badRequest(fmt.Sprintf("Dein Speicherkontingent (%d MB) reicht nicht aus", u.MaxMemoryMB))
			}
		}
		srv.MemoryMB = mb
		changed = append(changed, "memoryMb")
	}
	if raw, ok := req["cpuLimit"]; ok {
		if json.Unmarshal(raw, &srv.CPULimit) != nil || srv.CPULimit < 0 {
			return badRequest("Ungültiges CPU-Limit")
		}
		changed = append(changed, "cpuLimit")
	}
	if raw, ok := req["diskLimitMb"]; ok {
		if json.Unmarshal(raw, &srv.DiskLimitMB) != nil || srv.DiskLimitMB < 0 {
			return badRequest("Ungültiges Speicherlimit")
		}
		changed = append(changed, "diskLimitMb")
	}
	if raw, ok := req["port"]; ok {
		var p int
		if json.Unmarshal(raw, &p) != nil || p < 1 || p > 65535 {
			return badRequest("Ungültiger Port")
		}
		if p != srv.Port {
			if used, _ := a.Servers.PortInUse(r.Context(), p, srv.ID); used {
				return badRequest(fmt.Sprintf("Port %d wird bereits von einem anderen Server genutzt", p))
			}
			srv.Port = p
			changed = append(changed, "port")
		}
	}
	if raw, ok := req["extraPorts"]; ok {
		var ep []store.PortSpec
		if json.Unmarshal(raw, &ep) != nil {
			return badRequest("Ungültige Zusatzports")
		}
		for _, p := range ep {
			if p.Port < 1 || p.Port > 65535 {
				return badRequest("Ungültiger Zusatzport")
			}
			if used, _ := a.Servers.PortInUse(r.Context(), p.Port, srv.ID); used {
				return badRequest(fmt.Sprintf("Port %d wird bereits genutzt", p.Port))
			}
		}
		srv.ExtraPorts = ep
		changed = append(changed, "extraPorts")
	}
	if raw, ok := req["runtime"]; ok {
		var rt string
		json.Unmarshal(raw, &rt)
		if rt != "native" && rt != "docker" {
			return badRequest("Laufzeit muss native oder docker sein")
		}
		if rt != srv.Runtime && inst.State() != "offline" {
			return badRequest("Die Laufzeit kann nur bei gestopptem Server geändert werden")
		}
		srv.Runtime = rt
		changed = append(changed, "runtime")
	}
	if err := str("dockerImage", 200, &srv.DockerImage); err != nil {
		return err
	}
	if raw, ok := req["ownerId"]; ok {
		var id int64
		json.Unmarshal(raw, &id)
		if _, err := a.Store.GetUser(r.Context(), id); err != nil {
			return badRequest("Benutzer nicht gefunden")
		}
		srv.OwnerID = id
		a.Store.DeleteMember(r.Context(), srv.ID, id)
		changed = append(changed, "ownerId")
	}
	if raw, ok := req["settings"]; ok {
		// merge partial game settings; protected keys need admin rights
		var cur, patch map[string]any
		json.Unmarshal(srv.Settings, &cur)
		if cur == nil {
			cur = map[string]any{}
		}
		if err := json.Unmarshal(raw, &patch); err != nil {
			return badRequest("Ungültige Einstellungen")
		}
		protected := map[string]bool{"rconPort": true, "rconPassword": true, "installedVersion": true,
			"installedBuild": true, "loaderArgsFile": true, "flavor": true, "version": true, "build": true, "jar": true}
		adminOnly := map[string]bool{"startupOverride": true, "manageRcon": true}
		if srv.Game != "minecraft" {
			// the start command and environment of custom servers run directly on the host
			adminOnly["command"], adminOnly["env"] = true, true
		}
		for k, v := range patch {
			if protected[k] {
				continue
			}
			unchanged := reflect.DeepEqual(cur[k], v)
			if adminOnly[k] && !isAdmin && !unchanged {
				return errForbidden
			}
			if k == "java" && !isAdmin && !unchanged {
				if s, _ := v.(string); s != "auto" && s != "system" {
					return errForbidden
				}
			}
			if k == "jar" && srv.Game == "minecraft" {
				continue
			}
			if k == "map" {
				// the map plugin and its internal port are managed by the panel
				v = keepMapPlugin(cur["map"], v)
			}
			cur[k] = v
		}
		srv.Settings, _ = json.Marshal(cur)
	}
	if err := a.Servers.Update(r.Context(), srv); err != nil {
		return badRequest(err.Error())
	}
	a.audit(r, srv.ID, "server.update", map[string]any{"fields": changed})
	names, _ := a.Store.UserNames(r.Context())
	ok(w, a.view(r, inst, ps, names))
	return nil
}

// DELETE /api/servers/{id}
func (a *API) deleteServer(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	if !u.IsAdmin && !ps.Owner {
		return errForbidden
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	readJSON(r, &req)
	srv := inst.Server()
	if req.Confirm != srv.Name {
		return badRequest("Zur Bestätigung den Servernamen eingeben")
	}
	a.Maps.Clear(srv.ID)
	if err := a.Servers.Delete(r.Context(), srv.ID); err != nil {
		return err
	}
	a.audit(r, srv.ID, "server.delete", map[string]any{"name": srv.Name})
	ok(w, nil)
	return nil
}

// POST /api/servers/{id}/power
func (a *API) power(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	var req struct {
		Action string `json:"action"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	var err error
	switch req.Action {
	case "start":
		if !ps.Has(perms.PowerStart) {
			return errForbidden
		}
		if inst.Server().Suspended && !u.IsAdmin {
			return servers.ErrSuspended
		}
		err = inst.Start(u.Username)
	case "stop":
		if !ps.Has(perms.PowerStop) {
			return errForbidden
		}
		err = inst.Stop(u.Username, false)
	case "restart":
		if !ps.Has(perms.PowerStop) {
			return errForbidden
		}
		if inst.State() == "offline" {
			err = inst.Start(u.Username)
		} else {
			err = inst.Stop(u.Username, true)
		}
	case "kill":
		if !ps.Has(perms.PowerKill) {
			return errForbidden
		}
		err = inst.Kill(u.Username)
	default:
		return badRequest("Unbekannte Aktion")
	}
	if err != nil {
		return err
	}
	a.audit(r, inst.ID(), "server.power", map[string]any{"action": req.Action})
	ok(w, inst.Status())
	return nil
}

// POST /api/servers/{id}/command
func (a *API) command(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	var req struct {
		Command string `json:"command"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if len(req.Command) > 2000 {
		return badRequest("Befehl zu lang")
	}
	if err := inst.Command(req.Command, u.Username); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "server.command", map[string]any{"command": req.Command})
	ok(w, nil)
	return nil
}

// GET /api/servers/{id}/console
func (a *API) consoleHistory(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	ok(w, map[string]any{"lines": inst.ConsoleLines(queryInt(r, "lines", 1000))})
	return nil
}

// GET /api/servers/{id}/metrics?range=1h|6h|24h|7d|30d
func (a *API) metrics(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	from, bucket := metricRange(r.URL.Query().Get("range"))
	pts, err := a.Store.Metrics(r.Context(), inst.ID(), from, bucket)
	if err != nil {
		return err
	}
	if !ps.Has(perms.ConsoleRead) && !ps.Has(perms.SettingsRead) {
		for i := range pts {
			pts[i].CPU, pts[i].Mem = 0, 0
		}
	}
	ok(w, map[string]any{"points": pts, "bucket": bucket})
	return nil
}

func metricRange(rng string) (int64, int64) {
	now := time.Now().Unix()
	switch rng {
	case "6h":
		return now - 6*3600, 300
	case "24h":
		return now - 86400, 900
	case "7d":
		return now - 7*86400, 3600
	case "30d":
		return now - 30*86400, 6 * 3600
	default:
		return now - 3600, 60
	}
}

// POST /api/servers/{id}/reinstall
func (a *API) reinstall(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	if err := inst.Install(u.Username, false); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "server.reinstall", nil)
	ok(w, nil)
	return nil
}

// ---- members ----

// GET /api/servers/{id}/members
func (a *API) members(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	list, err := a.Store.ListMembers(r.Context(), inst.ID())
	if err != nil {
		return err
	}
	ok(w, map[string]any{"members": list, "permissions": perms.All, "presets": perms.Presets})
	return nil
}

// POST /api/servers/{id}/members  {username, permissions}
func (a *API) addMember(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	var req struct {
		Username    string   `json:"username"`
		UserID      int64    `json:"userId"`
		Permissions []string `json:"permissions"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	var target *store.User
	var err error
	if req.UserID != 0 {
		target, err = a.Store.GetUser(r.Context(), req.UserID)
	} else {
		target, err = a.Store.GetUserByName(r.Context(), strings.TrimSpace(req.Username))
	}
	if err != nil {
		return badRequest("Benutzer nicht gefunden")
	}
	srv := inst.Server()
	if target.ID == srv.OwnerID {
		return badRequest("Der Besitzer hat bereits alle Rechte")
	}
	p := perms.Sanitize(req.Permissions)
	if !ps.Subset(p) {
		return badRequest("Du kannst nur Rechte vergeben, die du selbst hast")
	}
	if err := a.Store.UpsertMember(r.Context(), a.Store.DB, srv.ID, target.ID, p, u.ID); err != nil {
		return err
	}
	a.Bus.Publish(events.Event{Type: events.ServerMeta, Server: srv.ID, Data: map[string]any{"action": "members"}})
	a.audit(r, srv.ID, "member.add", map[string]any{"user": target.Username, "permissions": p})
	ok(w, nil)
	return nil
}

// PUT /api/servers/{id}/members/{uid}
func (a *API) updateMember(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	uid, _ := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	var req struct {
		Permissions []string `json:"permissions"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	m, err := a.Store.GetMember(r.Context(), inst.ID(), uid)
	if err != nil {
		return err
	}
	p := perms.Sanitize(req.Permissions)
	// granting or revoking is limited to permissions the editor holds
	if !ps.Subset(p) || !ps.Subset(m.Permissions) {
		return badRequest("Du kannst nur Rechte ändern, die du selbst hast")
	}
	if err := a.Store.UpsertMember(r.Context(), a.Store.DB, inst.ID(), uid, p, u.ID); err != nil {
		return err
	}
	a.Bus.Publish(events.Event{Type: events.ServerMeta, Server: inst.ID(), Data: map[string]any{"action": "members"}})
	a.audit(r, inst.ID(), "member.update", map[string]any{"user": m.Username, "permissions": p})
	ok(w, nil)
	return nil
}

// DELETE /api/servers/{id}/members/{uid}
func (a *API) removeMember(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	uid, _ := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	m, err := a.Store.GetMember(r.Context(), inst.ID(), uid)
	if err != nil {
		return err
	}
	if !ps.Subset(m.Permissions) && uid != u.ID {
		return badRequest("Dieses Mitglied hat Rechte, die du nicht hast")
	}
	if err := a.Store.DeleteMember(r.Context(), inst.ID(), uid); err != nil {
		return err
	}
	a.Bus.Publish(events.Event{Type: events.ServerMeta, Server: inst.ID(), Data: map[string]any{"action": "members"}})
	a.audit(r, inst.ID(), "member.remove", map[string]any{"user": m.Username})
	ok(w, nil)
	return nil
}

// POST /api/servers/{id}/invites — invite a new user directly to this server.
func (a *API) serverInvite(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	var req struct {
		Permissions []string `json:"permissions"`
		Note        string   `json:"note"`
		Days        int      `json:"days"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	p := perms.Sanitize(req.Permissions)
	if !ps.Subset(p) {
		return badRequest("Du kannst nur Rechte vergeben, die du selbst hast")
	}
	inv := &store.Invite{ServerID: inst.ID(), ServerPerms: p, Note: truncate(strings.TrimSpace(req.Note), 200)}
	link, err := a.createInvite(r, u, inv, req.Days)
	if err != nil {
		return err
	}
	ok(w, map[string]any{"link": link, "expiresAt": inv.ExpiresAt})
	return nil
}

// GET /api/servers/{id}/activity
func (a *API) activity(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	f := store.AuditFilter{ServerID: inst.ID(), Limit: queryInt(r, "limit", 100), Action: r.URL.Query().Get("action")}
	f.Before, _ = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	list, err := a.Store.ListAudit(r.Context(), f)
	if err != nil {
		return err
	}
	ok(w, list)
	return nil
}

// ---- webhooks ----

func (a *API) serverWebhooks(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	list, err := a.Store.ListWebhooks(r.Context(), inst.ID())
	if err != nil {
		return err
	}
	ok(w, map[string]any{"webhooks": list, "events": notify.Events})
	return nil
}

func (a *API) serverSaveWebhook(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	return a.saveWebhook(w, r, inst.ID())
}

func (a *API) serverDeleteWebhook(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	return a.deleteWebhook(w, r, inst.ID())
}

func (a *API) serverTestWebhook(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	return a.testWebhook(w, r, inst.ID())
}

func (a *API) saveWebhook(w http.ResponseWriter, r *http.Request, serverID string) error {
	var req store.Webhook
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if wid := r.PathValue("wid"); wid != "" {
		id, _ := strconv.ParseInt(wid, 10, 64)
		existing, err := a.Store.GetWebhook(r.Context(), id)
		if err != nil || existing.ServerID != serverID {
			return errNotFound
		}
		req.ID = id
	} else {
		req.ID = 0
	}
	req.ServerID = serverID
	req.Name = truncate(strings.TrimSpace(req.Name), 60)
	if req.Name == "" {
		req.Name = "Webhook"
	}
	if req.Kind != "discord" && req.Kind != "generic" {
		return badRequest("Typ muss discord oder generic sein")
	}
	pu, err := url.Parse(req.URL)
	if err != nil || (pu.Scheme != "https" && pu.Scheme != "http") || pu.Host == "" {
		return badRequest("Ungültige Webhook-URL")
	}
	var evs []string
	for _, e := range req.Events {
		if notify.ValidEvent(e) {
			evs = append(evs, e)
		}
	}
	req.Events = evs
	if err := a.Store.SaveWebhook(r.Context(), &req); err != nil {
		return err
	}
	a.audit(r, serverID, "webhook.save", map[string]any{"name": req.Name})
	ok(w, req)
	return nil
}

func (a *API) deleteWebhook(w http.ResponseWriter, r *http.Request, serverID string) error {
	id, _ := strconv.ParseInt(r.PathValue("wid"), 10, 64)
	h, err := a.Store.GetWebhook(r.Context(), id)
	if err != nil || h.ServerID != serverID {
		return errNotFound
	}
	if err := a.Store.DeleteWebhook(r.Context(), id); err != nil {
		return err
	}
	ok(w, nil)
	return nil
}

func (a *API) testWebhook(w http.ResponseWriter, r *http.Request, serverID string) error {
	id, _ := strconv.ParseInt(r.PathValue("wid"), 10, 64)
	h, err := a.Store.GetWebhook(r.Context(), id)
	if err != nil || h.ServerID != serverID {
		return errNotFound
	}
	if err := a.Notify.Test(h); err != nil {
		return badRequest("Test fehlgeschlagen: " + err.Error())
	}
	ok(w, nil)
	return nil
}

// notifyEvent forwards bus events to webhooks (called from the app).
func (a *API) NotifyFromBus(ctx context.Context) {
	sub := a.Bus.Subscribe(512, func(e events.Event) bool { return e.Type == events.MCEvent })
	defer a.Bus.Unsubscribe(sub)
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-sub.C:
			if !ok {
				return
			}
			ev, isEv := e.Data.(*store.MCEvent)
			inst := a.Servers.Get(e.Server)
			if !isEv || inst == nil {
				continue
			}
			name := inst.Server().Name
			msg := notify.Message{ServerID: e.Server, ServerName: name, PlayerName: ev.Name, PlayerUUID: ev.UUID, At: time.Unix(ev.At, 0)}
			switch ev.Type {
			case "join":
				msg.Event, msg.Title, msg.Text = "player.join", ev.Name+" ist beigetreten", ""
			case "leave":
				var d struct {
					Duration int64 `json:"duration"`
					Silent   bool  `json:"silent"`
				}
				json.Unmarshal(ev.Data, &d)
				if d.Silent {
					continue
				}
				msg.Event, msg.Title = "player.leave", ev.Name+" hat den Server verlassen"
				if d.Duration > 0 {
					msg.Text = "Spielzeit: " + humanDuration(d.Duration)
				}
			case "death":
				var d minecraft.Death
				json.Unmarshal(ev.Data, &d)
				msg.Event, msg.Title, msg.Text = "player.death", "☠ "+ev.Name+" ist gestorben", ev.Message
			case "advancement":
				var d struct {
					Title string `json:"title"`
				}
				json.Unmarshal(ev.Data, &d)
				msg.Event, msg.Title, msg.Text = "player.advancement", "🏆 "+ev.Name, "hat „"+d.Title+"“ erreicht"
			case "chat":
				msg.Event, msg.Title, msg.Text = "player.chat", ev.Name, ev.Message
			default:
				continue
			}
			a.Notify.Send(msg)
		}
	}
}

func humanDuration(sec int64) string {
	d := time.Duration(sec) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%d Std. %d Min.", h, m)
	}
	return fmt.Sprintf("%d Min.", max(1, m))
}

// LifecycleNotify is passed to the server manager and backup service.
func (a *API) LifecycleNotify(serverID, event, detail string) {
	inst := a.Servers.Get(serverID)
	if inst == nil {
		return
	}
	name := inst.Server().Name
	msg := notify.Message{Event: event, ServerID: serverID, ServerName: name, Text: detail}
	switch event {
	case "server.started":
		msg.Title = "🟢 " + name + " ist online"
	case "server.stopped":
		msg.Title = "⚪ " + name + " wurde gestoppt"
	case "server.crashed":
		msg.Title = "🔴 " + name + " ist abgestürzt"
	case "backup.completed":
		msg.Title = "💾 Backup erstellt"
	case "backup.failed":
		msg.Title = "⚠️ Backup fehlgeschlagen"
	default:
		return
	}
	a.Notify.Send(msg)
}

var errOffline = errors.New("Der Server muss dafür gestoppt sein")

// keepMapPlugin carries the panel-managed map plugin fields over into new map settings.
func keepMapPlugin(old, next any) any {
	nm, ok := next.(map[string]any)
	if !ok {
		return next
	}
	om, _ := old.(map[string]any)
	for _, k := range []string{"plugin", "pluginPort"} {
		if v, ok := om[k]; ok {
			nm[k] = v
		} else {
			delete(nm, k)
		}
	}
	return nm
}
