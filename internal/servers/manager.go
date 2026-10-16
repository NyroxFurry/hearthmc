// Package servers manages the lifecycle of all game server instances.
package servers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"hearth/internal/config"
	"hearth/internal/events"
	"hearth/internal/game"
	"hearth/internal/runtime"
	"hearth/internal/settings"
	"hearth/internal/store"
)

type Manager struct {
	cfg      *config.Config
	store    *store.Store
	bus      *events.Bus
	settings *settings.Service
	log      *slog.Logger
	games    map[string]game.Game

	native runtime.Runtime
	docker *runtime.Docker

	mu        sync.RWMutex
	instances map[string]*Instance
	shutting  atomic.Bool
	portMu    sync.Mutex

	// AuditFunc records system actions (set by the app).
	AuditFunc func(serverID, action string, details map[string]any)
	// NotifyFunc receives lifecycle notifications (set by the app, used for webhooks).
	NotifyFunc func(serverID, event, detail string)
}

func NewManager(cfg *config.Config, st *store.Store, bus *events.Bus, set *settings.Service, log *slog.Logger, games ...game.Game) *Manager {
	m := &Manager{
		cfg: cfg, store: st, bus: bus, settings: set, log: log,
		games:     map[string]game.Game{},
		native:    runtime.Native{},
		instances: map[string]*Instance{},
	}
	for _, g := range games {
		m.games[g.ID()] = g
	}
	m.docker = runtime.NewDocker(set.Get(settings.DockerHost))
	m.docker.OnPull = func(serverID, msg string) {
		if inst := m.Get(serverID); inst != nil {
			inst.Console("Docker: " + msg)
		}
	}
	return m
}

func (m *Manager) Games() map[string]game.Game { return m.games }

func (m *Manager) Game(id string) (game.Game, bool) {
	g, ok := m.games[id]
	return g, ok
}

func (m *Manager) Docker() *runtime.Docker { return m.docker }

// ReconfigureDocker applies a changed docker host setting.
func (m *Manager) ReconfigureDocker(host string) {
	d := runtime.NewDocker(host)
	d.OnPull = m.docker.OnPull
	m.docker = d
}

func (m *Manager) Runtime(name string) (runtime.Runtime, error) {
	switch name {
	case "", "native":
		return m.native, nil
	case "docker":
		return m.docker, nil
	}
	return nil, fmt.Errorf("unbekannte Laufzeit %q", name)
}

func (m *Manager) audit(serverID, action string, details map[string]any) {
	if m.AuditFunc != nil {
		m.AuditFunc(serverID, action, details)
	}
}

func (m *Manager) notifyLifecycle(i *Instance, event, detail string) {
	if m.NotifyFunc != nil {
		m.NotifyFunc(i.id, event, detail)
	}
}

func (m *Manager) isShuttingDown() bool { return m.shutting.Load() }

// Load creates instances for all servers in the database, reattaches running
// Docker containers and starts servers flagged for autostart.
func (m *Manager) Load(ctx context.Context) error {
	list, err := m.store.ListServers(ctx)
	if err != nil {
		return err
	}
	var toStart []*Instance
	for _, srv := range list {
		inst := newInstance(m, srv)
		m.mu.Lock()
		m.instances[srv.ID] = inst
		m.mu.Unlock()
		// sessions left open by an unclean shutdown
		if srv.Runtime == "docker" {
			if proc, history, err := m.docker.Reattach(ctx, srv.ID); err == nil && proc != nil {
				m.log.Info("reattached running container", "server", srv.ID)
				inst.attach(proc, nil, history)
				inst.Console("Panel neu gestartet – mit laufendem Container verbunden.")
				continue
			}
		}
		if (srv.Autostart || srv.WasRunning) && srv.InstallState == "installed" && !srv.Suspended {
			toStart = append(toStart, inst)
		}
	}
	go func() {
		for _, inst := range toStart {
			if m.isShuttingDown() {
				return
			}
			if err := inst.Start(""); err != nil {
				inst.Console("Autostart fehlgeschlagen: " + err.Error())
			} else {
				inst.Console("Autostart nach Panel-Start.")
			}
			time.Sleep(3 * time.Second)
		}
	}()
	go m.loop()
	return nil
}

// loop samples resource usage and records metrics.
func (m *Manager) loop() {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	var n int
	for range tick.C {
		if m.isShuttingDown() {
			return
		}
		n++
		for _, inst := range m.List() {
			st := inst.State()
			if st == string(StateRunning) || st == string(StateStarting) || st == string(StateStopping) {
				inst.sampleUsage()
				status := inst.Status()
				m.bus.Publish(events.Event{Type: events.Stats, Server: inst.id, Data: status})
				if n%30 == 0 { // every minute
					m.store.AddMetric(context.Background(), inst.id, store.MetricPoint{
						At: time.Now().Unix(), CPU: status.CPU, Mem: int64(status.Mem), Heap: int64(status.Heap), Players: status.Players,
					})
				}
			}
			inst.mu.RLock()
			stale := time.Since(inst.diskAt) > 10*time.Minute
			inst.mu.RUnlock()
			if stale {
				inst.mu.Lock()
				inst.diskAt = time.Now() // prevent duplicate walks
				inst.mu.Unlock()
				go inst.updateDisk()
			}
		}
	}
}

func (m *Manager) Get(id string) *Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.instances[id]
}

func (m *Manager) List() []*Instance {
	m.mu.RLock()
	out := make([]*Instance, 0, len(m.instances))
	for _, i := range m.instances {
		out = append(out, i)
	}
	m.mu.RUnlock()
	sort.Slice(out, func(a, b int) bool { return out[a].srv.CreatedAt < out[b].srv.CreatedAt })
	return out
}

// Create persists a new server and runs its installer.
func (m *Manager) Create(ctx context.Context, srv *store.Server, startAfterInstall bool) (*Instance, error) {
	g, ok := m.games[srv.Game]
	if !ok {
		return nil, fmt.Errorf("unbekannter Spieltyp %q", srv.Game)
	}
	if err := g.ValidateSettings(srv, true); err != nil {
		return nil, err
	}
	for range 5 {
		srv.ID = store.RandomID(8)
		if m.Get(srv.ID) == nil {
			break
		}
	}
	srv.InstallState = "installing"
	if err := os.MkdirAll(filepath.Join(m.cfg.ServersDir, srv.ID), 0o755); err != nil {
		return nil, err
	}
	if err := m.store.CreateServer(ctx, srv); err != nil {
		return nil, err
	}
	inst := newInstance(m, srv)
	m.mu.Lock()
	m.instances[srv.ID] = inst
	m.mu.Unlock()
	m.bus.Publish(events.Event{Type: events.ServerMeta, Server: srv.ID, Data: map[string]any{"action": "created"}})
	if err := inst.Install("", startAfterInstall); err != nil {
		return inst, err
	}
	return inst, nil
}

// Update persists changed server configuration.
func (m *Manager) Update(ctx context.Context, srv *store.Server) error {
	inst := m.Get(srv.ID)
	if inst == nil {
		return store.ErrNotFound
	}
	if g, ok := m.games[srv.Game]; ok {
		if err := g.ValidateSettings(srv, false); err != nil {
			return err
		}
	}
	if err := m.store.UpdateServer(ctx, srv); err != nil {
		return err
	}
	inst.setConfig(srv.Clone())
	m.bus.Publish(events.Event{Type: events.ServerMeta, Server: srv.ID, Data: map[string]any{"action": "updated"}})
	return nil
}

// Delete stops and removes a server including all files, backups and map tiles.
func (m *Manager) Delete(ctx context.Context, id string) error {
	inst := m.Get(id)
	if inst == nil {
		return store.ErrNotFound
	}
	if inst.State() != string(StateOffline) {
		inst.Kill("")
		if !inst.WaitOffline(20 * time.Second) {
			return errors.New("Server konnte nicht gestoppt werden")
		}
	}
	inst.handler.Close()
	m.mu.Lock()
	delete(m.instances, id)
	m.mu.Unlock()
	if err := m.store.DeleteServer(ctx, id); err != nil {
		return err
	}
	for _, dir := range []string{
		filepath.Join(m.cfg.ServersDir, id),
		filepath.Join(m.cfg.BackupsDir, id),
		filepath.Join(m.cfg.MapsDir(), id),
	} {
		if err := os.RemoveAll(dir); err != nil {
			m.log.Warn("delete server files", "dir", dir, "err", err)
		}
	}
	m.bus.Publish(events.Event{Type: events.ServerMeta, Server: id, Data: map[string]any{"action": "deleted"}})
	return nil
}

// Shutdown gracefully stops native servers (Docker containers keep running and
// are reattached on the next start).
func (m *Manager) Shutdown(timeout time.Duration) {
	m.shutting.Store(true)
	var wg sync.WaitGroup
	for _, inst := range m.List() {
		st := inst.State()
		if st == string(StateOffline) || st == string(StateInstalling) {
			inst.handler.Close()
			continue
		}
		if inst.RuntimeName() == "docker" {
			inst.handler.Close()
			continue
		}
		wg.Add(1)
		go func(inst *Instance) {
			defer wg.Done()
			inst.mu.Lock()
			inst.shutdownStop = true
			inst.mu.Unlock()
			if err := inst.Stop("Panel wird beendet", false); err == nil {
				if !inst.WaitOffline(timeout) {
					inst.Kill("")
					inst.WaitOffline(5 * time.Second)
				}
			}
			inst.handler.Close()
		}(inst)
	}
	wg.Wait()
}

// ---- ports ----

func portFree(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	l.Close()
	pc, err := net.ListenPacket("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	pc.Close()
	return true
}

// AllocatePort finds a free port. internal=true uses the internal range (RCON etc.).
// exclude contains ports already chosen in the same operation.
func (m *Manager) AllocatePort(ctx context.Context, preferred int, internal bool, exclude ...int) (int, error) {
	m.portMu.Lock()
	defer m.portMu.Unlock()
	used, err := m.store.UsedPorts(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range exclude {
		used[p] = "pending"
	}
	start, end := m.settings.Int(settings.PortStart), m.settings.Int(settings.PortEnd)
	if internal {
		start, end = m.settings.Int(settings.InternalPortStart), m.settings.Int(settings.InternalPortEnd)
	}
	if preferred > 0 {
		if _, taken := used[preferred]; !taken && portFree(preferred) {
			return preferred, nil
		}
	}
	for p := start; p <= end; p++ {
		if _, taken := used[p]; taken {
			continue
		}
		if portFree(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("kein freier Port im Bereich %d–%d", start, end)
}

// PortInUse reports whether another server already uses port.
func (m *Manager) PortInUse(ctx context.Context, port int, exceptServer string) (bool, error) {
	used, err := m.store.UsedPorts(ctx)
	if err != nil {
		return false, err
	}
	id, ok := used[port]
	return ok && id != exceptServer, nil
}

// ServerSettings decodes the game settings of a server into v.
func ServerSettings(srv *store.Server, v any) error {
	if len(srv.Settings) == 0 {
		return nil
	}
	return json.Unmarshal(srv.Settings, v)
}
