package api

import (
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"hearth/internal/backup"
	"hearth/internal/perms"
	"hearth/internal/sched"
	"hearth/internal/servers"
	"hearth/internal/store"
)

func (a *API) loadBackup(r *http.Request, inst *servers.Instance) (*store.Backup, error) {
	b, err := a.Store.GetBackup(r.Context(), r.PathValue("bid"))
	if err != nil || b.ServerID != inst.ID() {
		return nil, errNotFound
	}
	return b, nil
}

// GET /api/servers/{id}/backups
func (a *API) listBackups(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	list, err := a.Store.ListBackups(r.Context(), inst.ID())
	if err != nil {
		return err
	}
	var cfg struct {
		BackupIgnore *[]string `json:"backupIgnore"`
	}
	json.Unmarshal(inst.Server().Settings, &cfg)
	ignore := backup.DefaultIgnore
	if cfg.BackupIgnore != nil {
		ignore = *cfg.BackupIgnore
	}
	var total int64
	for _, b := range list {
		total += b.Size
	}
	ok(w, map[string]any{"backups": list, "running": a.Backups.Running(inst.ID()), "ignore": ignore,
		"max": a.Settings.Int("backups.max"), "totalSize": total})
	return nil
}

// POST /api/servers/{id}/backups
func (a *API) createBackup(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	var req struct {
		Name string `json:"name"`
	}
	readJSON(r, &req)
	b, err := a.Backups.Create(r.Context(), inst, truncate(strings.TrimSpace(req.Name), 100), u.ID)
	if err != nil {
		return err
	}
	a.audit(r, inst.ID(), "backup.create", map[string]any{"name": b.Name})
	ok(w, b)
	return nil
}

// GET /api/servers/{id}/backups/{bid}/download
func (a *API) downloadBackup(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	b, err := a.loadBackup(r, inst)
	if err != nil {
		return err
	}
	f, err := os.Open(a.Backups.Path(b))
	if err != nil {
		return errNotFound
	}
	defer f.Close()
	name := strings.ReplaceAll(strings.ToLower(inst.Server().Name), " ", "-") + "_" + b.File
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	http.ServeContent(w, r, name, time.Unix(b.CreatedAt, 0), f)
	return nil
}

// POST /api/servers/{id}/backups/{bid}/restore
func (a *API) restoreBackup(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	b, err := a.loadBackup(r, inst)
	if err != nil {
		return err
	}
	var req struct {
		Wipe bool `json:"wipe"`
	}
	readJSON(r, &req)
	if err := a.Backups.Restore(r.Context(), inst, b, req.Wipe); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "backup.restore", map[string]any{"name": b.Name, "wipe": req.Wipe})
	ok(w, nil)
	return nil
}

// PATCH /api/servers/{id}/backups/{bid}
func (a *API) updateBackup(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	b, err := a.loadBackup(r, inst)
	if err != nil {
		return err
	}
	var req struct {
		Name   *string `json:"name"`
		Locked *bool   `json:"locked"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	if req.Name != nil {
		if !ps.Has(perms.BackupsCreate) {
			return errForbidden
		}
		n := truncate(strings.TrimSpace(*req.Name), 100)
		if n != "" {
			b.Name = n
		}
	}
	if req.Locked != nil {
		if !ps.Has(perms.BackupsDelete) {
			return errForbidden
		}
		b.Locked = *req.Locked
	}
	if err := a.Store.UpdateBackup(r.Context(), b); err != nil {
		return err
	}
	ok(w, b)
	return nil
}

// DELETE /api/servers/{id}/backups/{bid}
func (a *API) deleteBackup(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	b, err := a.loadBackup(r, inst)
	if err != nil {
		return err
	}
	if b.Locked {
		return badRequest("Gesperrte Backups müssen zuerst entsperrt werden")
	}
	if err := a.Backups.Delete(r.Context(), b); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "backup.delete", map[string]any{"name": b.Name})
	ok(w, nil)
	return nil
}

// PUT /api/servers/{id}/backups/settings {ignore: [...]}
func (a *API) backupSettings(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	var req struct {
		Ignore []string `json:"ignore"`
	}
	if err := readJSON(r, &req); err != nil {
		return err
	}
	var clean []string
	for _, p := range req.Ignore {
		p = strings.TrimSpace(p)
		if p != "" && len(p) < 200 {
			clean = append(clean, p)
		}
	}
	if clean == nil {
		clean = []string{}
	}
	srv := inst.Server()
	var m map[string]any
	json.Unmarshal(srv.Settings, &m)
	if m == nil {
		m = map[string]any{}
	}
	m["backupIgnore"] = clean
	srv.Settings, _ = json.Marshal(m)
	if err := a.Servers.Update(r.Context(), srv); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "backup.settings", nil)
	ok(w, map[string]any{"ignore": clean})
	return nil
}

// ---- schedules ----

type scheduleView struct {
	*store.Schedule
	Next    []int64 `json:"next"`
	Running bool    `json:"running"`
}

func (a *API) scheduleView(sc *store.Schedule) scheduleView {
	v := scheduleView{Schedule: sc, Running: a.Sched.IsRunning(sc.ID), Next: []int64{}}
	for _, t := range sched.Next(sc.Cron, a.Sched.Location(), 3) {
		v.Next = append(v.Next, t.Unix())
	}
	return v
}

// GET /api/servers/{id}/schedules
func (a *API) listSchedules(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	list, err := a.Store.ListSchedules(r.Context(), inst.ID())
	if err != nil {
		return err
	}
	out := make([]scheduleView, 0, len(list))
	for _, sc := range list {
		out = append(out, a.scheduleView(sc))
	}
	ok(w, map[string]any{"schedules": out, "timezone": a.Sched.Location().String()})
	return nil
}

func validateSchedule(sc *store.Schedule) error {
	sc.Name = truncate(strings.TrimSpace(sc.Name), 80)
	if sc.Name == "" {
		return badRequest("Bitte einen Namen angeben")
	}
	sc.Cron = strings.TrimSpace(sc.Cron)
	if err := sched.Validate(sc.Cron); err != nil {
		return badRequest(err.Error())
	}
	if err := sched.ValidateSteps(sc.Steps); err != nil {
		return badRequest(err.Error())
	}
	return nil
}

// POST /api/servers/{id}/schedules
func (a *API) createSchedule(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	var sc store.Schedule
	if err := readJSON(r, &sc); err != nil {
		return err
	}
	if err := validateSchedule(&sc); err != nil {
		return err
	}
	if err := a.checkStepPerms(ps, sc.Steps); err != nil {
		return err
	}
	sc.ID = 0
	sc.ServerID = inst.ID()
	if err := a.Store.CreateSchedule(r.Context(), &sc); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "schedule.create", map[string]any{"name": sc.Name, "cron": sc.Cron})
	ok(w, a.scheduleView(&sc))
	return nil
}

// checkStepPerms prevents members from scheduling actions they could not run themselves.
func (a *API) checkStepPerms(ps *perms.Set, steps []store.ScheduleStep) error {
	for _, st := range steps {
		need := ""
		switch st.Type {
		case "command", "say":
			need = perms.ConsoleCommand
		case "backup":
			need = perms.BackupsCreate
		case "power":
			switch st.Value {
			case "start":
				need = perms.PowerStart
			case "kill":
				need = perms.PowerKill
			default:
				need = perms.PowerStop
			}
		}
		if need != "" && !ps.Has(need) {
			return badRequest("Dir fehlt die Berechtigung für einen der Schritte")
		}
	}
	return nil
}

func (a *API) loadSchedule(r *http.Request, inst *servers.Instance) (*store.Schedule, error) {
	id, _ := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	sc, err := a.Store.GetSchedule(r.Context(), id)
	if err != nil || sc.ServerID != inst.ID() {
		return nil, errNotFound
	}
	return sc, nil
}

// PUT /api/servers/{id}/schedules/{sid}
func (a *API) updateSchedule(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	sc, err := a.loadSchedule(r, inst)
	if err != nil {
		return err
	}
	var req store.Schedule
	if err := readJSON(r, &req); err != nil {
		return err
	}
	sc.Name, sc.Cron, sc.Enabled, sc.OnlyWhenOnline, sc.Steps = req.Name, req.Cron, req.Enabled, req.OnlyWhenOnline, req.Steps
	if err := validateSchedule(sc); err != nil {
		return err
	}
	if err := a.checkStepPerms(ps, sc.Steps); err != nil {
		return err
	}
	if err := a.Store.UpdateSchedule(r.Context(), sc); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "schedule.update", map[string]any{"name": sc.Name})
	ok(w, a.scheduleView(sc))
	return nil
}

// DELETE /api/servers/{id}/schedules/{sid}
func (a *API) deleteSchedule(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	sc, err := a.loadSchedule(r, inst)
	if err != nil {
		return err
	}
	if err := a.Store.DeleteSchedule(r.Context(), sc.ID); err != nil {
		return err
	}
	a.audit(r, inst.ID(), "schedule.delete", map[string]any{"name": sc.Name})
	ok(w, nil)
	return nil
}

// POST /api/servers/{id}/schedules/{sid}/run
func (a *API) runSchedule(w http.ResponseWriter, r *http.Request, u *store.User, inst *servers.Instance, ps *perms.Set) error {
	sc, err := a.loadSchedule(r, inst)
	if err != nil {
		return err
	}
	if err := a.checkStepPerms(ps, sc.Steps); err != nil {
		return err
	}
	if !a.Sched.Trigger(sc, u.Username) {
		return errStatus(http.StatusConflict, "Dieser Zeitplan läuft gerade")
	}
	ok(w, nil)
	return nil
}

// GET /api/cron/preview?expr=
func (a *API) cronPreview(w http.ResponseWriter, r *http.Request, u *store.User) error {
	expr := r.URL.Query().Get("expr")
	if err := sched.Validate(expr); err != nil {
		return badRequest(err.Error())
	}
	out := []int64{}
	for _, t := range sched.Next(expr, a.Sched.Location(), 5) {
		out = append(out, t.Unix())
	}
	ok(w, map[string]any{"next": out})
	return nil
}
