package minecraft

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// FlushWorld asks a running server to write all loaded chunks to disk and
// waits until it confirms, so the map renderer reads current region files.
// Without RCON it returns immediately (the regular autosave still applies).
func (h *Handler) FlushWorld(ctx context.Context) {
	h.mu.Lock()
	ready := h.ready
	h.mu.Unlock()
	if !ready || h.rconClient() == nil {
		return
	}
	select {
	case <-h.saved:
	default:
	}
	out, err := h.RCONExec("save-all flush")
	if err != nil || strings.Contains(out, "Saved the game") || strings.Contains(out, "Saved the world") {
		return
	}
	// some forks confirm asynchronously in the log
	select {
	case <-h.saved:
	case <-ctx.Done():
	case <-time.After(30 * time.Second):
	}
}

// MapPluginState describes the map plugin of a server.
type MapPluginState struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Port      int    `json:"port"`
	File      string `json:"file"`      // installed jar ("" = missing)
	Enabled   bool   `json:"enabled"`   // jar not disabled
	Supported bool   `json:"supported"` // available for this server type
}

// MapPlugin returns the state of the configured map plugin (nil if none).
func (h *Handler) MapPlugin() *MapPluginState {
	s := h.Settings()
	if s.Map.Plugin == "" {
		return nil
	}
	p, ok := MapPluginByID(s.Map.Plugin)
	if !ok {
		return nil
	}
	st := &MapPluginState{ID: p.ID, Name: p.Name, Port: s.Map.PluginPort, Supported: p.Supports(s.Flavor)}
	st.File, st.Enabled = FindMapPluginJar(h.host.Dir(), s.Flavor, p.ID)
	return st
}

// InstallMapPlugin downloads the configured map plugin (with its required
// dependencies) from Modrinth and writes its configuration.
func (h *Handler) InstallMapPlugin(ctx context.Context) error {
	s := h.Settings()
	p, ok := MapPluginByID(s.Map.Plugin)
	if !ok {
		return errors.New("kein Karten-Plugin gewählt")
	}
	if !p.Supports(s.Flavor) {
		return fmt.Errorf("%s gibt es nicht für diese Server-Art", p.Name)
	}
	if s.Map.PluginPort <= 0 {
		return errors.New("für die Karte ist kein Port vergeben")
	}
	if file, enabled := FindMapPluginJar(h.host.Dir(), s.Flavor, p.ID); file != "" && enabled {
		h.host.Console(p.Name + " ist bereits installiert (" + file + ").")
		if err := ConfigureMapPlugin(h.host.Dir(), s.Flavor, p.ID, s.Map.PluginPort, MapPluginBind(h.host.RuntimeName())); err != nil {
			return fmt.Errorf("%s einrichten: %w", p.Name, err)
		}
	} else if err := h.installMapPluginWith(ctx, s); err != nil {
		return err
	}
	h.host.Console(p.Name + " ist eingerichtet. Die Karte erscheint nach dem nächsten Start unter „Karte“.")
	return nil
}

// installMapPluginWith is InstallMapPlugin for settings that are not stored yet
// (during the installation of the server).
func (h *Handler) installMapPluginWith(ctx context.Context, s Settings) error {
	p, ok := MapPluginByID(s.Map.Plugin)
	if !ok || !p.Supports(s.Flavor) || s.Map.PluginPort <= 0 {
		return nil
	}
	dir := filepath.Join(h.host.Dir(), ModDir(s.Flavor))
	version := s.InstalledVersion
	if version == "" {
		version = s.Version
	}
	h.host.Console(fmt.Sprintf("Installiere %s für Minecraft %s …", p.Name, version))
	files, err := h.g.InstallProject(ctx, InstallRequest{
		Dir: dir, Project: p.Project, Loaders: ModrinthLoaders(s.Flavor), GameVersion: version,
		Dependencies: true, Log: h.host.Console,
	})
	if err != nil {
		return fmt.Errorf("%s konnte nicht installiert werden: %w", p.Name, err)
	}
	if len(files) > 0 {
		h.host.Console("Installiert: " + strings.Join(files, ", "))
	}
	if err := ConfigureMapPlugin(h.host.Dir(), s.Flavor, p.ID, s.Map.PluginPort, MapPluginBind(h.host.RuntimeName())); err != nil {
		return fmt.Errorf("%s einrichten: %w", p.Name, err)
	}
	return nil
}

// prepareMapPlugin keeps the plugin's web server on the port the panel expects
// (runs before every start; the runtime may have changed).
func (h *Handler) prepareMapPlugin(s Settings, runtimeName string) {
	if s.Map.Plugin == "" || s.Map.PluginPort <= 0 {
		return
	}
	p, ok := MapPluginByID(s.Map.Plugin)
	if !ok || !p.Supports(s.Flavor) {
		return
	}
	dir := h.host.Dir()
	if file, enabled := FindMapPluginJar(dir, s.Flavor, p.ID); file == "" || !enabled {
		h.host.Console(p.Name + " ist nicht installiert oder deaktiviert. Unter „Karte“ kannst du es neu installieren.")
		return
	}
	if err := ConfigureMapPlugin(dir, s.Flavor, p.ID, s.Map.PluginPort, MapPluginBind(runtimeName)); err != nil {
		h.host.Console("Karten-Plugin konnte nicht eingerichtet werden: " + err.Error())
	}
}
