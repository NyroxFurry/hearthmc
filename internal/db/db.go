// Package db opens the SQLite database and applies schema migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// Open opens (and creates if needed) the SQLite database at path and
// migrates it to the newest schema version.
func Open(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(normal)")
	q.Add("_txlock", "immediate")
	dsn := "file:" + filepath.ToSlash(abs) + "?" + q.Encode()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	for i := current; i < len(migrations); i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// migrations are applied in order; never edit an existing entry, only append.
var migrations = []string{
	// 1: initial schema
	`
CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE users (
	id                 INTEGER PRIMARY KEY,
	username           TEXT NOT NULL UNIQUE COLLATE NOCASE,
	email              TEXT NOT NULL DEFAULT '',
	password_hash      TEXT NOT NULL,
	is_admin           INTEGER NOT NULL DEFAULT 0,
	disabled           INTEGER NOT NULL DEFAULT 0,
	can_create_servers INTEGER NOT NULL DEFAULT 0,
	max_servers        INTEGER NOT NULL DEFAULT 0,
	max_memory_mb      INTEGER NOT NULL DEFAULT 0,
	totp_secret        TEXT NOT NULL DEFAULT '',
	totp_enabled       INTEGER NOT NULL DEFAULT 0,
	recovery_codes     TEXT NOT NULL DEFAULT '[]',
	prefs              TEXT NOT NULL DEFAULT '{}',
	created_at         INTEGER NOT NULL,
	updated_at         INTEGER NOT NULL,
	last_login_at      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE sessions (
	id           TEXT PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	pending_2fa  INTEGER NOT NULL DEFAULT 0,
	created_at   INTEGER NOT NULL,
	expires_at   INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL,
	ip           TEXT NOT NULL DEFAULT '',
	user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE api_keys (
	id           INTEGER PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT NOT NULL,
	prefix       TEXT NOT NULL,
	token_hash   TEXT NOT NULL UNIQUE,
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER NOT NULL DEFAULT 0,
	expires_at   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE servers (
	id              TEXT PRIMARY KEY,
	name            TEXT NOT NULL,
	description     TEXT NOT NULL DEFAULT '',
	game            TEXT NOT NULL,
	owner_id        INTEGER REFERENCES users(id) ON DELETE SET NULL,
	runtime         TEXT NOT NULL DEFAULT 'native',
	memory_mb       INTEGER NOT NULL DEFAULT 0,
	cpu_limit       REAL NOT NULL DEFAULT 0,
	disk_limit_mb   INTEGER NOT NULL DEFAULT 0,
	port            INTEGER NOT NULL DEFAULT 0,
	extra_ports     TEXT NOT NULL DEFAULT '[]',
	autostart       INTEGER NOT NULL DEFAULT 0,
	auto_restart    INTEGER NOT NULL DEFAULT 1,
	docker_image    TEXT NOT NULL DEFAULT '',
	settings        TEXT NOT NULL DEFAULT '{}',
	install_state   TEXT NOT NULL DEFAULT '',
	install_error   TEXT NOT NULL DEFAULT '',
	public_enabled  INTEGER NOT NULL DEFAULT 0,
	public_slug     TEXT UNIQUE,
	public_config   TEXT NOT NULL DEFAULT '{}',
	display_address TEXT NOT NULL DEFAULT '',
	color           TEXT NOT NULL DEFAULT '',
	suspended       INTEGER NOT NULL DEFAULT 0,
	was_running     INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL
);

CREATE TABLE server_members (
	server_id   TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	permissions TEXT NOT NULL DEFAULT '[]',
	added_by    INTEGER,
	created_at  INTEGER NOT NULL,
	PRIMARY KEY (server_id, user_id)
);
CREATE INDEX server_members_user ON server_members(user_id);

CREATE TABLE invites (
	id                 INTEGER PRIMARY KEY,
	token_hash         TEXT NOT NULL UNIQUE,
	created_by         INTEGER REFERENCES users(id) ON DELETE SET NULL,
	note               TEXT NOT NULL DEFAULT '',
	is_admin           INTEGER NOT NULL DEFAULT 0,
	can_create_servers INTEGER NOT NULL DEFAULT 0,
	server_id          TEXT REFERENCES servers(id) ON DELETE SET NULL,
	server_perms       TEXT NOT NULL DEFAULT '[]',
	created_at         INTEGER NOT NULL,
	expires_at         INTEGER NOT NULL,
	used_by            INTEGER REFERENCES users(id) ON DELETE SET NULL,
	used_at            INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE backups (
	id          TEXT PRIMARY KEY,
	server_id   TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	name        TEXT NOT NULL,
	file        TEXT NOT NULL,
	size        INTEGER NOT NULL DEFAULT 0,
	files       INTEGER NOT NULL DEFAULT 0,
	status      TEXT NOT NULL,
	error       TEXT NOT NULL DEFAULT '',
	locked      INTEGER NOT NULL DEFAULT 0,
	created_by  INTEGER,
	created_at  INTEGER NOT NULL,
	finished_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX backups_server ON backups(server_id, created_at);

CREATE TABLE schedules (
	id               INTEGER PRIMARY KEY,
	server_id        TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	name             TEXT NOT NULL,
	cron             TEXT NOT NULL,
	enabled          INTEGER NOT NULL DEFAULT 1,
	only_when_online INTEGER NOT NULL DEFAULT 0,
	steps            TEXT NOT NULL DEFAULT '[]',
	last_run_at      INTEGER NOT NULL DEFAULT 0,
	last_status      TEXT NOT NULL DEFAULT '',
	last_error       TEXT NOT NULL DEFAULT '',
	created_at       INTEGER NOT NULL
);
CREATE INDEX schedules_server ON schedules(server_id);

CREATE TABLE audit_log (
	id        INTEGER PRIMARY KEY,
	at        INTEGER NOT NULL,
	user_id   INTEGER,
	username  TEXT NOT NULL DEFAULT '',
	server_id TEXT,
	action    TEXT NOT NULL,
	details   TEXT NOT NULL DEFAULT '{}',
	ip        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_at ON audit_log(at);
CREATE INDEX audit_server ON audit_log(server_id, at);

CREATE TABLE webhooks (
	id          INTEGER PRIMARY KEY,
	server_id   TEXT REFERENCES servers(id) ON DELETE CASCADE,
	name        TEXT NOT NULL,
	kind        TEXT NOT NULL,
	url         TEXT NOT NULL,
	events      TEXT NOT NULL DEFAULT '[]',
	enabled     INTEGER NOT NULL DEFAULT 1,
	last_status TEXT NOT NULL DEFAULT '',
	created_at  INTEGER NOT NULL
);

CREATE TABLE metrics (
	server_id TEXT NOT NULL,
	at        INTEGER NOT NULL,
	cpu       REAL NOT NULL,
	mem       INTEGER NOT NULL,
	players   INTEGER NOT NULL DEFAULT -1,
	PRIMARY KEY (server_id, at)
) WITHOUT ROWID;

CREATE TABLE mc_players (
	server_id       TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	uuid            TEXT NOT NULL,
	name            TEXT NOT NULL,
	first_seen      INTEGER NOT NULL DEFAULT 0,
	last_seen       INTEGER NOT NULL DEFAULT 0,
	tracked_seconds INTEGER NOT NULL DEFAULT 0,
	sessions        INTEGER NOT NULL DEFAULT 0,
	play_ticks      INTEGER NOT NULL DEFAULT 0,
	deaths          INTEGER NOT NULL DEFAULT 0,
	mob_kills       INTEGER NOT NULL DEFAULT 0,
	player_kills    INTEGER NOT NULL DEFAULT 0,
	blocks_mined    INTEGER NOT NULL DEFAULT 0,
	distance_cm     INTEGER NOT NULL DEFAULT 0,
	jumps           INTEGER NOT NULL DEFAULT 0,
	damage_dealt    INTEGER NOT NULL DEFAULT 0,
	damage_taken    INTEGER NOT NULL DEFAULT 0,
	items_crafted   INTEGER NOT NULL DEFAULT 0,
	fish_caught     INTEGER NOT NULL DEFAULT 0,
	advancements    INTEGER NOT NULL DEFAULT 0,
	stats           TEXT NOT NULL DEFAULT '',
	stats_mtime     INTEGER NOT NULL DEFAULT 0,
	adv_mtime       INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (server_id, uuid)
);

CREATE TABLE mc_sessions (
	id        INTEGER PRIMARY KEY,
	server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	uuid      TEXT NOT NULL,
	joined_at INTEGER NOT NULL,
	left_at   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX mc_sessions_player ON mc_sessions(server_id, uuid, joined_at);
CREATE INDEX mc_sessions_time ON mc_sessions(server_id, joined_at);

CREATE TABLE mc_events (
	id        INTEGER PRIMARY KEY,
	server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
	at        INTEGER NOT NULL,
	type      TEXT NOT NULL,
	uuid      TEXT NOT NULL DEFAULT '',
	name      TEXT NOT NULL DEFAULT '',
	message   TEXT NOT NULL DEFAULT '',
	data      TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX mc_events_server ON mc_events(server_id, type, at);
CREATE INDEX mc_events_player ON mc_events(server_id, uuid, at);
`,
	// 1.2: Java heap in the metrics, SSH keys for SFTP
	`
ALTER TABLE metrics ADD COLUMN heap INTEGER NOT NULL DEFAULT 0;

CREATE TABLE ssh_keys (
	id           INTEGER PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT NOT NULL,
	fingerprint  TEXT NOT NULL,
	public_key   TEXT NOT NULL,
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX ssh_keys_user_fp ON ssh_keys(user_id, fingerprint);
`,
}
