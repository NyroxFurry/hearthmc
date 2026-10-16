// Package settings provides cached, typed access to panel-wide settings
// stored in the database.
package settings

import (
	"context"
	"strconv"
	"sync"

	"hearth/internal/store"
)

const (
	PanelName          = "panel.name"
	PanelURL           = "panel.url"
	Contact            = "panel.contact"
	DefaultRuntime     = "runtime.default"
	DockerHost         = "docker.host"
	PortStart          = "ports.start"
	PortEnd            = "ports.end"
	InternalPortStart  = "ports.internalStart"
	InternalPortEnd    = "ports.internalEnd"
	BackupsMax         = "backups.max"
	MetricsRetention   = "retention.metricsDays"
	AuditRetention     = "retention.auditDays"
	ChatRetention      = "retention.chatDays"
	Timezone           = "timezone"
	JavaMode           = "java.mode" // auto | system
	SetupToken         = "setup.token"
	MapThreads         = "map.threads"
	MapPublicURL       = "map.publicUrl" // external address of the map port (reverse proxy setups)
	DefaultMemoryMB    = "servers.defaultMemoryMb"
	PublicPagesEnabled = "public.enabled"
	UploadLimitMB      = "files.uploadLimitMb"
	SFTPHost           = "sftp.host" // host[:port] shown for SFTP logins ("" = host of the panel)
)

var defaults = map[string]string{
	PanelName:          "Hearth",
	PanelURL:           "",
	Contact:            "",
	DefaultRuntime:     "native",
	DockerHost:         "",
	PortStart:          "25565",
	PortEnd:            "25665",
	InternalPortStart:  "41000",
	InternalPortEnd:    "41999",
	BackupsMax:         "10",
	MetricsRetention:   "30",
	AuditRetention:     "365",
	ChatRetention:      "30",
	Timezone:           "",
	JavaMode:           "auto",
	MapThreads:         "2",
	MapPublicURL:       "",
	DefaultMemoryMB:    "4096",
	PublicPagesEnabled: "1",
	UploadLimitMB:      "0",
	SFTPHost:           "",
}

// Public lists the settings exposed in the admin UI.
var Public = []string{PanelName, PanelURL, Contact, DefaultRuntime, DockerHost, PortStart, PortEnd, InternalPortStart,
	InternalPortEnd, BackupsMax, MetricsRetention, AuditRetention, ChatRetention, Timezone, JavaMode, MapThreads,
	MapPublicURL, DefaultMemoryMB, PublicPagesEnabled, UploadLimitMB, SFTPHost}

type Service struct {
	st    *store.Store
	mu    sync.RWMutex
	cache map[string]string
}

func New(st *store.Store) (*Service, error) {
	s := &Service{st: st}
	all, err := st.AllSettings(context.Background())
	if err != nil {
		return nil, err
	}
	s.cache = all
	return s, nil
}

func (s *Service) Get(key string) string {
	s.mu.RLock()
	v, ok := s.cache[key]
	s.mu.RUnlock()
	if ok {
		return v
	}
	return defaults[key]
}

func (s *Service) Int(key string) int {
	n, err := strconv.Atoi(s.Get(key))
	if err != nil {
		n, _ = strconv.Atoi(defaults[key])
	}
	return n
}

func (s *Service) Bool(key string) bool {
	v := s.Get(key)
	return v == "1" || v == "true"
}

func (s *Service) Set(ctx context.Context, key, value string) error {
	if err := s.st.SetSetting(ctx, key, value); err != nil {
		return err
	}
	s.mu.Lock()
	s.cache[key] = value
	s.mu.Unlock()
	return nil
}

func (s *Service) Delete(ctx context.Context, key string) error {
	if err := s.st.DeleteSetting(ctx, key); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.cache, key)
	s.mu.Unlock()
	return nil
}

// All returns the public settings with defaults applied.
func (s *Service) All() map[string]string {
	out := map[string]string{}
	for _, k := range Public {
		out[k] = s.Get(k)
	}
	return out
}
