// Package backup creates, restores and prunes server backups (zip archives).
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"hearth/internal/events"
	"hearth/internal/files"
	"hearth/internal/game"
	"hearth/internal/servers"
	"hearth/internal/settings"
	"hearth/internal/store"
)

type Service struct {
	Dir      string
	Store    *store.Store
	Servers  *servers.Manager
	Bus      *events.Bus
	Settings *settings.Service
	Log      *slog.Logger
	// Notify is called when a backup finished (ok or failed).
	Notify func(serverID, event, detail string)

	mu      sync.Mutex
	running map[string]string // serverID -> backup id
}

func (s *Service) init() {
	s.mu.Lock()
	if s.running == nil {
		s.running = map[string]string{}
	}
	s.mu.Unlock()
}

func (s *Service) Path(b *store.Backup) string {
	return filepath.Join(s.Dir, b.ServerID, b.File)
}

// DefaultIgnore are skipped unless the server configures its own list.
var DefaultIgnore = []string{"logs/", "cache/", "crash-reports/", "*.hearth-tmp", "bluemap/web/maps/", "plugins/dynmap/web/tiles/", "plugins/BlueMap/web/maps/"}

func ignoreList(srv *store.Server) []string {
	var cfg struct {
		BackupIgnore *[]string `json:"backupIgnore"`
	}
	json.Unmarshal(srv.Settings, &cfg)
	if cfg.BackupIgnore != nil {
		return *cfg.BackupIgnore
	}
	return DefaultIgnore
}

type Progress struct {
	BackupID string `json:"backupId"`
	Status   string `json:"status"` // running, ok, failed, restoring, restored
	Files    int64  `json:"files"`
	Bytes    int64  `json:"bytes"`
	Total    int64  `json:"total"`
	Error    string `json:"error,omitempty"`
}

// Running returns the id of the backup currently being created for a server.
func (s *Service) Running(serverID string) string {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[serverID]
}

// Create starts a backup in the background and returns the record immediately.
func (s *Service) Create(ctx context.Context, inst *servers.Instance, name string, by int64) (*store.Backup, error) {
	s.init()
	srv := inst.Server()
	s.mu.Lock()
	if _, busy := s.running[srv.ID]; busy {
		s.mu.Unlock()
		return nil, errors.New("Für diesen Server läuft bereits ein Backup")
	}
	if b := inst.Busy(); b != "" {
		s.mu.Unlock()
		return nil, servers.ErrBusy
	}
	id := store.RandomID(10)
	s.running[srv.ID] = id
	s.mu.Unlock()

	if name == "" {
		name = "Backup " + time.Now().Format("02.01.2006 15:04")
	}
	b := &store.Backup{
		ID: id, ServerID: srv.ID, Name: name, Status: "running", CreatedBy: by,
		File: time.Now().Format("2006-01-02_15-04-05") + "_" + id + ".zip",
	}
	if err := s.Store.CreateBackup(ctx, b); err != nil {
		s.mu.Lock()
		delete(s.running, srv.ID)
		s.mu.Unlock()
		return nil, err
	}
	go s.run(inst, b, ignoreList(srv))
	return b, nil
}

func (s *Service) publish(serverID string, p Progress) {
	s.Bus.Publish(events.Event{Type: events.Backup, Server: serverID, Data: p})
}

func (s *Service) run(inst *servers.Instance, b *store.Backup, ignore []string) {
	defer func() {
		s.mu.Lock()
		delete(s.running, b.ServerID)
		s.mu.Unlock()
	}()
	start := time.Now()
	dir := filepath.Join(s.Dir, b.ServerID)
	fail := func(err error) {
		b.Status, b.Error, b.FinishedAt = "failed", err.Error(), time.Now().Unix()
		s.Store.UpdateBackup(context.Background(), b)
		os.Remove(filepath.Join(dir, b.File+".tmp"))
		s.publish(b.ServerID, Progress{BackupID: b.ID, Status: "failed", Error: err.Error()})
		inst.Console("Backup fehlgeschlagen: " + err.Error())
		if s.Notify != nil {
			s.Notify(b.ServerID, "backup.failed", err.Error())
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(err)
		return
	}
	fsys := files.New(inst.Dir())
	skip := func(rel string, isDir bool) bool { return files.MatchIgnore(ignore, rel, isDir) }

	// estimate total size for the progress bar
	var total int64
	filepath.WalkDir(inst.Dir(), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(inst.Dir(), p)
		rel = filepath.ToSlash(rel)
		if rel != "." && skip(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})

	// flush world data so the backup is consistent
	resume := func() {}
	if saver, ok := inst.Handler().(game.Saver); ok && inst.Running() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resume = saver.PrepareBackup(ctx)
		cancel()
	}
	inst.Console("Backup wird erstellt: " + b.Name)
	tmp := filepath.Join(dir, b.File+".tmp")
	out, err := os.Create(tmp)
	if err != nil {
		resume()
		fail(err)
		return
	}
	last := time.Now()
	nFiles, nBytes, err := fsys.ZipPaths(out, nil, skip, func(f, n int64) {
		if time.Since(last) > 500*time.Millisecond {
			last = time.Now()
			s.publish(b.ServerID, Progress{BackupID: b.ID, Status: "running", Files: f, Bytes: n, Total: total})
		}
	})
	resume()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		fail(err)
		return
	}
	if err := os.Rename(tmp, filepath.Join(dir, b.File)); err != nil {
		fail(err)
		return
	}
	st, _ := os.Stat(filepath.Join(dir, b.File))
	if st != nil {
		b.Size = st.Size()
	}
	b.Files = nFiles
	b.Status = "ok"
	b.FinishedAt = time.Now().Unix()
	s.Store.UpdateBackup(context.Background(), b)
	s.publish(b.ServerID, Progress{BackupID: b.ID, Status: "ok", Files: nFiles, Bytes: nBytes, Total: total})
	inst.Console(fmt.Sprintf("Backup fertig: %d Dateien, %.1f MB in %s.", nFiles, float64(b.Size)/1e6,
		time.Since(start).Round(time.Second)))
	if s.Notify != nil {
		s.Notify(b.ServerID, "backup.completed", fmt.Sprintf("%s (%.1f MB)", b.Name, float64(b.Size)/1e6))
	}
	s.prune(b.ServerID)
}

// prune deletes the oldest unlocked backups above the configured maximum.
func (s *Service) prune(serverID string) {
	max := s.Settings.Int(settings.BackupsMax)
	if max <= 0 {
		return
	}
	list, err := s.Store.ListBackups(context.Background(), serverID)
	if err != nil {
		return
	}
	var ok []*store.Backup
	for _, b := range list {
		if b.Status == "ok" && !b.Locked {
			ok = append(ok, b)
		}
	}
	sort.Slice(ok, func(i, j int) bool { return ok[i].CreatedAt > ok[j].CreatedAt })
	for i := max; i < len(ok); i++ {
		s.Delete(context.Background(), ok[i])
	}
}

func (s *Service) Delete(ctx context.Context, b *store.Backup) error {
	if b.Status == "running" && s.Running(b.ServerID) == b.ID {
		return errors.New("Backup läuft noch")
	}
	os.Remove(s.Path(b))
	return s.Store.DeleteBackup(ctx, b.ID)
}

// Restore replaces the server files with a backup. The server must be offline.
// wipe=true deletes all current files first (recommended for full restores).
func (s *Service) Restore(ctx context.Context, inst *servers.Instance, b *store.Backup, wipe bool) error {
	if inst.State() != string(servers.StateOffline) {
		return errors.New("Der Server muss für eine Wiederherstellung gestoppt sein")
	}
	if b.Status != "ok" {
		return errors.New("Dieses Backup ist nicht vollständig")
	}
	if _, err := os.Stat(s.Path(b)); err != nil {
		return errors.New("Backup-Datei fehlt")
	}
	if err := inst.SetBusy("restore"); err != nil {
		return err
	}
	go func() {
		defer inst.SetBusy("")
		inst.Console("Wiederherstellung von „" + b.Name + "“ gestartet …")
		s.publish(b.ServerID, Progress{BackupID: b.ID, Status: "restoring", Total: b.Size})
		last := time.Now()
		progress := func(f, n int64) {
			if time.Since(last) > 500*time.Millisecond {
				last = time.Now()
				s.publish(b.ServerID, Progress{BackupID: b.ID, Status: "restoring", Files: f, Bytes: n})
			}
		}
		var err error
		if wipe {
			// extract next to the server directory and swap on success, so a
			// failed restore never leaves a half-deleted server behind
			tmpDir := inst.Dir() + ".restoring"
			oldDir := inst.Dir() + ".old"
			os.RemoveAll(tmpDir)
			os.RemoveAll(oldDir)
			err = files.New(tmpDir).ExtractZipFile(s.Path(b), 0, progress)
			if err == nil {
				if err = os.Rename(inst.Dir(), oldDir); err == nil {
					if err = os.Rename(tmpDir, inst.Dir()); err != nil {
						os.Rename(oldDir, inst.Dir())
					} else {
						os.RemoveAll(oldDir)
					}
				}
			}
			os.RemoveAll(tmpDir)
		} else {
			err = files.New(inst.Dir()).ExtractZipFile(s.Path(b), 0, progress)
		}
		if err != nil {
			inst.Console("Wiederherstellung fehlgeschlagen: " + err.Error())
			s.publish(b.ServerID, Progress{BackupID: b.ID, Status: "failed", Error: err.Error()})
			return
		}
		inst.Console("Wiederherstellung abgeschlossen.")
		s.publish(b.ServerID, Progress{BackupID: b.ID, Status: "restored"})
		inst.RefreshDisk()
	}()
	return nil
}
