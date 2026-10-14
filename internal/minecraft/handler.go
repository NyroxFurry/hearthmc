package minecraft

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"hearth/internal/events"
	"hearth/internal/game"
	"hearth/internal/httpx"
	"hearth/internal/javart"
	"hearth/internal/nbt"
	"hearth/internal/runtime"
	"hearth/internal/store"
)

type OnlinePlayer struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	Since     int64  `json:"since"`
	sessionID int64
	missing   int
}

type Position struct {
	UUID string  `json:"uuid"`
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Z    float64 `json:"z"`
	Dim  string  `json:"dim"`
	At   int64   `json:"at"`
	Live bool    `json:"live"`
}

// Info is the live status returned by the API.
type Info struct {
	Flavor     string         `json:"flavor"`
	Version    string         `json:"version"`
	Build      string         `json:"build"`
	Detected   string         `json:"detectedVersion"`
	Online     []OnlinePlayer `json:"online"`
	MaxPlayers int            `json:"maxPlayers"`
	RCON       bool           `json:"rcon"`
	RCONError  string         `json:"rconError,omitempty"`
	TPS        []float64      `json:"tps,omitempty"`
	Ready      bool           `json:"ready"`
	JavaMajor  int            `json:"javaMajor"`
	World      string         `json:"world"`
	ModDir     string         `json:"modDir"`
}

type job struct {
	kind   string // join, leave, event, stats, stale
	player *OnlinePlayer
	event  *store.MCEvent
	silent bool
	at     int64
}

// Handler is the per-server Minecraft integration.
type Handler struct {
	g    *Game
	host game.Host
	log  *slog.Logger

	mu         sync.Mutex
	online     map[string]*OnlinePlayer // lower(name) -> player
	uuids      map[string]string        // lower(name) -> uuid from login lines
	detected   string
	ready      bool
	maxPlayers int
	saved      chan struct{}
	rcon       *RCON
	rconErr    string
	tps        []float64
	positions  map[string]Position
	posWanted  time.Time
	ping       *PingResult
	pingAt     time.Time
	runCancel  context.CancelFunc
	rconWake   chan struct{}
	statsSoon  chan struct{} // a player left: their stats file was just written

	jobs      chan job
	closed    chan struct{}
	closeOnce sync.Once
}

func newHandler(g *Game, host game.Host) *Handler {
	h := &Handler{
		g: g, host: host, log: host.Logger().With("game", "minecraft"),
		online: map[string]*OnlinePlayer{}, uuids: map[string]string{}, positions: map[string]Position{},
		saved: make(chan struct{}, 1), rconWake: make(chan struct{}, 1), statsSoon: make(chan struct{}, 1),
		jobs: make(chan job, 2000), closed: make(chan struct{}),
	}
	go h.worker()
	return h
}

func (h *Handler) Settings() Settings { return DecodeSettings(h.host.Server().Settings) }

func (h *Handler) Host() game.Host { return h.host }

func (h *Handler) Game() *Game { return h.g }

func (h *Handler) propsPath() string { return filepath.Join(h.host.Dir(), "server.properties") }

// LevelName returns the configured world folder.
func (h *Handler) LevelName() string {
	p, err := LoadProperties(h.propsPath())
	if err != nil {
		return "world"
	}
	return p.GetDefault("level-name", "world")
}

func (h *Handler) World() *WorldLayout { return DetectWorld(h.host.Dir(), h.LevelName()) }

// ---- installation ----

func (h *Handler) Install(ctx context.Context) error {
	s := h.Settings()
	dir := h.host.Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if s.Flavor == "custom" {
		h.host.Console("Eigene Server-JAR: lade deine JAR-Datei als „" + s.Jar + "“ im Dateimanager hoch.")
		if s.JavaMajor == 0 {
			s.JavaMajor = GuessJava(s.Version)
		}
	} else {
		h.host.SetInstallStep("Versionsinformationen abrufen")
		h.host.Console(fmt.Sprintf("Suche %s %s …", s.Flavor, s.Version))
		res, err := h.g.Versions.Resolve(ctx, s.Flavor, s.Version, s.Build)
		if err != nil {
			return err
		}
		s.JavaMajor = res.JavaMajor
		if !res.Installer {
			h.host.SetInstallStep("Server herunterladen")
			h.host.Console("Lade Server-JAR herunter: " + res.URL)
			if err := h.download(ctx, res.URL, filepath.Join(dir, "server.jar"), res.Checksum); err != nil {
				return err
			}
			s.Jar = "server.jar"
			s.LoaderArgsFile = ""
		} else {
			if err := h.runLoaderInstaller(ctx, &s, res); err != nil {
				return err
			}
		}
		s.InstalledVersion, s.InstalledBuild = s.Version, res.Build
	}
	h.host.SetInstallStep("Konfiguration schreiben")
	if s.EULA {
		os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("# Accepted via Hearth panel\neula=true\n"), 0o644)
	}
	if err := h.applyProperties(s, true); err != nil {
		return err
	}
	if s.Map.Plugin != "" {
		h.host.SetInstallStep("Karten-Plugin installieren")
		cur := h.Settings()
		cur.InstalledVersion = s.InstalledVersion
		if err := h.installMapPluginWith(ctx, cur); err != nil {
			// not fatal: the server works without it, the map page offers a retry
			h.host.Console("Hinweis: " + err.Error())
		}
	}
	// pre-fetch the Java runtime so the first start is fast
	if h.host.RuntimeName() != "docker" && s.Java == "auto" && h.host.Setting("java.mode") != "system" {
		h.host.SetInstallStep(fmt.Sprintf("Java %d bereitstellen", s.JavaMajor))
		if _, err := h.g.Java.Ensure(ctx, s.JavaMajor, h.host.Console); err != nil {
			h.host.Console("Hinweis: Java konnte noch nicht geladen werden: " + err.Error())
		}
	}
	return h.host.UpdateSettings(func(raw json.RawMessage) (json.RawMessage, error) {
		cur := DecodeSettings(raw)
		cur.Jar, cur.JavaMajor, cur.LoaderArgsFile = s.Jar, s.JavaMajor, s.LoaderArgsFile
		cur.InstalledVersion, cur.InstalledBuild = s.InstalledVersion, s.InstalledBuild
		return cur.Encode(), nil
	})
}

func (h *Handler) download(ctx context.Context, url, dest string, sum *httpx.Checksum) error {
	last := -1
	return h.g.HTTP.Download(ctx, url, dest, sum, func(done, total int64) {
		if total <= 0 {
			return
		}
		pct := int(done * 100 / total)
		if pct/25 != last/25 {
			last = pct
			h.host.Console(fmt.Sprintf("Download: %d%% (%.1f MB)", pct, float64(total)/1e6))
		}
	})
}

func (h *Handler) javaForInstaller(ctx context.Context, major int, s Settings) (string, error) {
	if s.Java != "auto" && s.Java != "system" {
		return s.Java, nil
	}
	if s.Java == "system" || h.host.Setting("java.mode") == "system" {
		if p, _ := javart.System(); p != "" {
			return p, nil
		}
	}
	return h.g.Java.Ensure(ctx, major, h.host.Console)
}

// runLoaderInstaller downloads and runs the Forge/NeoForge installer.
func (h *Handler) runLoaderInstaller(ctx context.Context, s *Settings, res *Resolved) error {
	dir := h.host.Dir()
	installer := filepath.Join(dir, "hearth-installer.jar")
	h.host.SetInstallStep("Installer herunterladen")
	h.host.Console("Lade Installer: " + res.URL)
	if err := h.download(ctx, res.URL, installer, nil); err != nil {
		return err
	}
	defer os.Remove(installer)
	defer os.Remove(installer + ".log")
	java, err := h.javaForInstaller(ctx, res.JavaMajor, *s)
	if err != nil {
		return err
	}
	h.host.SetInstallStep("Mod-Loader installieren")
	h.host.Console("Starte Installer (das kann einige Minuten dauern) …")
	cmd := exec.CommandContext(ctx, java, "-jar", installer, "--installServer")
	cmd.Dir = dir
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		// installers are chatty; show a sample plus important lines
		if n%25 == 0 || strings.Contains(strings.ToLower(line), "error") || strings.Contains(line, "Successfully") {
			h.host.Console(line)
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("Installer fehlgeschlagen: %w", err)
	}
	argsName := "unix_args.txt"
	if goruntime.GOOS == "windows" && h.host.RuntimeName() != "docker" {
		argsName = "win_args.txt"
	}
	var argsFile string
	for _, base := range []string{"libraries/net/neoforged/neoforge", "libraries/net/minecraftforge/forge", "libraries/net/neoforged/forge"} {
		matches, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(base), "*", argsName))
		for _, m := range matches {
			if strings.Contains(m, res.Build) {
				argsFile = m
			}
		}
		if argsFile == "" && len(matches) > 0 {
			argsFile = matches[len(matches)-1]
		}
		if argsFile != "" {
			break
		}
	}
	if argsFile != "" {
		rel, _ := filepath.Rel(dir, argsFile)
		s.LoaderArgsFile = filepath.ToSlash(rel)
		s.Jar = ""
		h.host.Console("Mod-Loader installiert (" + s.LoaderArgsFile + ").")
		return nil
	}
	// legacy Forge (<= 1.16) produces a runnable forge-*.jar
	jars, _ := filepath.Glob(filepath.Join(dir, "forge-*.jar"))
	for _, j := range jars {
		if !strings.Contains(j, "installer") {
			s.Jar = filepath.Base(j)
			s.LoaderArgsFile = ""
			h.host.Console("Mod-Loader installiert (" + s.Jar + ").")
			return nil
		}
	}
	return errors.New("Installer lief durch, aber keine Startdatei gefunden")
}

// applyProperties writes panel-managed keys into server.properties.
func (h *Handler) applyProperties(s Settings, initial bool) error {
	srv := h.host.Server()
	p, err := LoadProperties(h.propsPath())
	if err != nil {
		return err
	}
	if srv.Port > 0 {
		p.Set("server-port", strconv.Itoa(srv.Port))
		if q, ok := p.Get("enable-query"); ok && q == "true" {
			p.Set("query.port", strconv.Itoa(srv.Port))
		}
	}
	if s.ManageRCON && s.RCONPort > 0 {
		p.Set("enable-rcon", "true")
		p.Set("rcon.port", strconv.Itoa(s.RCONPort))
		p.Set("rcon.password", s.RCONPassword)
		p.Set("broadcast-rcon-to-ops", "false")
	}
	if initial {
		if _, ok := p.Get("motd"); !ok {
			p.Set("motd", srv.Name)
		}
		if _, ok := p.Get("max-players"); !ok {
			p.Set("max-players", "20")
		}
		if _, ok := p.Get("online-mode"); !ok {
			p.Set("online-mode", "true")
		}
	}
	return os.WriteFile(h.propsPath(), p.Bytes(), 0o644)
}

// ---- starting ----

func (h *Handler) resolveJava(ctx context.Context, s Settings) (string, error) {
	major := s.JavaMajor
	if major == 0 {
		major = GuessJava(s.Version)
	}
	switch {
	case s.Java == "system" || (s.Java == "auto" && h.host.Setting("java.mode") == "system"):
		p, v := javart.System()
		if p == "" {
			return "", errors.New("Kein System-Java gefunden (java nicht im PATH)")
		}
		if v > 0 && v < major {
			h.host.Console(fmt.Sprintf("Warnung: System-Java %d ist älter als benötigt (%d).", v, major))
		}
		return p, nil
	case s.Java == "auto":
		return h.g.Java.Ensure(ctx, major, h.host.Console)
	default:
		if _, err := os.Stat(s.Java); err != nil {
			return "", fmt.Errorf("Java nicht gefunden: %s", s.Java)
		}
		return s.Java, nil
	}
}

// DockerImage returns the container image for a Java version.
func DockerImage(major int) string {
	return fmt.Sprintf("eclipse-temurin:%d-jre", normalizeJava(major))
}

func (h *Handler) Prepare(ctx context.Context, runtimeName string) (*game.Launch, error) {
	s := h.Settings()
	srv := h.host.Server()
	dir := h.host.Dir()
	if !s.EULA {
		return nil, errors.New("Die Minecraft-EULA wurde nicht akzeptiert")
	}
	os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("# Accepted via Hearth panel\neula=true\n"), 0o644)
	if err := h.applyProperties(s, false); err != nil {
		return nil, err
	}
	if s.LoaderArgsFile == "" {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(s.Jar))); err != nil {
			return nil, fmt.Errorf("Server-JAR „%s“ fehlt – bitte hochladen oder neu installieren", s.Jar)
		}
	}
	major := s.JavaMajor
	if major == 0 {
		major = GuessJava(s.Version)
	}
	launch := &game.Launch{StopTimeout: 120 * time.Second}
	java := "java"
	if runtimeName == "docker" {
		launch.Image = srv.DockerImage
		if launch.Image == "" {
			launch.Image = DockerImage(major)
		}
		if s.ManageRCON && s.RCONPort > 0 {
			launch.ExtraPorts = append(launch.ExtraPorts, runtime.Port{Host: s.RCONPort, Container: s.RCONPort, Proto: "tcp", HostIP: "127.0.0.1"})
		}
		if s.Map.Plugin != "" && s.Map.PluginPort > 0 {
			// web server of the map plugin, reachable only for the panel's map port
			launch.ExtraPorts = append(launch.ExtraPorts, runtime.Port{Host: s.Map.PluginPort, Container: s.Map.PluginPort, Proto: "tcp", HostIP: "127.0.0.1"})
		}
	} else {
		var err error
		if java, err = h.resolveJava(ctx, s); err != nil {
			return nil, err
		}
	}
	h.prepareMapPlugin(s, runtimeName)
	mem := srv.MemoryMB
	if mem <= 0 {
		mem = 2048
	}
	if s.StartupOverride != "" {
		cmdline := s.StartupOverride
		repl := strings.NewReplacer("{{java}}", quoteArg(java), "{{memory}}", strconv.Itoa(mem), "{{jar}}", s.Jar, "{{port}}", strconv.Itoa(srv.Port))
		launch.Argv = SplitArgs(repl.Replace(cmdline))
		return launch, nil
	}
	minMem := s.MinMemoryMB
	if minMem <= 0 {
		if s.FlagsPreset == "aikar" {
			minMem = mem
		} else {
			minMem = max(256, mem/4)
		}
	}
	argv := []string{java, fmt.Sprintf("-Xms%dM", minMem), fmt.Sprintf("-Xmx%dM", mem)}
	if s.FlagsPreset == "aikar" {
		argv = append(argv, aikarFlags(mem)...)
	}
	if s.Flavor == "paper" || s.Flavor == "purpur" {
		// colored console output without a TTY
		argv = append(argv, "-Dterminal.jline=false", "-Dterminal.ansi=true")
	}
	argv = append(argv, SplitArgs(s.JVMArgs)...)
	if s.LoaderArgsFile != "" {
		argv = append(argv, "@"+s.LoaderArgsFile)
	} else {
		argv = append(argv, "-jar", s.Jar)
	}
	argv = append(argv, "nogui")
	argv = append(argv, SplitArgs(s.ServerArgs)...)
	launch.Argv = argv
	return launch, nil
}

func quoteArg(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}

func (h *Handler) OnStart() {
	h.mu.Lock()
	if h.runCancel != nil {
		h.runCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.runCancel = cancel
	h.online = map[string]*OnlinePlayer{}
	h.ready = false
	h.detected = ""
	h.tps = nil
	h.positions = map[string]Position{}
	h.mu.Unlock()
	h.enqueue(job{kind: "stale"})
	if p, err := LoadProperties(h.propsPath()); err == nil {
		if n, err := strconv.Atoi(p.GetDefault("max-players", "20")); err == nil {
			h.mu.Lock()
			h.maxPlayers = n
			h.mu.Unlock()
		}
	}
	go h.rconLoop(ctx)
	go h.statsLoop(ctx)
	go h.positionLoop(ctx)
	// reattached process (Docker): already running
	if h.host.State() == "running" {
		h.mu.Lock()
		h.ready = true
		h.mu.Unlock()
		h.wakeRCON()
	}
}

func (h *Handler) wakeRCON() {
	select {
	case h.rconWake <- struct{}{}:
	default:
	}
}

func (h *Handler) Ready(line string) bool {
	return ParseLine(LogMessage(line)).Kind == LineDone
}

func (h *Handler) StopCommand() string { return "stop" }

func (h *Handler) OnStop(exitCode int, crashed bool) {
	h.mu.Lock()
	if h.runCancel != nil {
		h.runCancel()
		h.runCancel = nil
	}
	players := make([]*OnlinePlayer, 0, len(h.online))
	for _, p := range h.online {
		players = append(players, p)
	}
	h.online = map[string]*OnlinePlayer{}
	h.ready = false
	h.positions = map[string]Position{}
	rc := h.rcon
	h.rcon = nil
	h.mu.Unlock()
	if rc != nil {
		rc.Close()
	}
	now := time.Now().Unix()
	for _, p := range players {
		h.enqueue(job{kind: "leave", player: p, silent: true, at: now})
	}
	h.host.Publish(events.Players, []OnlinePlayer{})
	go func() {
		time.Sleep(2 * time.Second)
		h.ImportStats(context.Background())
	}()
}

func (h *Handler) Close() {
	h.closeOnce.Do(func() {
		h.mu.Lock()
		if h.runCancel != nil {
			h.runCancel()
		}
		if h.rcon != nil {
			h.rcon.Close()
		}
		h.mu.Unlock()
		close(h.closed)
	})
}

// ---- console parsing ----

func (h *Handler) isOnlineName(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.online[strings.ToLower(name)]
	return ok
}

func (h *Handler) OnLine(line string) {
	msg := LogMessage(line)
	if msg == "" {
		return
	}
	p := ParseLine(msg)
	switch p.Kind {
	case LineUUID:
		h.mu.Lock()
		h.uuids[strings.ToLower(p.Name)] = p.UUID
		h.mu.Unlock()
	case LineLogin, LineJoin:
		h.playerJoin(p.Name, "")
	case LineLeave, LineLostConn:
		h.playerLeave(p.Name)
	case LineChat:
		if h.Settings().TrackChat {
			uuid := h.uuidOf(p.Name)
			h.enqueue(job{kind: "event", event: &store.MCEvent{Type: "chat", UUID: uuid, Name: p.Name, Message: p.Text}})
		}
	case LineAdvancement:
		uuid := h.uuidOf(p.Name)
		data, _ := json.Marshal(map[string]string{"title": p.Text, "kind": p.AdvType})
		h.enqueue(job{kind: "event", event: &store.MCEvent{Type: "advancement", UUID: uuid, Name: p.Name, Message: msg, Data: data}})
	case LineDone:
		h.mu.Lock()
		h.ready = true
		h.mu.Unlock()
		h.wakeRCON()
	case LineVersion:
		h.mu.Lock()
		h.detected = p.Text
		h.mu.Unlock()
	case LineSaved:
		select {
		case h.saved <- struct{}{}:
		default:
		}
	case LineBindFailed:
		h.host.Console("Der Port ist bereits belegt. Läuft der Server schon woanders oder nutzt ein anderer Dienst den Port?")
	case LineEULA:
		h.host.Console("Die EULA ist nicht akzeptiert. Akzeptiere sie in den Servereinstellungen.")
	default:
		if d := ParseDeath(msg, h.isOnlineName); d != nil {
			uuid := h.uuidOf(d.Victim)
			data, _ := json.Marshal(d)
			h.enqueue(job{kind: "event", event: &store.MCEvent{Type: "death", UUID: uuid, Name: d.Victim, Message: msg, Data: data}})
		}
	}
}

func (h *Handler) uuidOf(name string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p, ok := h.online[strings.ToLower(name)]; ok {
		return p.UUID
	}
	return h.uuids[strings.ToLower(name)]
}

func (h *Handler) playerJoin(name, uuid string) {
	if !ValidPlayerName(name) {
		return
	}
	key := strings.ToLower(name)
	h.mu.Lock()
	if _, ok := h.online[key]; ok {
		h.mu.Unlock()
		return
	}
	if uuid == "" {
		uuid = h.uuids[key]
	}
	h.mu.Unlock()
	if uuid == "" {
		for u, n := range ReadUsercache(h.host.Dir()) {
			if strings.EqualFold(n, name) {
				uuid = u
				break
			}
		}
	}
	if uuid == "" {
		uuid = OfflineUUID(name)
	}
	p := &OnlinePlayer{UUID: uuid, Name: name, Since: time.Now().Unix()}
	h.mu.Lock()
	if _, ok := h.online[key]; ok {
		h.mu.Unlock()
		return
	}
	h.online[key] = p
	h.uuids[key] = uuid
	h.mu.Unlock()
	h.enqueue(job{kind: "join", player: p, at: p.Since})
	h.publishOnline()
}

func (h *Handler) playerLeave(name string) {
	key := strings.ToLower(name)
	h.mu.Lock()
	p, ok := h.online[key]
	if ok {
		delete(h.online, key)
		delete(h.positions, p.UUID)
	}
	h.mu.Unlock()
	if !ok {
		return
	}
	h.enqueue(job{kind: "leave", player: p, at: time.Now().Unix()})
	h.publishOnline()
	select {
	case h.statsSoon <- struct{}{}:
	default:
	}
}

func (h *Handler) publishOnline() {
	h.host.Publish(events.Players, h.OnlinePlayers())
}

// OnlinePlayers returns the currently online players.
func (h *Handler) OnlinePlayers() []OnlinePlayer {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]OnlinePlayer, 0, len(h.online))
	for _, p := range h.online {
		out = append(out, OnlinePlayer{UUID: p.UUID, Name: p.Name, Since: p.Since})
	}
	sortPlayers(out)
	return out
}

func sortPlayers(p []OnlinePlayer) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && strings.ToLower(p[j].Name) < strings.ToLower(p[j-1].Name); j-- {
			p[j], p[j-1] = p[j-1], p[j]
		}
	}
}

func (h *Handler) PlayerCount() (int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.online), h.maxPlayers
}

func (h *Handler) enqueue(j job) {
	select {
	case h.jobs <- j:
	default:
		h.log.Warn("minecraft event queue full, dropping", "kind", j.kind)
	}
}

// worker persists player sessions and events in order.
func (h *Handler) worker() {
	ctx := context.Background()
	st := h.host.Store()
	id := h.host.ServerID()
	for {
		var j job
		select {
		case <-h.closed:
			return
		case j = <-h.jobs:
		}
		switch j.kind {
		case "stale":
			st.CloseStaleMCSessions(ctx, id)
		case "join":
			p := j.player
			if err := st.PlayerSeen(ctx, id, p.UUID, p.Name, j.at, true); err != nil {
				h.log.Warn("player seen", "err", err)
			}
			sid, err := st.StartMCSession(ctx, id, p.UUID, j.at)
			if err == nil {
				h.mu.Lock()
				p.sessionID = sid
				h.mu.Unlock()
			}
			ev := &store.MCEvent{At: j.at, Type: "join", UUID: p.UUID, Name: p.Name}
			st.AddMCEvent(ctx, id, ev)
			h.host.Publish(events.MCEvent, ev)
		case "leave":
			p := j.player
			h.mu.Lock()
			sid := p.sessionID
			h.mu.Unlock()
			if sid > 0 {
				st.EndMCSession(ctx, id, sid, j.at)
			}
			data, _ := json.Marshal(map[string]any{"duration": j.at - p.Since, "silent": j.silent})
			ev := &store.MCEvent{At: j.at, Type: "leave", UUID: p.UUID, Name: p.Name, Data: data}
			st.AddMCEvent(ctx, id, ev)
			if !j.silent {
				h.host.Publish(events.MCEvent, ev)
			}
		case "event":
			ev := j.event
			if ev.At == 0 {
				ev.At = time.Now().Unix()
			}
			if ev.UUID != "" {
				st.PlayerSeen(ctx, id, ev.UUID, ev.Name, ev.At, false)
			}
			st.AddMCEvent(ctx, id, ev)
			h.host.Publish(events.MCEvent, ev)
		}
	}
}

// ---- RCON ----

func (h *Handler) rconClient() *RCON {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rcon
}

// RCONExec runs a command over RCON (silent, not shown in the console).
func (h *Handler) RCONExec(cmd string) (string, error) {
	rc := h.rconClient()
	if rc == nil {
		return "", errors.New("RCON nicht verbunden")
	}
	out, err := rc.Exec(cmd)
	if err != nil {
		h.mu.Lock()
		if h.rcon == rc {
			h.rcon = nil
			h.rconErr = err.Error()
		}
		h.mu.Unlock()
		rc.Close()
		h.wakeRCON()
	}
	return StripLegacy(out), err
}

func (h *Handler) rconLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.closed:
			return
		case <-ticker.C:
		case <-h.rconWake:
		}
		h.mu.Lock()
		ready := h.ready
		connected := h.rcon != nil
		h.mu.Unlock()
		if !ready {
			continue
		}
		s := h.Settings()
		if !connected && s.ManageRCON && s.RCONPort > 0 {
			rc, err := DialRCON(fmt.Sprintf("127.0.0.1:%d", s.RCONPort), s.RCONPassword, 5*time.Second)
			h.mu.Lock()
			if err != nil {
				h.rconErr = err.Error()
			} else {
				h.rcon, h.rconErr = rc, ""
				connected = true
			}
			h.mu.Unlock()
		}
		if connected {
			h.syncPlayerList()
			if s.Flavor == "paper" || s.Flavor == "purpur" {
				h.pollTPS()
			}
		}
	}
}

var reList = regexp.MustCompile(`There are (\d+) of a max(?:imum)? of (\d+) players online:?(.*)`)
var reListOld = regexp.MustCompile(`There are (\d+)/(\d+) players online:?(.*)`)
var reListEntry = regexp.MustCompile(`^(\S+?)(?: \(([0-9a-fA-F-]{36})\))?$`)

func (h *Handler) syncPlayerList() {
	out, err := h.RCONExec("list uuids")
	if err != nil {
		return
	}
	out = strings.ReplaceAll(out, "\n", " ")
	m := reList.FindStringSubmatch(out)
	if m == nil {
		m = reListOld.FindStringSubmatch(out)
	}
	if m == nil {
		return
	}
	if maxP, err := strconv.Atoi(m[2]); err == nil {
		h.mu.Lock()
		h.maxPlayers = maxP
		h.mu.Unlock()
	}
	present := map[string]bool{}
	for _, part := range strings.Split(m[3], ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		em := reListEntry.FindStringSubmatch(part)
		if em == nil {
			continue
		}
		name, uuid := em[1], NormalizeUUID(em[2])
		present[strings.ToLower(name)] = true
		if uuid != "" {
			h.mu.Lock()
			h.uuids[strings.ToLower(name)] = uuid
			h.mu.Unlock()
		}
		if !h.isOnlineName(name) {
			h.playerJoin(name, uuid)
		}
	}
	var gone []string
	h.mu.Lock()
	for key, p := range h.online {
		if present[key] {
			p.missing = 0
			continue
		}
		p.missing++
		if p.missing >= 2 {
			gone = append(gone, p.Name)
		}
	}
	h.mu.Unlock()
	for _, n := range gone {
		h.playerLeave(n)
	}
}

var reFloat = regexp.MustCompile(`\*?(\d+(?:\.\d+)?)`)

func (h *Handler) pollTPS() {
	out, err := h.RCONExec("tps")
	if err != nil {
		return
	}
	i := strings.Index(out, ":")
	if i < 0 {
		return
	}
	var tps []float64
	for _, m := range reFloat.FindAllStringSubmatch(out[i+1:], 3) {
		if f, err := strconv.ParseFloat(m[1], 64); err == nil {
			tps = append(tps, f)
		}
	}
	h.mu.Lock()
	h.tps = tps
	h.mu.Unlock()
}

// ---- positions ----

var rePos = regexp.MustCompile(`entity data: \[(-?[\d.E-]+)d, (-?[\d.E-]+)d, (-?[\d.E-]+)d\]`)
var reDim = regexp.MustCompile(`entity data: "([^"]+)"`)

// Positions returns player positions; calling it keeps live polling active for a minute.
func (h *Handler) Positions() []Position {
	h.mu.Lock()
	h.posWanted = time.Now()
	out := make([]Position, 0, len(h.positions))
	for _, p := range h.positions {
		out = append(out, p)
	}
	h.mu.Unlock()
	return out
}

func (h *Handler) positionLoop(ctx context.Context) {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	var lastFile time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.closed:
			return
		case <-ticker.C:
		}
		h.mu.Lock()
		wanted := time.Since(h.posWanted) < time.Minute
		h.mu.Unlock()
		if !wanted {
			continue
		}
		players := h.OnlinePlayers()
		s := h.Settings()
		if s.Map.LivePlayers && h.rconClient() != nil {
			for _, p := range players {
				pos, err := h.RCONExec("data get entity " + p.Name + " Pos")
				if err != nil {
					break
				}
				m := rePos.FindStringSubmatch(pos)
				if m == nil {
					continue
				}
				dim := "minecraft:overworld"
				if d, err := h.RCONExec("data get entity " + p.Name + " Dimension"); err == nil {
					if dm := reDim.FindStringSubmatch(d); dm != nil {
						dim = dm[1]
					}
				}
				x, _ := strconv.ParseFloat(m[1], 64)
				y, _ := strconv.ParseFloat(m[2], 64)
				z, _ := strconv.ParseFloat(m[3], 64)
				h.mu.Lock()
				h.positions[p.UUID] = Position{UUID: p.UUID, Name: p.Name, X: x, Y: y, Z: z, Dim: dim, At: time.Now().Unix(), Live: true}
				h.mu.Unlock()
			}
			continue
		}
		// fallback: last saved position from playerdata (updated on autosave)
		if time.Since(lastFile) < 30*time.Second {
			continue
		}
		lastFile = time.Now()
		w := h.World()
		for _, p := range players {
			c, err := nbt.ReadFile(filepath.Join(w.PlayerDataDir, p.UUID+".dat"), map[string]bool{"Inventory": true, "EnderItems": true, "recipeBook": true, "Brain": true, "attributes": true, "Attributes": true})
			if err != nil {
				continue
			}
			pos := nbt.List(c["Pos"])
			if len(pos) != 3 {
				continue
			}
			x, _ := nbt.Float(pos[0])
			y, _ := nbt.Float(pos[1])
			z, _ := nbt.Float(pos[2])
			dim := nbt.String(c["Dimension"])
			if dim == "" {
				if n, ok := nbt.Int(c["Dimension"]); ok { // pre-1.16 numeric ids
					dim = map[int64]string{-1: "minecraft:the_nether", 0: "minecraft:overworld", 1: "minecraft:the_end"}[n]
				}
			}
			h.mu.Lock()
			h.positions[p.UUID] = Position{UUID: p.UUID, Name: p.Name, X: x, Y: y, Z: z, Dim: dim, At: time.Now().Unix()}
			h.mu.Unlock()
		}
	}
}

// ---- stats ----

func (h *Handler) statsLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.closed:
			return
		case <-ticker.C:
		case <-h.statsSoon:
			// the server writes a player's stats when they leave; give it a moment
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return
			}
		}
		h.ImportStats(ctx)
	}
}

// ImportStats reads changed stats and advancement files into the database.
func (h *Handler) ImportStats(ctx context.Context) (int, error) {
	st := h.host.Store()
	id := h.host.ServerID()
	w := h.World()
	mtimes, err := st.MCStatsMTimes(ctx, id)
	if err != nil {
		return 0, err
	}
	names := ReadUsercache(h.host.Dir())
	n := 0
	for _, f := range listUUIDFiles(w.StatsDir) {
		prev := mtimes[f.UUID]
		if f.MTime.Unix() <= prev[0] {
			if name := names[f.UUID]; name != "" {
				st.RenameMCPlayer(ctx, id, f.UUID, name)
			}
			continue
		}
		b, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		u, err := ParseStats(f.UUID, b)
		if err != nil {
			continue
		}
		u.Name = names[f.UUID]
		u.MTime = f.MTime.Unix()
		if err := st.UpdateMCStats(ctx, id, u); err == nil {
			n++
		}
	}
	for _, f := range listUUIDFiles(w.AdvDir) {
		prev := mtimes[f.UUID]
		if f.MTime.Unix() <= prev[1] {
			continue
		}
		b, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		st.UpdateMCAdvancements(ctx, id, f.UUID, CountAdvancements(b), f.MTime.Unix())
	}
	return n, nil
}

// ---- backups ----

func (h *Handler) PrepareBackup(ctx context.Context) func() {
	h.mu.Lock()
	ready := h.ready
	h.mu.Unlock()
	if !ready {
		return func() {}
	}
	if h.rconClient() != nil {
		if _, err := h.RCONExec("save-off"); err == nil {
			h.RCONExec("save-all flush")
			return func() { h.RCONExec("save-on") }
		}
	}
	select {
	case <-h.saved:
	default:
	}
	h.host.SendCommand("save-off")
	h.host.SendCommand("save-all flush")
	select {
	case <-h.saved:
	case <-ctx.Done():
	case <-time.After(60 * time.Second):
	}
	return func() { h.host.SendCommand("save-on") }
}

// ---- status ----

func (h *Handler) Info() any {
	s := h.Settings()
	h.mu.Lock()
	info := Info{
		Flavor: s.Flavor, Version: s.Version, Build: s.InstalledBuild, Detected: h.detected,
		MaxPlayers: h.maxPlayers, RCON: h.rcon != nil, RCONError: h.rconErr, TPS: h.tps, Ready: h.ready,
		JavaMajor: s.JavaMajor, ModDir: ModDir(s.Flavor),
	}
	h.mu.Unlock()
	info.Online = h.OnlinePlayers()
	info.World = h.LevelName()
	return info
}

// Ping queries the running server via Server List Ping (cached for 20s).
func (h *Handler) Ping(ctx context.Context) (*PingResult, error) {
	h.mu.Lock()
	if h.ping != nil && time.Since(h.pingAt) < 20*time.Second {
		p := h.ping
		h.mu.Unlock()
		return p, nil
	}
	h.mu.Unlock()
	srv := h.host.Server()
	if !h.host.Running() {
		return nil, errors.New("Server ist offline")
	}
	p, err := Ping(ctx, "127.0.0.1", srv.Port)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.ping, h.pingAt = p, time.Now()
	h.mu.Unlock()
	return p, nil
}

// SetProperties updates server.properties (managed keys are ignored).
func (h *Handler) SetProperties(values map[string]string) error {
	p, err := LoadProperties(h.propsPath())
	if err != nil {
		return err
	}
	for k, v := range values {
		if IsManagedKey(k) || strings.ContainsAny(k, "=:\n\r ") || k == "" {
			continue
		}
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("Wert für %s darf keine Zeilenumbrüche enthalten", k)
		}
		p.Set(k, v)
	}
	return os.WriteFile(h.propsPath(), p.Bytes(), 0o644)
}

func (h *Handler) Properties() (map[string]string, error) {
	p, err := LoadProperties(h.propsPath())
	if err != nil {
		return nil, err
	}
	m := p.Map()
	delete(m, "rcon.password")
	return m, nil
}
