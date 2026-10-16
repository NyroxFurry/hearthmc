package servers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hearth/internal/events"
	"hearth/internal/game"
	"hearth/internal/jvm"
	"hearth/internal/runtime"
	"hearth/internal/store"
)

type State string

const (
	StateOffline    State = "offline"
	StateInstalling State = "installing"
	StateStarting   State = "starting"
	StateRunning    State = "running"
	StateStopping   State = "stopping"
)

var (
	ErrNotRunning     = errors.New("Der Server läuft nicht")
	ErrAlreadyRunning = errors.New("Der Server läuft bereits")
	ErrBusy           = errors.New("Der Server ist gerade beschäftigt")
	ErrNotInstalled   = errors.New("Der Server ist noch nicht installiert")
	ErrSuspended      = errors.New("Der Server ist gesperrt")
)

type procRun struct {
	proc   runtime.Process
	exited chan struct{}
	launch *game.Launch
}

// Instance is the live representation of one game server.
type Instance struct {
	m   *Manager
	id  string
	log *slog.Logger

	mu           sync.RWMutex
	srv          *store.Server
	state        State
	run          *procRun
	rtName       string
	handler      game.Handler
	usage        runtime.Usage
	heap         jvm.Heap  // Java heap of the server process (zero = unknown)
	heapAt       time.Time // last heap query
	heapMisses   int
	javaPID      int
	startedAt    time.Time
	stopping     bool
	restartAfter bool
	shutdownStop bool
	crashes      []time.Time
	busy         string
	lastExitCode int
	lastCrashAt  int64
	crashLoop    bool
	diskBytes    int64
	diskAt       time.Time
	installStep  string

	opMu    sync.Mutex
	console *Console
}

func newInstance(m *Manager, srv *store.Server) *Instance {
	i := &Instance{m: m, id: srv.ID, srv: srv, state: StateOffline, log: m.log.With("server", srv.ID)}
	i.console = NewConsole(2500, func(lines []string) {
		m.bus.Publish(events.Event{Type: events.Console, Server: i.id, Data: lines})
	})
	if g, ok := m.games[srv.Game]; ok {
		i.handler = g.NewHandler(i)
	} else {
		i.handler = nullHandler{}
	}
	return i
}

// ---- game.Host implementation ----

func (i *Instance) ServerID() string { return i.id }
func (i *Instance) Dir() string      { return filepath.Join(i.m.cfg.ServersDir, i.id) }

func (i *Instance) Server() *store.Server {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.srv.Clone()
}

func (i *Instance) UpdateSettings(fn func(json.RawMessage) (json.RawMessage, error)) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	next, err := fn(append(json.RawMessage(nil), i.srv.Settings...))
	if err != nil {
		return err
	}
	cp := i.srv.Clone()
	cp.Settings = next
	if err := i.m.store.UpdateServer(context.Background(), cp); err != nil {
		return err
	}
	i.srv = cp
	return nil
}

func (i *Instance) SendCommand(cmd string) error {
	i.mu.RLock()
	run := i.run
	i.mu.RUnlock()
	if run == nil {
		return ErrNotRunning
	}
	_, err := run.proc.Write([]byte(strings.TrimRight(cmd, "\r\n") + "\n"))
	return err
}

func (i *Instance) Console(msg string) {
	for _, l := range strings.Split(msg, "\n") {
		i.console.Add("\x1b[38;5;208m[Hearth]\x1b[0m " + l)
	}
}

func (i *Instance) Publish(t string, data any) {
	i.m.bus.Publish(events.Event{Type: t, Server: i.id, Data: data})
}

func (i *Instance) Store() *store.Store { return i.m.store }

func (i *Instance) State() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return string(i.state)
}

func (i *Instance) Running() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.state == StateRunning
}

func (i *Instance) RuntimeName() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.srv.Runtime
}

func (i *Instance) Logger() *slog.Logger { return i.log }

func (i *Instance) DataDir(kind string) string {
	var d string
	switch kind {
	case "maps":
		d = filepath.Join(i.m.cfg.MapsDir(), i.id)
	default:
		d = filepath.Join(i.m.cfg.CacheDir(), kind, i.id)
	}
	os.MkdirAll(d, 0o755)
	return d
}

func (i *Instance) Setting(key string) string { return i.m.settings.Get(key) }

// ---- accessors ----

func (i *Instance) ID() string            { return i.id }
func (i *Instance) Handler() game.Handler { return i.handler }
func (i *Instance) ConsoleLines(n int) []string {
	return i.console.Lines(n)
}

// Status is the live status sent to clients.
type Status struct {
	State        State   `json:"state"`
	StartedAt    int64   `json:"startedAt"`
	CPU          float64 `json:"cpu"`
	Mem          uint64  `json:"mem"`
	MemLimit     uint64  `json:"memLimit"`
	Disk         int64   `json:"disk"`
	Players      int     `json:"players"`
	MaxPlayers   int     `json:"maxPlayers"`
	Busy         string  `json:"busy,omitempty"`
	InstallStep  string  `json:"installStep,omitempty"`
	LastExitCode int     `json:"lastExitCode"`
	LastCrashAt  int64   `json:"lastCrashAt"`
	CrashLoop    bool    `json:"crashLoop,omitempty"`
	PID          string  `json:"pid,omitempty"`

	// Java servers: heap in use and heap size. Java reserves the whole heap at
	// start, so Mem stays near the limit while Heap shows the real usage.
	Heap     uint64 `json:"heap,omitempty"`
	HeapSize uint64 `json:"heapSize,omitempty"`
}

func (i *Instance) Status() Status {
	i.mu.RLock()
	st := Status{
		State: i.state, CPU: i.usage.CPU, Mem: i.usage.MemBytes, MemLimit: i.usage.MemLimit, Disk: i.diskBytes,
		Busy: i.busy, InstallStep: i.installStep, LastExitCode: i.lastExitCode, LastCrashAt: i.lastCrashAt,
		CrashLoop: i.crashLoop, Players: -1, MaxPlayers: -1,
	}
	if !i.startedAt.IsZero() && (i.state == StateRunning || i.state == StateStarting || i.state == StateStopping) {
		st.StartedAt = i.startedAt.Unix()
	}
	if i.run != nil {
		st.PID = i.run.proc.ID()
	}
	if i.heap.Committed > 0 && time.Since(i.heapAt) < 2*time.Minute {
		st.Heap, st.HeapSize = i.heap.Used, max(i.heap.Committed, i.heap.Max)
	}
	if st.MemLimit == 0 && i.srv.MemoryMB > 0 {
		st.MemLimit = uint64(i.srv.MemoryMB) << 20
	}
	h := i.handler
	running := i.state == StateRunning
	i.mu.RUnlock()
	if pc, ok := h.(game.PlayerCounter); ok && running {
		st.Players, st.MaxPlayers = pc.PlayerCount()
	}
	return st
}

func (i *Instance) setState(s State) {
	i.mu.Lock()
	i.state = s
	i.mu.Unlock()
	i.publishStatus()
}

func (i *Instance) publishStatus() {
	i.m.bus.Publish(events.Event{Type: events.Status, Server: i.id, Data: i.Status()})
}

func (i *Instance) setBusy(b string) error {
	i.mu.Lock()
	if b != "" && i.busy != "" {
		i.mu.Unlock()
		return ErrBusy
	}
	i.busy = b
	i.mu.Unlock()
	i.publishStatus()
	return nil
}

// SetBusy marks the server busy with a long operation (backup restore, ...).
// While busy, the server cannot be started. Pass "" to clear.
func (i *Instance) SetBusy(b string) error { return i.setBusy(b) }

func (i *Instance) Busy() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.busy
}

// setConfig replaces the config snapshot (after an update via the API).
func (i *Instance) setConfig(srv *store.Server) {
	i.mu.Lock()
	i.srv = srv
	i.mu.Unlock()
}

// ---- power actions ----

// Start launches the server asynchronously. Errors during preparation are
// written to the console.
func (i *Instance) Start(actor string) error {
	i.mu.Lock()
	switch {
	case i.state != StateOffline:
		i.mu.Unlock()
		if i.state == StateInstalling {
			return ErrBusy
		}
		return ErrAlreadyRunning
	case i.busy != "":
		i.mu.Unlock()
		return ErrBusy
	case i.srv.Suspended:
		i.mu.Unlock()
		return ErrSuspended
	case i.srv.InstallState != "installed":
		i.mu.Unlock()
		return ErrNotInstalled
	}
	i.state = StateStarting
	i.crashLoop = false
	i.startedAt = time.Now()
	i.usage = runtime.Usage{}
	i.resetHeap()
	i.mu.Unlock()
	i.publishStatus()
	go i.doStart(actor)
	return nil
}

func (i *Instance) doStart(actor string) {
	i.opMu.Lock()
	defer i.opMu.Unlock()
	srv := i.Server()
	rt, err := i.m.Runtime(srv.Runtime)
	if err == nil {
		err = rt.Check(context.Background())
	}
	if err != nil {
		i.Console("Start fehlgeschlagen: " + err.Error())
		i.setState(StateOffline)
		return
	}
	if actor != "" {
		i.Console("Server wird gestartet (" + actor + ") …")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	launch, err := i.handler.Prepare(ctx, rt.Name())
	if err != nil {
		i.Console("Start fehlgeschlagen: " + err.Error())
		i.setState(StateOffline)
		return
	}
	if i.State() != string(StateStarting) { // stopped/killed while preparing
		return
	}
	spec := &runtime.Spec{
		ServerID: srv.ID,
		Dir:      i.Dir(),
		Argv:     launch.Argv,
		Env:      launch.Env,
		MemoryMB: dockerMemory(srv.MemoryMB),
		CPUs:     srv.CPULimit,
		Image:    launch.Image,
	}
	if srv.Port > 0 {
		spec.Ports = append(spec.Ports, runtime.Port{Host: srv.Port, Container: srv.Port, Proto: "tcp"},
			runtime.Port{Host: srv.Port, Container: srv.Port, Proto: "udp"})
	}
	for _, p := range srv.ExtraPorts {
		for _, proto := range protos(p.Protocol) {
			spec.Ports = append(spec.Ports, runtime.Port{Host: p.Port, Container: p.Port, Proto: proto})
		}
	}
	spec.Ports = append(spec.Ports, launch.ExtraPorts...)
	proc, err := rt.Start(ctx, spec)
	if err != nil {
		i.Console("Start fehlgeschlagen: " + err.Error())
		i.setState(StateOffline)
		return
	}
	i.attach(proc, launch, nil)
	i.m.store.SetServerWasRunning(context.Background(), i.id, true)
}

func protos(p string) []string {
	switch p {
	case "udp":
		return []string{"udp"}
	case "both":
		return []string{"tcp", "udp"}
	default:
		return []string{"tcp"}
	}
}

// dockerMemory adds headroom for JVM/native overhead to the configured game memory.
func dockerMemory(mb int) int {
	if mb <= 0 {
		return 0
	}
	extra := mb / 4
	if extra < 512 {
		extra = 512
	}
	return mb + extra
}

// attach wires up output pumping and exit watching for a freshly started
// (or reattached) process.
func (i *Instance) attach(proc runtime.Process, launch *game.Launch, history []string) {
	run := &procRun{proc: proc, exited: make(chan struct{}), launch: launch}
	i.mu.Lock()
	i.run = run
	i.rtName = i.srv.Runtime
	if launch == nil { // reattached: assume it is fully up
		i.state = StateRunning
	}
	i.mu.Unlock()
	for _, l := range history {
		i.console.Add(l)
	}
	i.handler.OnStart()
	// games without a readiness marker are "running" as soon as the process is up
	if launch != nil && i.handler.Ready("") {
		i.mu.Lock()
		if i.state == StateStarting {
			i.state = StateRunning
		}
		i.mu.Unlock()
		defer i.m.notifyLifecycle(i, "server.started", "")
	}
	i.publishStatus()
	pumpDone := make(chan struct{})
	go i.pump(proc, pumpDone)
	go i.watch(run, pumpDone)
}

func (i *Instance) pump(proc runtime.Process, done chan struct{}) {
	defer close(done)
	r := bufio.NewReaderSize(proc.Output(), 64<<10)
	var partial []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			partial = append(partial, chunk...)
			if len(partial) > 256<<10 {
				i.handleLine(string(partial))
				partial = partial[:0]
			}
			continue
		}
		if len(chunk) > 0 || len(partial) > 0 {
			line := string(append(partial, chunk...))
			partial = partial[:0]
			line = strings.TrimRight(line, "\r\n")
			if line != "" || err == nil {
				i.handleLine(line)
			}
		}
		if err != nil {
			if err != io.EOF && !errors.Is(err, os.ErrClosed) {
				i.log.Debug("output closed", "err", err)
			}
			return
		}
	}
}

func (i *Instance) handleLine(line string) {
	// some programs redraw progress with \r; keep only the final state
	if idx := strings.LastIndexByte(line, '\r'); idx >= 0 {
		line = line[idx+1:]
	}
	i.console.Add(line)
	i.handler.OnLine(line)
	i.mu.RLock()
	starting := i.state == StateStarting
	i.mu.RUnlock()
	if starting && i.handler.Ready(line) {
		i.mu.Lock()
		if i.state == StateStarting {
			i.state = StateRunning
		}
		i.mu.Unlock()
		i.publishStatus()
		i.m.notifyLifecycle(i, "server.started", "")
	}
}

func (i *Instance) watch(run *procRun, pumpDone chan struct{}) {
	code, _ := run.proc.Wait()
	select {
	case <-pumpDone:
	case <-time.After(3 * time.Second):
	}
	close(run.exited)

	i.mu.Lock()
	if i.run != run {
		i.mu.Unlock()
		return
	}
	i.run = nil
	wasStopping := i.stopping
	restart := i.restartAfter
	shutdown := i.shutdownStop
	i.stopping, i.restartAfter, i.shutdownStop = false, false, false
	crashed := !wasStopping && code != 0
	i.lastExitCode = code
	i.state = StateOffline
	i.usage = runtime.Usage{}
	i.resetHeap()
	autoRestart := i.srv.AutoRestart
	var loop bool
	if crashed {
		now := time.Now()
		i.lastCrashAt = now.Unix()
		recent := i.crashes[:0]
		for _, t := range i.crashes {
			if now.Sub(t) < 10*time.Minute {
				recent = append(recent, t)
			}
		}
		i.crashes = append(recent, now)
		if len(i.crashes) >= 3 {
			loop = true
			i.crashLoop = true
		}
	}
	i.mu.Unlock()

	i.handler.OnStop(code, crashed)
	if crashed {
		i.Console(fmt.Sprintf("Server ist abgestürzt (Exit-Code %d).", code))
		i.m.audit(i.id, "server.crash", map[string]any{"exitCode": code})
	} else {
		i.Console(fmt.Sprintf("Server gestoppt (Exit-Code %d).", code))
	}
	i.publishStatus()
	if !shutdown {
		i.m.store.SetServerWasRunning(context.Background(), i.id, false)
	}
	if crashed {
		i.m.notifyLifecycle(i, "server.crashed", fmt.Sprintf("Exit-Code %d", code))
	} else {
		i.m.notifyLifecycle(i, "server.stopped", "")
	}
	if i.m.isShuttingDown() || shutdown {
		return
	}
	switch {
	case restart:
		time.Sleep(time.Second)
		if err := i.Start(""); err != nil {
			i.Console("Neustart fehlgeschlagen: " + err.Error())
		}
	case crashed && autoRestart && !loop:
		i.Console("Automatischer Neustart in 5 Sekunden …")
		time.AfterFunc(5*time.Second, func() {
			if i.State() == string(StateOffline) && !i.m.isShuttingDown() {
				if err := i.Start(""); err != nil {
					i.Console("Automatischer Neustart fehlgeschlagen: " + err.Error())
				}
			}
		})
	case crashed && loop:
		i.Console("Crash-Schleife erkannt (3 Abstürze in 10 Minuten). Automatischer Neustart pausiert.")
	}
}

// Stop requests a graceful stop. restart=true starts the server again afterwards.
func (i *Instance) Stop(actor string, restart bool) error {
	i.mu.Lock()
	if i.state == StateStopping {
		if restart {
			i.restartAfter = true
		}
		i.mu.Unlock()
		return nil
	}
	if i.state == StateStarting && i.run == nil {
		// still preparing (e.g. downloading Java): abort
		i.state = StateOffline
		i.mu.Unlock()
		i.Console("Start abgebrochen.")
		i.publishStatus()
		return nil
	}
	if i.state != StateRunning && i.state != StateStarting {
		i.mu.Unlock()
		return ErrNotRunning
	}
	run := i.run
	wasRunning := i.state == StateRunning
	i.stopping = true
	i.restartAfter = restart
	i.state = StateStopping
	i.mu.Unlock()
	i.publishStatus()
	if actor != "" {
		if restart {
			i.Console("Neustart angefordert (" + actor + ") …")
		} else {
			i.Console("Stopp angefordert (" + actor + ") …")
		}
	}
	cmd := i.handler.StopCommand()
	if cmd != "" && wasRunning {
		if _, err := run.proc.Write([]byte(cmd + "\n")); err != nil {
			run.proc.Terminate()
		}
	} else {
		run.proc.Terminate()
	}
	timeout := 60 * time.Second
	if run.launch != nil && run.launch.StopTimeout > 0 {
		timeout = run.launch.StopTimeout
	}
	go i.enforceStop(run, timeout)
	return nil
}

func (i *Instance) enforceStop(run *procRun, timeout time.Duration) {
	select {
	case <-run.exited:
		return
	case <-time.After(timeout):
	}
	i.Console("Server reagiert nicht auf den Stopp-Befehl – sende SIGTERM …")
	run.proc.Terminate()
	select {
	case <-run.exited:
		return
	case <-time.After(20 * time.Second):
	}
	i.Console("Beenden wird erzwungen.")
	run.proc.Kill()
}

// Kill forcefully stops the process.
func (i *Instance) Kill(actor string) error {
	i.mu.Lock()
	if i.state == StateStarting && i.run == nil {
		i.state = StateOffline
		i.mu.Unlock()
		i.publishStatus()
		return nil
	}
	run := i.run
	if run == nil {
		i.mu.Unlock()
		return ErrNotRunning
	}
	i.stopping = true
	i.restartAfter = false
	i.mu.Unlock()
	if actor != "" {
		i.Console("Prozess wird hart beendet (" + actor + ").")
	}
	return run.proc.Kill()
}

// Command sends a console command on behalf of a user.
func (i *Instance) Command(cmd, actor string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	if strings.ContainsAny(cmd, "\r\n") {
		return errors.New("Befehle dürfen keine Zeilenumbrüche enthalten")
	}
	i.mu.RLock()
	run := i.run
	i.mu.RUnlock()
	if run == nil {
		return ErrNotRunning
	}
	i.console.Add("\x1b[38;5;244m> " + cmd + "\x1b[0m")
	_, err := run.proc.Write([]byte(cmd + "\n"))
	return err
}

// Install runs the game installer asynchronously.
func (i *Instance) Install(actor string, thenStart bool) error {
	i.mu.Lock()
	if i.state != StateOffline {
		i.mu.Unlock()
		return ErrAlreadyRunning
	}
	if i.busy != "" {
		i.mu.Unlock()
		return ErrBusy
	}
	i.state = StateInstalling
	i.installStep = ""
	i.mu.Unlock()
	i.publishStatus()
	go func() {
		i.opMu.Lock()
		defer i.opMu.Unlock()
		os.MkdirAll(i.Dir(), 0o755)
		if actor != "" {
			i.Console("Installation gestartet (" + actor + ") …")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		err := i.handler.Install(ctx)
		cancel()
		i.mu.Lock()
		cp := i.srv.Clone()
		if err != nil {
			cp.InstallState, cp.InstallError = "failed", err.Error()
		} else {
			cp.InstallState, cp.InstallError = "installed", ""
		}
		i.m.store.UpdateServer(context.Background(), cp)
		i.srv = cp
		i.state = StateOffline
		i.installStep = ""
		i.mu.Unlock()
		if err != nil {
			i.Console("Installation fehlgeschlagen: " + err.Error())
			i.m.audit(i.id, "server.install_failed", map[string]any{"error": err.Error()})
		} else {
			i.Console("Installation abgeschlossen.")
		}
		i.publishStatus()
		i.Publish(events.ServerMeta, map[string]any{"action": "installed"})
		if err == nil && thenStart {
			if err := i.Start(""); err != nil {
				i.Console("Start fehlgeschlagen: " + err.Error())
			}
		}
	}()
	return nil
}

// SetInstallStep updates the install progress label shown in the UI.
func (i *Instance) SetInstallStep(step string) {
	i.mu.Lock()
	i.installStep = step
	i.mu.Unlock()
	i.publishStatus()
}

// WaitOffline blocks until the server is offline or the timeout expires.
func (i *Instance) WaitOffline(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if i.State() == string(StateOffline) {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func (i *Instance) sampleUsage() {
	i.mu.RLock()
	run := i.run
	i.mu.RUnlock()
	if run == nil {
		return
	}
	u, err := run.proc.Usage()
	if err != nil {
		return
	}
	i.mu.Lock()
	i.usage = u
	i.mu.Unlock()
	i.sampleHeap(run)
}

// resetHeap forgets the heap of the previous run (caller holds i.mu).
func (i *Instance) resetHeap() {
	i.heap, i.heapAt, i.heapMisses, i.javaPID = jvm.Heap{}, time.Time{}, 0, 0
}

// sampleHeap reads the Java heap every few seconds. Processes without a JVM
// (or with an old Java) are asked less and less often, up to once a minute.
func (i *Instance) sampleHeap(run *procRun) {
	hp, ok := run.proc.(runtime.HostPIDer)
	if !ok {
		return
	}
	i.mu.RLock()
	wait := 6 * time.Second << min(i.heapMisses, 4)
	due := (i.state == StateRunning || i.state == StateStarting) && time.Since(i.heapAt) >= min(wait, time.Minute)
	pid := i.javaPID
	i.mu.RUnlock()
	if !due {
		return
	}
	if pid == 0 {
		if root := hp.HostPID(); root > 0 {
			pid = jvm.FindJava(root)
		}
	}
	h, err := jvm.Heap{}, jvm.ErrNoListener
	if pid > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		h, err = jvm.HeapInfo(ctx, pid)
		cancel()
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.run != run {
		return // the server stopped in the meantime
	}
	i.heapAt = time.Now()
	if err != nil {
		i.heapMisses++
		i.javaPID = 0
		return
	}
	i.heap, i.heapMisses, i.javaPID = h, 0, pid
}

func (i *Instance) updateDisk() {
	var total int64
	filepath.WalkDir(i.Dir(), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	i.mu.Lock()
	i.diskBytes = total
	i.diskAt = time.Now()
	i.mu.Unlock()
}

// RefreshDisk recalculates disk usage in the background.
func (i *Instance) RefreshDisk() { go i.updateDisk() }

func (i *Instance) DiskBytes() int64 {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.diskBytes
}

// nullHandler is used for servers whose game type is unknown (e.g. plugin removed).
type nullHandler struct{}

func (nullHandler) Install(context.Context) error { return errors.New("unbekannter Spieltyp") }
func (nullHandler) Prepare(context.Context, string) (*game.Launch, error) {
	return nil, errors.New("unbekannter Spieltyp")
}
func (nullHandler) OnStart()            {}
func (nullHandler) OnLine(string)       {}
func (nullHandler) Ready(string) bool   { return true }
func (nullHandler) StopCommand() string { return "" }
func (nullHandler) OnStop(int, bool)    {}
func (nullHandler) Info() any           { return nil }
func (nullHandler) Close()              {}
