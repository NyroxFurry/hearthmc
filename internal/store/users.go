package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

type User struct {
	ID               int64           `json:"id"`
	Username         string          `json:"username"`
	Email            string          `json:"email"`
	PasswordHash     string          `json:"-"`
	IsAdmin          bool            `json:"isAdmin"`
	Disabled         bool            `json:"disabled"`
	CanCreateServers bool            `json:"canCreateServers"`
	MaxServers       int             `json:"maxServers"`
	MaxMemoryMB      int             `json:"maxMemoryMb"`
	TOTPSecret       string          `json:"-"`
	TOTPEnabled      bool            `json:"totpEnabled"`
	RecoveryCodes    []string        `json:"-"` // hashed
	Prefs            json.RawMessage `json:"prefs"`
	CreatedAt        int64           `json:"createdAt"`
	UpdatedAt        int64           `json:"updatedAt"`
	LastLoginAt      int64           `json:"lastLoginAt"`
}

const userCols = `id, username, email, password_hash, is_admin, disabled, can_create_servers, max_servers,
	max_memory_mb, totp_secret, totp_enabled, recovery_codes, prefs, created_at, updated_at, last_login_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var rc, prefs string
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.Disabled, &u.CanCreateServers,
		&u.MaxServers, &u.MaxMemoryMB, &u.TOTPSecret, &u.TOTPEnabled, &rc, &prefs, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	if err != nil {
		return nil, notFound(err)
	}
	json.Unmarshal([]byte(rc), &u.RecoveryCodes)
	if prefs == "" {
		prefs = "{}"
	}
	u.Prefs = json.RawMessage(prefs)
	return &u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

func (s *Store) GetUserByName(ctx context.Context, name string) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username = ?`, name))
}

func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CreateUser(ctx context.Context, u *User) error {
	t := now()
	u.CreatedAt, u.UpdatedAt = t, t
	if u.Prefs == nil {
		u.Prefs = json.RawMessage("{}")
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO users (username, email, password_hash, is_admin, disabled,
		can_create_servers, max_servers, max_memory_mb, prefs, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.Username, u.Email, u.PasswordHash, b2i(u.IsAdmin), b2i(u.Disabled), b2i(u.CanCreateServers),
		u.MaxServers, u.MaxMemoryMB, string(u.Prefs), t, t)
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	return nil
}

// UpdateUser saves profile/role fields (not password or 2FA).
func (s *Store) UpdateUser(ctx context.Context, u *User) error {
	u.UpdatedAt = now()
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET username = ?, email = ?, is_admin = ?, disabled = ?,
		can_create_servers = ?, max_servers = ?, max_memory_mb = ?, prefs = ?, updated_at = ? WHERE id = ?`,
		u.Username, u.Email, b2i(u.IsAdmin), b2i(u.Disabled), b2i(u.CanCreateServers), u.MaxServers,
		u.MaxMemoryMB, string(u.Prefs), u.UpdatedAt, u.ID)
	return err
}

func (s *Store) SetPassword(ctx context.Context, id int64, hash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, now(), id)
	return err
}

func (s *Store) SetTOTP(ctx context.Context, id int64, secret string, enabled bool, recoveryHashes []string) error {
	if recoveryHashes == nil {
		recoveryHashes = []string{}
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET totp_secret = ?, totp_enabled = ?, recovery_codes = ?, updated_at = ? WHERE id = ?`,
		secret, b2i(enabled), mustJSON(recoveryHashes), now(), id)
	return err
}

func (s *Store) SetRecoveryCodes(ctx context.Context, id int64, hashes []string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET recovery_codes = ? WHERE id = ?`, mustJSON(hashes), id)
	return err
}

func (s *Store) TouchLogin(ctx context.Context, id int64) {
	s.DB.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, now(), id)
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1 AND disabled = 0`).Scan(&n)
	return n, err
}

// UserBrief is a lightweight user reference for lists.
type UserBrief struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

func (s *Store) UserNames(ctx context.Context) (map[int64]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, username FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ---- sessions ----

type Session struct {
	ID         string `json:"id"` // hashed token
	UserID     int64  `json:"userId"`
	Pending2FA bool   `json:"pending2fa"`
	CreatedAt  int64  `json:"createdAt"`
	ExpiresAt  int64  `json:"expiresAt"`
	LastSeenAt int64  `json:"lastSeenAt"`
	IP         string `json:"ip"`
	UserAgent  string `json:"userAgent"`
}

func (s *Store) CreateSession(ctx context.Context, sess *Session) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions (id, user_id, pending_2fa, created_at, expires_at, last_seen_at, ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, sess.ID, sess.UserID, b2i(sess.Pending2FA), sess.CreatedAt, sess.ExpiresAt,
		sess.LastSeenAt, sess.IP, sess.UserAgent)
	return err
}

func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	var sess Session
	err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, pending_2fa, created_at, expires_at, last_seen_at, ip, user_agent
		FROM sessions WHERE id = ?`, id).Scan(&sess.ID, &sess.UserID, &sess.Pending2FA, &sess.CreatedAt, &sess.ExpiresAt,
		&sess.LastSeenAt, &sess.IP, &sess.UserAgent)
	if err != nil {
		return nil, notFound(err)
	}
	return &sess, nil
}

func (s *Store) TouchSession(ctx context.Context, id string, expiresAt int64, ip string) {
	s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ?, ip = ? WHERE id = ?`, now(), expiresAt, ip, id)
}

func (s *Store) CompleteSession2FA(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET pending_2fa = 0 WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID int64, exceptID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id != ?`, userID, exceptID)
	return err
}

func (s *Store) ListSessions(ctx context.Context, userID int64) ([]*Session, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, pending_2fa, created_at, expires_at, last_seen_at, ip, user_agent
		FROM sessions WHERE user_id = ? AND pending_2fa = 0 ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.UserID, &sess.Pending2FA, &sess.CreatedAt, &sess.ExpiresAt,
			&sess.LastSeenAt, &sess.IP, &sess.UserAgent); err != nil {
			return nil, err
		}
		out = append(out, &sess)
	}
	return out, rows.Err()
}

// ---- API keys ----

type APIKey struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"userId"`
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	TokenHash  string `json:"-"`
	CreatedAt  int64  `json:"createdAt"`
	LastUsedAt int64  `json:"lastUsedAt"`
	ExpiresAt  int64  `json:"expiresAt"`
}

func (s *Store) CreateAPIKey(ctx context.Context, k *APIKey) error {
	k.CreatedAt = now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO api_keys (user_id, name, prefix, token_hash, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, k.UserID, k.Name, k.Prefix, k.TokenHash, k.CreatedAt, k.ExpiresAt)
	if err != nil {
		return err
	}
	k.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error) {
	var k APIKey
	err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, name, prefix, token_hash, created_at, last_used_at, expires_at
		FROM api_keys WHERE token_hash = ?`, hash).Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.TokenHash,
		&k.CreatedAt, &k.LastUsedAt, &k.ExpiresAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &k, nil
}

func (s *Store) TouchAPIKey(ctx context.Context, id int64) {
	s.DB.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now(), id)
}

func (s *Store) ListAPIKeys(ctx context.Context, userID int64) ([]*APIKey, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, name, prefix, token_hash, created_at, last_used_at, expires_at
		FROM api_keys WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.TokenHash, &k.CreatedAt, &k.LastUsedAt, &k.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIKey(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- invites ----

type Invite struct {
	ID               int64    `json:"id"`
	TokenHash        string   `json:"-"`
	CreatedBy        int64    `json:"createdBy"`
	Note             string   `json:"note"`
	IsAdmin          bool     `json:"isAdmin"`
	CanCreateServers bool     `json:"canCreateServers"`
	ServerID         string   `json:"serverId"`
	ServerPerms      []string `json:"serverPerms"`
	CreatedAt        int64    `json:"createdAt"`
	ExpiresAt        int64    `json:"expiresAt"`
	UsedBy           int64    `json:"usedBy"`
	UsedAt           int64    `json:"usedAt"`
}

const inviteCols = `id, token_hash, COALESCE(created_by, 0), note, is_admin, can_create_servers, COALESCE(server_id, ''),
	server_perms, created_at, expires_at, COALESCE(used_by, 0), used_at`

func scanInvite(row interface{ Scan(...any) error }) (*Invite, error) {
	var inv Invite
	var perms string
	if err := row.Scan(&inv.ID, &inv.TokenHash, &inv.CreatedBy, &inv.Note, &inv.IsAdmin, &inv.CanCreateServers,
		&inv.ServerID, &perms, &inv.CreatedAt, &inv.ExpiresAt, &inv.UsedBy, &inv.UsedAt); err != nil {
		return nil, notFound(err)
	}
	json.Unmarshal([]byte(perms), &inv.ServerPerms)
	if inv.ServerPerms == nil {
		inv.ServerPerms = []string{}
	}
	return &inv, nil
}

func (s *Store) CreateInvite(ctx context.Context, inv *Invite) error {
	inv.CreatedAt = now()
	var serverID any
	if inv.ServerID != "" {
		serverID = inv.ServerID
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO invites (token_hash, created_by, note, is_admin, can_create_servers,
		server_id, server_perms, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.TokenHash, inv.CreatedBy, inv.Note, b2i(inv.IsAdmin), b2i(inv.CanCreateServers), serverID,
		mustJSON(inv.ServerPerms), inv.CreatedAt, inv.ExpiresAt)
	if err != nil {
		return err
	}
	inv.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) GetInviteByHash(ctx context.Context, hash string) (*Invite, error) {
	return scanInvite(s.DB.QueryRowContext(ctx, `SELECT `+inviteCols+` FROM invites WHERE token_hash = ?`, hash))
}

func (s *Store) ListInvites(ctx context.Context) ([]*Invite, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+inviteCols+` FROM invites ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Invite{}
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *Store) UseInvite(ctx context.Context, tx *sql.Tx, id, userID int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE invites SET used_by = ?, used_at = ? WHERE id = ? AND used_at = 0`, userID, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteInvite(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM invites WHERE id = ?`, id)
	return err
}
