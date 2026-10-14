// Package sched runs user defined schedules (cron expressions with a list of steps).
package sched

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"hearth/internal/backup"
	"hearth/internal/servers"
	"hearth/internal/settings"
	"hearth/internal/store"
)

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Validate checks a cron expression.
func Validate(expr string) error {
	if _, err := parser.Parse(strings.TrimSpace(expr)); err != nil {
		return fmt.Errorf("Ungültiger Cron-Ausdruck: %v", err)
	}
	return nil
}

// ValidateSteps checks the step list of a schedule.
func ValidateSteps(steps []store.ScheduleStep) error {
	if len(steps) == 0 {
		return errors.New("Ein Zeitplan braucht mindestens einen Schritt")
	}
	if len(steps) > 50 {
		return errors.New("Zu viele Schritte (max. 50)")
	}
	for _, st := range steps {
		switch st.Type {
		case "command", "say":
			if strings.TrimSpace(st.Value) == "" || strings.ContainsAny(st.Value, "\r\n") {
				return errors.New("Befehlsschritte brauchen einen einzeiligen Text")
			}
		case "power":
			switch st.Value {
			case "start", "stop", "restart", "kill":
			default:
				return errors.New("Unbekannte Aktion: " + st.Value)
			}
		case "backup":
		case "wait":
			if st.Seconds < 1 || st.Seconds > 3600 {
				return errors.New("Wartezeit muss zwischen 1 Sekunde und 1 Stunde liegen")
			}
		default:
			return errors.New("Unbekannter Schritt-Typ: " + st.Type)
		}
	}
	return nil
}

// Next returns the next n run times of an expression.
func Next(expr string, loc *time.Location, n int) []time.Time {
	sch, err := parser.Parse(strings.TrimSpace(expr))
	if err != nil {
		return nil
	}
	t := time.Now().In(loc)
	var out []time.Time
	for range n {
		t = sch.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out
}

type Scheduler struct {
	Store    *store.Store
	Servers  *servers.Manager
	Backups  *backup.Service
	Settings *settings.Service
	Log      *slog.Logger
	Audit    func(serverID, action string, details map[string]any)

	mu      sync.Mutex
	running map[int64]bool
}

func (s *Scheduler) Location() *time.Location {
	if tz := s.Settings.Get(settings.Timezone); tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.Local
}

// Run evaluates schedules until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.mu.Lock()
	s.running = map[int64]bool{}
	s.mu.Unlock()
	last := time.Now()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.tick(last, now)
			last = now
		}
	}
}

func (s *Scheduler) tick(from, now time.Time) {
	list, err := s.Store.ListSchedules(context.Background(), "")
	if err != nil {
		return
	}
	loc := s.Location()
	for _, sc := range list {
		if !sc.Enabled {
			continue
		}
		sch, err := parser.Parse(sc.Cron)
		if err != nil {
			continue
		}
		next := sch.Next(from.In(loc))
		if next.IsZero() || next.After(now) {
			continue
		}
		s.Trigger(sc, "")
	}
}

// Trigger runs a schedule now (in the background) unless it is already running.
func (s *Scheduler) Trigger(sc *store.Schedule, actor string) bool {
	s.mu.Lock()
	if s.running == nil {
		s.running = map[int64]bool{}
	}
	if s.running[sc.ID] {
		s.mu.Unlock()
		return false
	}
	s.running[sc.ID] = true
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.running, sc.ID)
			s.mu.Unlock()
		}()
		start := time.Now()
		status, errMsg := s.execute(sc, actor)
		s.Store.SetScheduleResult(context.Background(), sc.ID, start.Unix(), status, errMsg)
		if s.Audit != nil {
			s.Audit(sc.ServerID, "schedule.run", map[string]any{"schedule": sc.Name, "status": status, "error": errMsg})
		}
	}()
	return true
}

func (s *Scheduler) IsRunning(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[id]
}

func (s *Scheduler) execute(sc *store.Schedule, actor string) (status, errMsg string) {
	inst := s.Servers.Get(sc.ServerID)
	if inst == nil {
		return "failed", "Server nicht gefunden"
	}
	label := "Zeitplan „" + sc.Name + "“"
	if actor != "" {
		label += " (" + actor + ")"
	}
	if sc.OnlyWhenOnline && !inst.Running() {
		return "skipped", "Server war offline"
	}
	inst.Console(label + " wird ausgeführt.")
	var errs []string
	for idx, st := range sc.Steps {
		if err := s.step(inst, st, label); err != nil {
			errs = append(errs, fmt.Sprintf("Schritt %d: %v", idx+1, err))
			if st.Type == "power" || st.Type == "backup" {
				break // later steps usually depend on these
			}
		}
	}
	if len(errs) > 0 {
		return "failed", strings.Join(errs, "; ")
	}
	return "ok", ""
}

func (s *Scheduler) step(inst *servers.Instance, st store.ScheduleStep, label string) error {
	switch st.Type {
	case "wait":
		time.Sleep(time.Duration(st.Seconds) * time.Second)
		return nil
	case "command":
		return inst.Command(st.Value, label)
	case "say":
		if inst.Server().Game == "minecraft" {
			return inst.Command("say "+st.Value, label)
		}
		return inst.Command(st.Value, label)
	case "backup":
		b, err := s.Backups.Create(context.Background(), inst, st.Value, 0)
		if err != nil {
			return err
		}
		deadline := time.Now().Add(6 * time.Hour)
		for time.Now().Before(deadline) {
			time.Sleep(2 * time.Second)
			cur, err := s.Store.GetBackup(context.Background(), b.ID)
			if err != nil {
				return err
			}
			switch cur.Status {
			case "ok":
				return nil
			case "failed":
				return errors.New(cur.Error)
			}
		}
		return errors.New("Backup-Zeitüberschreitung")
	case "power":
		switch st.Value {
		case "start":
			if inst.State() != string(servers.StateOffline) {
				return nil
			}
			if err := inst.Start(label); err != nil {
				return err
			}
			return waitState(inst, servers.StateRunning, 10*time.Minute)
		case "stop":
			if inst.State() == string(servers.StateOffline) {
				return nil
			}
			if err := inst.Stop(label, false); err != nil {
				return err
			}
			if !inst.WaitOffline(5 * time.Minute) {
				return errors.New("Server hat nicht rechtzeitig gestoppt")
			}
			return nil
		case "restart":
			if inst.State() == string(servers.StateOffline) {
				if err := inst.Start(label); err != nil {
					return err
				}
			} else if err := inst.Stop(label, true); err != nil {
				return err
			}
			time.Sleep(3 * time.Second)
			return waitState(inst, servers.StateRunning, 10*time.Minute)
		case "kill":
			return inst.Kill(label)
		}
	}
	return fmt.Errorf("unbekannter Schritt %q", st.Type)
}

func waitState(inst *servers.Instance, want servers.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st := inst.State()
		if st == string(want) {
			return nil
		}
		if want == servers.StateRunning && st == string(servers.StateOffline) {
			// give a pending (re)start a moment before failing
			time.Sleep(3 * time.Second)
			if inst.State() == string(servers.StateOffline) {
				return errors.New("Server ist nicht gestartet")
			}
		}
		time.Sleep(time.Second)
	}
	return errors.New("Zeitüberschreitung")
}
