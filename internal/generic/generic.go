// Package generic implements a game type that runs an arbitrary start command.
// It is the base for running any other game (Terraria, Valheim, Factorio, a
// Discord bot ...) with console, files, backups, schedules and permissions.
package generic

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hearth/internal/game"
	"hearth/internal/minecraft"
	"hearth/internal/store"
)

type Settings struct {
	Command     string   `json:"command"`     // start command, e.g. "./TerrariaServer -config serverconfig.txt"
	StopCommand string   `json:"stopCommand"` // console command for a graceful stop ("" = SIGTERM)
	ReadyRegex  string   `json:"readyRegex"`  // console line that marks the server as ready ("" = immediately)
	StopTimeout int      `json:"stopTimeout"` // seconds
	Env         []string `json:"env"`
}

func decode(raw json.RawMessage) Settings {
	var s Settings
	json.Unmarshal(raw, &s)
	return s
}

type Game struct{}

func (Game) ID() string   { return "generic" }
func (Game) Name() string { return "Eigener Startbefehl" }

func (Game) ValidateSettings(srv *store.Server, isNew bool) error {
	s := decode(srv.Settings)
	s.Command = strings.TrimSpace(s.Command)
	if s.Command == "" {
		return errors.New("Bitte einen Startbefehl angeben")
	}
	if strings.ContainsAny(s.Command+s.StopCommand, "\r\n") {
		return errors.New("Befehle dürfen keine Zeilenumbrüche enthalten")
	}
	if s.ReadyRegex != "" {
		if _, err := regexp.Compile(s.ReadyRegex); err != nil {
			return errors.New("Ungültiger regulärer Ausdruck für „Bereit“")
		}
	}
	if srv.Runtime == "docker" && strings.TrimSpace(srv.DockerImage) == "" {
		return errors.New("Für Docker muss ein Image angegeben werden")
	}
	for _, e := range s.Env {
		if !strings.Contains(e, "=") {
			return errors.New("Umgebungsvariablen im Format NAME=wert angeben")
		}
	}
	if s.StopTimeout < 0 || s.StopTimeout > 3600 {
		return errors.New("Stopp-Zeitlimit muss zwischen 0 und 3600 Sekunden liegen")
	}
	b, _ := json.Marshal(s)
	srv.Settings = b
	return nil
}

func (Game) NewHandler(h game.Host) game.Handler { return &handler{host: h} }

type handler struct {
	host  game.Host
	ready *regexp.Regexp
}

func (h *handler) Install(ctx context.Context) error {
	h.host.Console("Lade deine Serverdateien im Dateimanager hoch und starte den Server.")
	return nil
}

func (h *handler) Prepare(ctx context.Context, runtimeName string) (*game.Launch, error) {
	srv := h.host.Server()
	s := decode(srv.Settings)
	repl := strings.NewReplacer("{{port}}", strconv.Itoa(srv.Port), "{{memory}}", strconv.Itoa(srv.MemoryMB))
	argv := minecraft.SplitArgs(repl.Replace(s.Command))
	if len(argv) == 0 {
		return nil, errors.New("kein Startbefehl konfiguriert")
	}
	h.ready = nil
	if s.ReadyRegex != "" {
		h.ready, _ = regexp.Compile(s.ReadyRegex)
	}
	timeout := 30 * time.Second
	if s.StopTimeout > 0 {
		timeout = time.Duration(s.StopTimeout) * time.Second
	}
	return &game.Launch{Argv: argv, Env: s.Env, Image: srv.DockerImage, StopTimeout: timeout}, nil
}

func (h *handler) OnStart()                          {}
func (h *handler) OnLine(line string)                {}
func (h *handler) OnStop(exitCode int, crashed bool) {}
func (h *handler) Info() any                         { return nil }
func (h *handler) Close()                            {}

func (h *handler) Ready(line string) bool {
	if h.ready == nil {
		return true
	}
	return h.ready.MatchString(minecraft.StripANSI(line))
}

func (h *handler) StopCommand() string {
	return decode(h.host.Server().Settings).StopCommand
}
