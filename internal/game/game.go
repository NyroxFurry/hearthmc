// Package game defines the interface between the generic server manager and
// game specific integrations (Minecraft, generic command, ...).
//
// To add support for a new game, implement Game and Handler and register the
// game in the manager. Everything else (console, files, backups, schedules,
// permissions, webhooks) works for every game automatically.
package game

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"hearth/internal/runtime"
	"hearth/internal/store"
)

// Host is the view of a server instance that a game handler can use.
type Host interface {
	ServerID() string
	Dir() string
	// Server returns a snapshot of the current server configuration.
	Server() *store.Server
	// UpdateSettings atomically modifies and persists the game specific settings JSON.
	UpdateSettings(fn func(settings json.RawMessage) (json.RawMessage, error)) error
	// SendCommand writes a line to the process' stdin (no-op when offline).
	SendCommand(cmd string) error
	// Console prints a panel message into the server console.
	Console(msg string)
	// Publish sends a live event for this server to WebSocket clients and webhooks.
	Publish(eventType string, data any)
	Store() *store.Store
	State() string
	Running() bool
	RuntimeName() string
	Logger() *slog.Logger
	// DataDir returns a panel-managed directory for this server outside of its
	// files (e.g. map tiles), creating it if needed.
	DataDir(kind string) string
	Setting(key string) string
	// SetInstallStep shows a short progress label during installation.
	SetInstallStep(step string)
}

// Launch describes how to start the server process.
type Launch struct {
	Argv        []string // native: program path + args; docker: argv inside the container (cwd /data)
	Env         []string
	Image       string         // docker image
	ExtraPorts  []runtime.Port // additional published ports (e.g. RCON bound to 127.0.0.1)
	StopTimeout time.Duration  // how long to wait after the stop command
}

// Handler is created per server instance.
type Handler interface {
	// Install prepares the server files (download, unpack, configure). Output via Host.Console.
	Install(ctx context.Context) error
	// Prepare is called before each start and returns the launch parameters.
	Prepare(ctx context.Context, runtimeName string) (*Launch, error)
	// OnStart is called after the process has been started.
	OnStart()
	// OnLine receives every console line (may contain ANSI escapes).
	OnLine(line string)
	// Ready reports whether a console line indicates the server finished starting.
	// Return true on the first line if the game has no such marker.
	Ready(line string) bool
	// StopCommand returns the console command for a graceful stop ("" = send SIGTERM).
	StopCommand() string
	// OnStop is called after the process exited.
	OnStop(exitCode int, crashed bool)
	// Info returns live game information for the API (players, version, ...).
	Info() any
	// Close releases resources (server deleted or panel shutting down).
	Close()
}

// PlayerCounter is optionally implemented by handlers that know their online player count.
type PlayerCounter interface {
	PlayerCount() (online, max int)
}

// Saver is optionally implemented by handlers that can flush world data before backups.
type Saver interface {
	// PrepareBackup flushes data to disk and pauses writes; the returned func resumes them.
	PrepareBackup(ctx context.Context) (resume func())
}

// Game describes a supported game type.
type Game interface {
	ID() string
	Name() string
	NewHandler(h Host) Handler
	// ValidateSettings normalizes the game settings for a new or updated server.
	ValidateSettings(srv *store.Server, isNew bool) error
}
