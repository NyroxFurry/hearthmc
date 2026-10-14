// Package app wires all components together and runs the panel.
package app

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"hearth/internal/api"
	"hearth/internal/auth"
	"hearth/internal/backup"
	"hearth/internal/config"
	"hearth/internal/db"
	"hearth/internal/events"
	"hearth/internal/generic"
	"hearth/internal/httpx"
	"hearth/internal/javart"
	"hearth/internal/mapr"
	"hearth/internal/minecraft"
	"hearth/internal/notify"
	"hearth/internal/sched"
	"hearth/internal/servers"
	"hearth/internal/settings"
	"hearth/internal/store"
	"hearth/internal/version"
	"hearth/internal/web"
)

type App struct {
	Cfg      *config.Config
	Log      *slog.Logger
	DB       *sql.DB
	Store    *store.Store
	Settings *settings.Service
	Bus      *events.Bus
	Servers  *servers.Manager
	Backups  *backup.Service
	Sched    *sched.Scheduler
	Notify   *notify.Notifier
	Maps     *mapr.Service
	API      *api.API
	HTTP     *httpx.Client
	Java     *javart.Manager
	MC       *minecraft.Game
}

func New(cfg *config.Config, log *slog.Logger) (*App, error) {
	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}
	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil, err
	}
	st := store.New(database)
	set, err := settings.New(st)
	if err != nil {
		return nil, err
	}
	a := &App{Cfg: cfg, Log: log, DB: database, Store: st, Settings: set, Bus: events.NewBus()}
	a.HTTP = httpx.New(func() string { return set.Get(settings.Contact) })
	a.Java = &javart.Manager{Dir: cfg.JavaDir(), HTTP: a.HTTP}
	a.MC = &minecraft.Game{
		Versions: &minecraft.Versions{HTTP: a.HTTP},
		Java:     a.Java,
		HTTP:     a.HTTP,
		Modrinth: &minecraft.Modrinth{HTTP: a.HTTP},
		Heads:    &minecraft.Heads{HTTP: a.HTTP, Dir: filepath.Join(cfg.CacheDir(), "heads")},
	}
	a.Servers = servers.NewManager(cfg, st, a.Bus, set, log, a.MC, generic.Game{})
	a.Notify = &notify.Notifier{Store: st, Settings: set, Log: log}
	a.Backups = &backup.Service{Dir: cfg.BackupsDir, Store: st, Servers: a.Servers, Bus: a.Bus, Settings: set, Log: log}
	a.Sched = &sched.Scheduler{Store: st, Servers: a.Servers, Backups: a.Backups, Settings: set, Log: log}
	a.Maps = &mapr.Service{Dir: cfg.MapsDir(), Log: log, Threads: func() int { return set.Int(settings.MapThreads) }}
	a.Maps.OnProgress = func(serverID string, p mapr.Progress) {
		a.Bus.Publish(events.Event{Type: events.Map, Server: serverID, Data: p})
	}
	webUI := web.New()
	a.API = &api.API{
		Cfg: cfg, Store: st, Settings: set, Servers: a.Servers, Backups: a.Backups, Sched: a.Sched,
		Notify: a.Notify, Bus: a.Bus, Java: a.Java, MC: a.MC, Maps: a.Maps, Log: log,
		Static: webUI, Index: webUI.Index,
	}
	a.API.Init()
	webUI.ExtraConnect = a.API.MapOrigin
	a.Servers.AuditFunc = a.API.SystemAudit
	a.Servers.NotifyFunc = a.API.LifecycleNotify
	a.Backups.Notify = a.API.LifecycleNotify
	a.Sched.Audit = a.API.SystemAudit
	return a, nil
}

// SetupCode creates a one-time code for the first-run wizard if no user exists.
func (a *App) SetupCode(ctx context.Context) (string, error) {
	n, err := a.Store.CountUsers(ctx)
	if err != nil || n > 0 {
		os.Remove(filepath.Join(a.Cfg.DataDir, "setup-code.txt"))
		return "", err
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	rand.Read(b)
	var sb strings.Builder
	for i, c := range b {
		if i == 4 {
			sb.WriteByte('-')
		}
		sb.WriteByte(alphabet[int(c)%len(alphabet)])
	}
	code := sb.String()
	if err := a.Settings.Set(ctx, settings.SetupToken, auth.HashToken(code)); err != nil {
		return "", err
	}
	os.WriteFile(filepath.Join(a.Cfg.DataDir, "setup-code.txt"), []byte(code+"\n"), 0o600)
	return code, nil
}

// Run starts everything and blocks until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	a.Store.MarkRunningBackupsFailed(ctx)
	code, err := a.SetupCode(ctx)
	if err != nil {
		return err
	}
	bg, cancelBG := context.WithCancel(context.Background())
	defer cancelBG()
	a.Notify.Start(bg)
	if err := a.Servers.Load(ctx); err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); a.Sched.Run(bg) }()
	go func() { defer wg.Done(); a.API.NotifyFromBus(bg) }()
	go func() { defer wg.Done(); a.maintenance(bg) }()

	handler := a.API.Handler()
	srvs, urls, err := a.httpServers(handler)
	if err != nil {
		return err
	}
	errCh := make(chan error, len(srvs))
	for _, s := range srvs {
		go func(s *httpServer) {
			a.Log.Info("listening", "addr", s.Addr, "tls", s.tls, "map", s.optional)
			var err error
			if s.tls {
				err = s.ListenAndServeTLS("", "")
			} else {
				err = s.ListenAndServe()
			}
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				if s.optional {
					// the map port is a convenience; the panel keeps running without it
					a.Log.Warn("Kartenport nicht verfügbar", "addr", s.Addr, "err", err)
					a.API.SetMapListenError(err)
					return
				}
				errCh <- err
			}
		}(s)
	}
	a.Log.Info("Hearth gestartet", "version", version.Version, "url", strings.Join(urls, ", "))
	if code != "" {
		banner := strings.Repeat("═", 60)
		fmt.Fprintf(os.Stderr, "\n%s\n  Willkommen bei Hearth!\n  Öffne %s und richte dein Admin-Konto ein.\n  Setup-Code: %s\n%s\n\n", banner, urls[0], code, banner)
	}

	select {
	case <-ctx.Done():
	case err = <-errCh:
		a.Log.Error("http server failed", "err", err)
	}
	a.Log.Info("Hearth wird beendet …")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	for _, s := range srvs {
		s.Shutdown(shutdownCtx)
	}
	cancel()
	a.Servers.Shutdown(90 * time.Second)
	cancelBG()
	wg.Wait()
	a.DB.Close()
	return err
}

type httpServer struct {
	*http.Server
	tls      bool
	optional bool // map port: failing to listen is logged, not fatal
}

func (a *App) httpServers(h http.Handler) ([]*httpServer, []string, error) {
	base := func(addr string, handler http.Handler) *http.Server {
		return &http.Server{
			Addr: addr, Handler: handler, ReadHeaderTimeout: 20 * time.Second, IdleTimeout: 120 * time.Second,
			ErrorLog: slog.NewLogLogger(a.Log.Handler(), slog.LevelDebug),
		}
	}
	cfg := a.Cfg
	switch {
	case cfg.TLSDomain != "":
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.TLSDomain),
			Cache:      autocert.DirCache(cfg.TLSCacheDir()),
			Email:      cfg.TLSEmail,
		}
		https := base(":443", h)
		https.TLSConfig = m.TLSConfig()
		https.TLSConfig.MinVersion = tls.VersionTLS12
		redirect := base(":80", m.HTTPHandler(nil))
		out := []*httpServer{{Server: https, tls: true}, {Server: redirect}}
		if addr := cfg.MapAddr(); addr != "" {
			ms := base(addr, a.API.MapProxy())
			ms.TLSConfig = https.TLSConfig.Clone()
			out = append(out, &httpServer{Server: ms, tls: true, optional: true})
		}
		return out, []string{"https://" + cfg.TLSDomain}, nil
	case cfg.TLSCert != "" && cfg.TLSKey != "":
		s := base(cfg.Listen, h)
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return nil, nil, fmt.Errorf("TLS-Zertifikat: %w", err)
		}
		s.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
		out := []*httpServer{{Server: s, tls: true}}
		if addr := cfg.MapAddr(); addr != "" {
			ms := base(addr, a.API.MapProxy())
			ms.TLSConfig = s.TLSConfig.Clone()
			out = append(out, &httpServer{Server: ms, tls: true, optional: true})
		}
		return out, []string{"https://" + displayAddr(cfg.Listen)}, nil
	default:
		out := []*httpServer{{Server: base(cfg.Listen, h)}}
		if addr := cfg.MapAddr(); addr != "" {
			out = append(out, &httpServer{Server: base(addr, a.API.MapProxy()), optional: true})
		}
		return out, []string{"http://" + displayAddr(cfg.Listen)}, nil
	}
}

// displayAddr turns ":8080" into "<host-ip>:8080" for log output.
func displayAddr(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
		if ip := outboundIP(); ip != "" {
			host = ip
		}
	}
	return net.JoinHostPort(host, port)
}

func outboundIP() string {
	conn, err := net.Dial("udp", "1.1.1.1:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// maintenance runs periodic jobs: data retention and automatic map rendering.
func (a *App) maintenance(ctx context.Context) {
	cleanup := func() {
		a.Store.Cleanup(ctx, a.Settings.Int(settings.MetricsRetention), a.Settings.Int(settings.AuditRetention), a.Settings.Int(settings.ChatRetention))
	}
	cleanup()
	maps := &mapScheduler{app: a, last: map[string]time.Time{}, wasRunning: map[string]bool{}}
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		n++
		if n%(6*60) == 0 {
			cleanup()
		}
		maps.tick(ctx)
	}
}

// mapScheduler renders the built-in maps while servers run. A new world gets
// its first map shortly after the start (and retries until region files
// exist); afterwards the map follows the configured interval. When a server
// stops, its map is updated once more from the final save.
type mapScheduler struct {
	app        *App
	last       map[string]time.Time // last render attempt
	wasRunning map[string]bool
}

func (m *mapScheduler) tick(ctx context.Context) {
	a := m.app
	for _, inst := range a.Servers.List() {
		h, ok := inst.Handler().(*minecraft.Handler)
		if !ok {
			continue
		}
		id := inst.ID()
		running := inst.Running()
		stopped := m.wasRunning[id] && !running
		m.wasRunning[id] = running
		s := h.Settings()
		if !s.Map.Enabled || s.Map.IntervalMinutes <= 0 || (!running && !stopped) {
			continue
		}
		specs := mapSpecs(h, s)
		if len(specs) == 0 {
			continue // no region files yet: try again next minute
		}
		hasTiles := false
		lastRender := time.Time{}
		for _, d := range specs {
			info := a.Maps.Info(id, d.Key)
			if info.Regions > 0 {
				hasTiles = true
			}
			if t := time.Unix(info.LastRender, 0); info.LastRender > 0 && t.After(lastRender) {
				lastRender = t
			}
		}
		if t := m.last[id]; t.After(lastRender) {
			lastRender = t
		}
		due := stopped
		switch {
		case due:
		case !hasTiles:
			// first map: as soon as the world has been saved, then every two minutes until it shows something
			due = time.Since(lastRender) >= 2*time.Minute && time.Since(time.Unix(inst.Status().StartedAt, 0)) >= 45*time.Second
		default:
			due = time.Since(lastRender) >= time.Duration(s.Map.IntervalMinutes)*time.Minute
		}
		if !due {
			continue
		}
		if running {
			fctx, cancel := context.WithTimeout(ctx, 45*time.Second)
			h.FlushWorld(fctx)
			cancel()
		}
		if err := a.Maps.Render(id, specs, false); err == nil || errors.Is(err, mapr.ErrRunning) {
			m.last[id] = time.Now()
		}
	}
}

func mapSpecs(h *minecraft.Handler, s minecraft.Settings) []mapr.DimSpec {
	want := map[string]bool{}
	for _, d := range s.Map.Dimensions {
		want[d] = true
	}
	var specs []mapr.DimSpec
	for _, d := range h.World().Dimensions {
		if (len(want) == 0 || want[d.Key]) && d.Regions > 0 {
			specs = append(specs, mapr.DimSpec{Key: d.Key, RegionDir: d.RegionDir, Nether: d.ID == "minecraft:the_nether"})
		}
	}
	return specs
}
