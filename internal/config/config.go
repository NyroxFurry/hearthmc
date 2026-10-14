// Package config handles the bootstrap configuration of the panel.
// Everything that can be changed at runtime lives in the database
// (settings table); this file only covers what is needed to start.
package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	// DataDir holds the database, server files, backups, map tiles and Java runtimes.
	DataDir string `json:"dataDir"`
	// Listen is the HTTP listen address, e.g. ":8080" or "127.0.0.1:8080".
	Listen string `json:"listen"`
	// TLSDomain enables automatic HTTPS via Let's Encrypt (listens on :443 and :80).
	TLSDomain string `json:"tlsDomain,omitempty"`
	TLSEmail  string `json:"tlsEmail,omitempty"`
	// TLSCert/TLSKey use a manually provided certificate instead.
	TLSCert string `json:"tlsCert,omitempty"`
	TLSKey  string `json:"tlsKey,omitempty"`
	// TrustProxy makes the panel honour X-Forwarded-For / X-Forwarded-Proto
	// (enable when running behind nginx/Caddy/Traefik).
	TrustProxy bool `json:"trustProxy,omitempty"`
	// ServersDir overrides where server files are stored (default <data>/servers).
	ServersDir string `json:"serversDir,omitempty"`
	// BackupsDir overrides where backups are stored (default <data>/backups).
	BackupsDir string `json:"backupsDir,omitempty"`
	// LogLevel: debug, info, warn, error
	LogLevel string `json:"logLevel,omitempty"`
	// MapListen is the address of the map port, which serves the web maps of
	// map plugins (BlueMap, squaremap, Dynmap) on their own origin. Empty means
	// automatic (panel port + 1, with HTTPS 8443), "off" disables it.
	MapListen string `json:"mapListen,omitempty"`
}

func Default() *Config {
	return &Config{DataDir: "data", Listen: ":8080", LogLevel: "info"}
}

// Load builds the configuration from (in increasing priority):
// defaults, <data>/config.json, environment variables, command line flags.
func Load(args []string) (*Config, error) {
	cfg := Default()

	fs := flag.NewFlagSet("hearth", flag.ContinueOnError)
	dataDir := fs.String("data", "", "data directory (env HEARTH_DATA)")
	listen := fs.String("listen", "", "listen address, e.g. :8080 (env HEARTH_LISTEN)")
	tlsDomain := fs.String("tls-domain", "", "domain for automatic Let's Encrypt HTTPS (env HEARTH_TLS_DOMAIN)")
	tlsEmail := fs.String("tls-email", "", "contact e-mail for Let's Encrypt (env HEARTH_TLS_EMAIL)")
	tlsCert := fs.String("tls-cert", "", "TLS certificate file (env HEARTH_TLS_CERT)")
	tlsKey := fs.String("tls-key", "", "TLS key file (env HEARTH_TLS_KEY)")
	trustProxy := fs.Bool("trust-proxy", false, "trust X-Forwarded-* headers (env HEARTH_TRUST_PROXY=1)")
	logLevel := fs.String("log-level", "", "log level: debug, info, warn, error (env HEARTH_LOG_LEVEL)")
	mapListen := fs.String("map-listen", "", "address of the map port for map plugins, e.g. :8081 or off (env HEARTH_MAP_LISTEN)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// data dir first, because config.json lives inside it
	if v := os.Getenv("HEARTH_DATA"); v != "" {
		cfg.DataDir = v
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	abs, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	cfg.DataDir = abs

	if b, err := os.ReadFile(filepath.Join(cfg.DataDir, "config.json")); err == nil {
		fileCfg := *cfg
		if err := json.Unmarshal(b, &fileCfg); err != nil {
			return nil, fmt.Errorf("config.json: %w", err)
		}
		fileCfg.DataDir = cfg.DataDir // data dir cannot be moved by the file inside it
		*cfg = fileCfg
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	envStr := func(key string, dst *string) {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	envStr("HEARTH_LISTEN", &cfg.Listen)
	envStr("HEARTH_TLS_DOMAIN", &cfg.TLSDomain)
	envStr("HEARTH_TLS_EMAIL", &cfg.TLSEmail)
	envStr("HEARTH_TLS_CERT", &cfg.TLSCert)
	envStr("HEARTH_TLS_KEY", &cfg.TLSKey)
	envStr("HEARTH_LOG_LEVEL", &cfg.LogLevel)
	envStr("HEARTH_SERVERS_DIR", &cfg.ServersDir)
	envStr("HEARTH_BACKUPS_DIR", &cfg.BackupsDir)
	envStr("HEARTH_MAP_LISTEN", &cfg.MapListen)
	if v := os.Getenv("HEARTH_TRUST_PROXY"); v == "1" || strings.EqualFold(v, "true") {
		cfg.TrustProxy = true
	}

	if *listen != "" {
		cfg.Listen = *listen
	}
	if *tlsDomain != "" {
		cfg.TLSDomain = *tlsDomain
	}
	if *tlsEmail != "" {
		cfg.TLSEmail = *tlsEmail
	}
	if *tlsCert != "" {
		cfg.TLSCert = *tlsCert
	}
	if *tlsKey != "" {
		cfg.TLSKey = *tlsKey
	}
	if *trustProxy {
		cfg.TrustProxy = true
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}
	if *mapListen != "" {
		cfg.MapListen = *mapListen
	}

	if cfg.ServersDir == "" {
		cfg.ServersDir = filepath.Join(cfg.DataDir, "servers")
	}
	if cfg.BackupsDir == "" {
		cfg.BackupsDir = filepath.Join(cfg.DataDir, "backups")
	}
	for _, p := range []*string{&cfg.ServersDir, &cfg.BackupsDir} {
		if a, err := filepath.Abs(*p); err == nil {
			*p = a
		}
	}
	return cfg, nil
}

// MapAddr returns the listen address of the map port ("" = disabled).
func (c *Config) MapAddr() string {
	switch strings.ToLower(strings.TrimSpace(c.MapListen)) {
	case "off", "false", "0", "no":
		return ""
	case "":
	default:
		return c.MapListen
	}
	if c.TLSDomain != "" {
		return ":8443"
	}
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return ""
	}
	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 || p >= 65535 {
		return ""
	}
	return net.JoinHostPort(host, strconv.Itoa(p+1))
}

// Paths derived from the data dir.
func (c *Config) DBPath() string      { return filepath.Join(c.DataDir, "hearth.db") }
func (c *Config) MapsDir() string     { return filepath.Join(c.DataDir, "maps") }
func (c *Config) JavaDir() string     { return filepath.Join(c.DataDir, "java") }
func (c *Config) CacheDir() string    { return filepath.Join(c.DataDir, "cache") }
func (c *Config) TLSCacheDir() string { return filepath.Join(c.DataDir, "tls") }

// EnsureDirs creates all directories the panel needs.
func (c *Config) EnsureDirs() error {
	for _, d := range []string{c.DataDir, c.ServersDir, c.BackupsDir, c.MapsDir(), c.JavaDir(), c.CacheDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}

// Save writes config.json into the data directory (used by the installer).
func (c *Config) Save() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.DataDir, "config.json"), b, 0o600)
}
