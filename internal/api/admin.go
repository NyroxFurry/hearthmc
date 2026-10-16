package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"

	"hearth/internal/auth"
	"hearth/internal/javart"
	"hearth/internal/notify"
	"hearth/internal/perms"
	"hearth/internal/settings"
	"hearth/internal/store"
	"hearth/internal/version"
)

// ---- users ----

type userView struct {
	*store.User
	Servers int `json:"servers"`
}

// GET /api/admin/users
func (a *API) adminUsers(w http.ResponseWriter, r *http.Request, u *store.User) error {
	list, err := a.Store.ListUsers(r.Context())
	if err != nil {
		return err
	}
	counts := map[int64]int{}
	for _, inst := range a.Servers.List() {
		counts[inst.Server().OwnerID]++
	}
	out := make([]userView, 0, len(list))
	for _, x := range list {
		out = append(out, userView{User: x, Servers: counts[x.ID]})
	}
	ok(w, out)
	return nil
}

type userReq struct {
	Username         *string `json:"username"`
	Email            *string `json:"email"`
	Password         *string `json:"password"`
	IsAdmin          *bool   `json:"isAdmin"`
	Disabled         *bool   `json:"disabled"`
	CanCreateServers *bool   `json:"canCreateServers"`
	MaxServers       *int    `json:"maxServers"`
	MaxMemoryMB      *int    `json:"maxMemoryMb"`
}

func applyUserReq(x *store.User, req *userReq) error {
	if req.Username != nil {
		n := strings.TrimSpace(*req.Username)
		if err := validUsername(n); err != nil {
			return err
		}
		x.Username = n
	}
	if req.Email != nil {
		x.Email = truncate(strings.TrimSpace(*req.Email), 200)
	}
	if req.IsAdmin != nil {
		x.IsAdmin = *req.IsAdmin
	}
	if req.Disabled != nil {
		x.Disabled = *req.Disabled
	}
	if req.CanCreateServers != nil {
		x.CanCreateServers = *req.CanCreateServers
	}
	if req.MaxServers != nil {
		x.MaxServers = max(0, *req.MaxServers)
	}
	if req.MaxMemoryMB != nil {
		x.MaxMemoryMB = max(0, *req.MaxMemoryMB)
	}
	return nil
}

// POST /api/admin/users
func (a *API) adminCreateUser(w http.ResponseWriter, r *http.Request, u *store.User) error {
	var req userReq
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if req.Username == nil || req.Password == nil {
		return badRequest("Benutzername und Passwort sind erforderlich")
	}
	x := &store.User{}
	if err := applyUserReq(x, &req); err != nil {
		return err
	}
	if err := auth.ValidatePassword(*req.Password); err != nil {
		return badRequest(err.Error())
	}
	hash, err := auth.HashPassword(*req.Password)
	if err != nil {
		return err
	}
	x.PasswordHash = hash
	if err := a.Store.CreateUser(r.Context(), x); err != nil {
		if store.IsUniqueViolation(err) {
			return badRequest("Dieser Benutzername ist bereits vergeben")
		}
		return err
	}
	a.audit(r, "", "user.create", map[string]any{"user": x.Username, "admin": x.IsAdmin})
	ok(w, x)
	return nil
}

// PATCH /api/admin/users/{uid}
func (a *API) adminUpdateUser(w http.ResponseWriter, r *http.Request, u *store.User) error {
	id, _ := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	x, err := a.Store.GetUser(r.Context(), id)
	if err != nil {
		return err
	}
	var req userReq
	if err := readJSON(r, &req); err != nil {
		return err
	}
	wasAdmin := x.IsAdmin && !x.Disabled
	if err := applyUserReq(x, &req); err != nil {
		return err
	}
	if wasAdmin && (!x.IsAdmin || x.Disabled) {
		n, _ := a.Store.CountAdmins(r.Context())
		if n <= 1 {
			return badRequest("Der letzte aktive Administrator kann nicht entfernt werden")
		}
	}
	if err := a.Store.UpdateUser(r.Context(), x); err != nil {
		if store.IsUniqueViolation(err) {
			return badRequest("Dieser Benutzername ist bereits vergeben")
		}
		return err
	}
	if req.Password != nil && *req.Password != "" {
		if err := auth.ValidatePassword(*req.Password); err != nil {
			return badRequest(err.Error())
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			return err
		}
		a.Store.SetPassword(r.Context(), x.ID, hash)
		a.Store.DeleteUserSessions(r.Context(), x.ID, "")
	}
	if x.Disabled {
		a.Store.DeleteUserSessions(r.Context(), x.ID, "")
	}
	a.audit(r, "", "user.update", map[string]any{"user": x.Username})
	ok(w, x)
	return nil
}

// POST /api/admin/users/{uid}/reset-2fa
func (a *API) adminReset2FA(w http.ResponseWriter, r *http.Request, u *store.User) error {
	id, _ := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	x, err := a.Store.GetUser(r.Context(), id)
	if err != nil {
		return err
	}
	if err := a.Store.SetTOTP(r.Context(), x.ID, "", false, nil); err != nil {
		return err
	}
	a.audit(r, "", "user.reset_2fa", map[string]any{"user": x.Username})
	ok(w, nil)
	return nil
}

// DELETE /api/admin/users/{uid}
func (a *API) adminDeleteUser(w http.ResponseWriter, r *http.Request, u *store.User) error {
	id, _ := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	if id == u.ID {
		return badRequest("Du kannst dich nicht selbst löschen")
	}
	x, err := a.Store.GetUser(r.Context(), id)
	if err != nil {
		return err
	}
	if x.IsAdmin {
		n, _ := a.Store.CountAdmins(r.Context())
		if n <= 1 {
			return badRequest("Der letzte Administrator kann nicht gelöscht werden")
		}
	}
	for _, inst := range a.Servers.List() {
		if inst.Server().OwnerID == id {
			return badRequest("Dieser Benutzer besitzt noch Server – übertrage sie zuerst an jemand anderen")
		}
	}
	if err := a.Store.DeleteUser(r.Context(), id); err != nil {
		return err
	}
	a.audit(r, "", "user.delete", map[string]any{"user": x.Username})
	ok(w, nil)
	return nil
}

// GET /api/users/search?q=
func (a *API) searchUsers(w http.ResponseWriter, r *http.Request, u *store.User) error {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	out := []store.UserBrief{}
	if len(q) < 2 {
		ok(w, out)
		return nil
	}
	list, err := a.Store.ListUsers(r.Context())
	if err != nil {
		return err
	}
	for _, x := range list {
		if x.Disabled || !strings.HasPrefix(strings.ToLower(x.Username), q) {
			continue
		}
		out = append(out, store.UserBrief{ID: x.ID, Username: x.Username})
		if len(out) >= 10 {
			break
		}
	}
	ok(w, out)
	return nil
}

// ---- invites ----

// GET /api/admin/invites
func (a *API) adminInvites(w http.ResponseWriter, r *http.Request, u *store.User) error {
	list, err := a.Store.ListInvites(r.Context())
	if err != nil {
		return err
	}
	ok(w, list)
	return nil
}

// POST /api/admin/invites (admins) — members with members.manage may invite to their server via the server endpoint.
func (a *API) createInvite(r *http.Request, u *store.User, inv *store.Invite, days int) (string, error) {
	if days <= 0 || days > 90 {
		days = 7
	}
	tok := auth.RandomToken(24)
	inv.TokenHash = auth.HashToken(tok)
	inv.CreatedBy = u.ID
	inv.ExpiresAt = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	inv.ServerPerms = perms.Sanitize(inv.ServerPerms)
	if err := a.Store.CreateInvite(r.Context(), inv); err != nil {
		return "", err
	}
	a.audit(r, inv.ServerID, "invite.create", map[string]any{"note": inv.Note, "admin": inv.IsAdmin})
	return a.PublicURL(r) + "/invite/" + tok, nil
}

func (a *API) adminCreateInvite(w http.ResponseWriter, r *http.Request, u *store.User) error {
	var req struct {
		store.Invite
		Days int `json:"days"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if req.ServerID != "" && a.Servers.Get(req.ServerID) == nil {
		return badRequest("Server nicht gefunden")
	}
	inv := req.Invite
	inv.Note = truncate(strings.TrimSpace(inv.Note), 200)
	link, err := a.createInvite(r, u, &inv, req.Days)
	if err != nil {
		return err
	}
	ok(w, map[string]any{"invite": inv, "link": link})
	return nil
}

// DELETE /api/admin/invites/{iid}
func (a *API) adminDeleteInvite(w http.ResponseWriter, r *http.Request, u *store.User) error {
	id, _ := strconv.ParseInt(r.PathValue("iid"), 10, 64)
	if err := a.Store.DeleteInvite(r.Context(), id); err != nil {
		return err
	}
	ok(w, nil)
	return nil
}

// ---- settings ----

// GET /api/admin/settings
func (a *API) adminSettings(w http.ResponseWriter, r *http.Request, u *store.User) error {
	ok(w, a.Settings.All())
	return nil
}

// PUT /api/admin/settings
func (a *API) adminUpdateSettings(w http.ResponseWriter, r *http.Request, u *store.User) error {
	var req map[string]string
	if err := readJSON(r, &req); err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, k := range settings.Public {
		allowed[k] = true
	}
	intRange := func(v string, lo, hi int) error {
		n, err := strconv.Atoi(v)
		if err != nil || n < lo || n > hi {
			return fmt.Errorf("Wert muss zwischen %d und %d liegen", lo, hi)
		}
		return nil
	}
	for k, v := range req {
		if !allowed[k] {
			return badRequest("Unbekannte Einstellung: " + k)
		}
		v = strings.TrimSpace(v)
		var err error
		switch k {
		case settings.PanelName:
			if v == "" || len(v) > 60 {
				err = fmt.Errorf("Name muss 1–60 Zeichen lang sein")
			}
		case settings.PanelURL, settings.MapPublicURL:
			if v != "" {
				if pu, perr := url.Parse(v); perr != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
					err = fmt.Errorf("Ungültige URL")
				}
				v = strings.TrimRight(v, "/")
			}
		case settings.DefaultRuntime:
			if v != "native" && v != "docker" {
				err = fmt.Errorf("Laufzeit muss native oder docker sein")
			}
		case settings.PortStart, settings.PortEnd, settings.InternalPortStart, settings.InternalPortEnd:
			err = intRange(v, 1024, 65535)
		case settings.BackupsMax:
			err = intRange(v, 0, 1000)
		case settings.MetricsRetention, settings.AuditRetention, settings.ChatRetention:
			err = intRange(v, 1, 3650)
		case settings.MapThreads:
			err = intRange(v, 1, 64)
		case settings.DefaultMemoryMB:
			err = intRange(v, 256, 1<<20)
		case settings.UploadLimitMB:
			err = intRange(v, 0, 1<<24)
		case settings.Timezone:
			if v != "" {
				if _, lerr := time.LoadLocation(v); lerr != nil {
					err = fmt.Errorf("Unbekannte Zeitzone")
				}
			}
		case settings.JavaMode:
			if v != "auto" && v != "system" {
				err = fmt.Errorf("Java-Modus muss auto oder system sein")
			}
		case settings.PublicPagesEnabled:
			if v != "0" && v != "1" {
				err = fmt.Errorf("Wert muss 0 oder 1 sein")
			}
		case settings.SFTPHost:
			if v != "" && (len(v) > 255 || strings.ContainsAny(v, " /\\@") || strings.Contains(v, "://")) {
				err = fmt.Errorf("Bitte nur einen Hostnamen, optional mit Port (z. B. sftp.example.de:2022)")
			}
		case settings.DockerHost:
			if v != "" && !strings.HasPrefix(v, "unix://") && !strings.HasPrefix(v, "tcp://") {
				err = fmt.Errorf("Docker-Host muss mit unix:// oder tcp:// beginnen")
			}
		}
		if err != nil {
			return badRequest(k + ": " + err.Error())
		}
		req[k] = v
	}
	for k, v := range req {
		if err := a.Settings.Set(r.Context(), k, v); err != nil {
			return err
		}
		if k == settings.DockerHost {
			a.Servers.ReconfigureDocker(v)
		}
	}
	a.audit(r, "", "settings.update", map[string]any{"keys": keys(req)})
	ok(w, a.Settings.All())
	return nil
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---- system ----

// GET /api/admin/system
func (a *API) adminSystem(w http.ResponseWriter, r *http.Request, u *store.User) error {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	out := map[string]any{
		"version":   version.Version,
		"commit":    version.Commit,
		"goVersion": goruntime.Version(),
		"os":        goruntime.GOOS,
		"arch":      goruntime.GOARCH,
		"uptime":    int64(time.Since(a.started).Seconds()),
		"dataDir":   a.Cfg.DataDir,
		"pid":       os.Getpid(),
	}
	if hi, err := host.InfoWithContext(ctx); err == nil {
		out["hostname"] = hi.Hostname
		out["platform"] = strings.TrimSpace(hi.Platform + " " + hi.PlatformVersion)
		out["kernel"] = hi.KernelVersion
		out["hostUptime"] = hi.Uptime
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		out["cpus"] = n
	}
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		out["cpuModel"] = infos[0].ModelName
	}
	if p, err := cpu.PercentWithContext(ctx, 300*time.Millisecond, false); err == nil && len(p) > 0 {
		out["cpuPercent"] = p[0]
	}
	if l, err := load.AvgWithContext(ctx); err == nil {
		out["load"] = []float64{l.Load1, l.Load5, l.Load15}
	}
	if m, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		out["memTotal"] = m.Total
		out["memUsed"] = m.Total - m.Available
	}
	if d, err := disk.UsageWithContext(ctx, a.Cfg.DataDir); err == nil {
		out["diskTotal"] = d.Total
		out["diskUsed"] = d.Used
		out["diskFree"] = d.Free
	}
	dockerErr := a.Servers.Docker().Check(ctx)
	out["docker"] = dockerErr == nil
	if dockerErr != nil {
		out["dockerError"] = dockerErr.Error()
	} else if info, err := a.Servers.Docker().Info(ctx); err == nil {
		out["dockerVersion"] = info["ServerVersion"]
	}
	running := 0
	for _, inst := range a.Servers.List() {
		if inst.State() != "offline" {
			running++
		}
	}
	out["servers"] = len(a.Servers.List())
	out["serversRunning"] = running
	sysJava, sysMajor := javart.System()
	out["systemJava"] = map[string]any{"path": sysJava, "major": sysMajor}
	ok(w, out)
	return nil
}

// GET /api/admin/audit
func (a *API) adminAudit(w http.ResponseWriter, r *http.Request, u *store.User) error {
	f := store.AuditFilter{
		ServerID: r.URL.Query().Get("server"),
		Action:   r.URL.Query().Get("action"),
		Limit:    queryInt(r, "limit", 100),
	}
	f.Before, _ = strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	f.UserID, _ = strconv.ParseInt(r.URL.Query().Get("user"), 10, 64)
	list, err := a.Store.ListAudit(r.Context(), f)
	if err != nil {
		return err
	}
	ok(w, list)
	return nil
}

// ---- java runtimes ----

// GET /api/admin/java
func (a *API) adminJava(w http.ResponseWriter, r *http.Request, u *store.User) error {
	list := a.Java.List()
	if list == nil {
		list = []javart.Runtime{}
	}
	sysPath, sysMajor := javart.System()
	ok(w, map[string]any{"runtimes": list, "system": map[string]any{"path": sysPath, "major": sysMajor}})
	return nil
}

// POST /api/admin/java/{major}
func (a *API) adminInstallJava(w http.ResponseWriter, r *http.Request, u *store.User) error {
	major, err := strconv.Atoi(r.PathValue("major"))
	if err != nil || major < 8 || major > 99 {
		return badRequest("Ungültige Java-Version")
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_, err := a.Java.Ensure(ctx, major, func(msg string) {
			a.Bus.Publish(eventTask("java", msg))
		})
		if err != nil {
			a.Bus.Publish(eventTask("java", "Fehler: "+err.Error()))
		} else {
			a.Bus.Publish(eventTask("java", fmt.Sprintf("Java %d ist bereit.", major)))
		}
	}()
	a.audit(r, "", "java.install", map[string]any{"major": major})
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
	return nil
}

// DELETE /api/admin/java/{major}
func (a *API) adminRemoveJava(w http.ResponseWriter, r *http.Request, u *store.User) error {
	major, _ := strconv.Atoi(r.PathValue("major"))
	if err := a.Java.Remove(major); err != nil {
		return err
	}
	a.audit(r, "", "java.remove", map[string]any{"major": major})
	ok(w, nil)
	return nil
}

// ---- global webhooks ----

func (a *API) adminWebhooks(w http.ResponseWriter, r *http.Request, u *store.User) error {
	list, err := a.Store.ListWebhooks(r.Context(), "")
	if err != nil {
		return err
	}
	ok(w, map[string]any{"webhooks": list, "events": notify.Events})
	return nil
}

func (a *API) adminSaveWebhook(w http.ResponseWriter, r *http.Request, u *store.User) error {
	return a.saveWebhook(w, r, "")
}

func (a *API) adminDeleteWebhook(w http.ResponseWriter, r *http.Request, u *store.User) error {
	return a.deleteWebhook(w, r, "")
}

func (a *API) adminTestWebhook(w http.ResponseWriter, r *http.Request, u *store.User) error {
	return a.testWebhook(w, r, "")
}
