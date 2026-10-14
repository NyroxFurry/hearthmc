package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

type PortSpec struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // tcp, udp, both
	Note     string `json:"note,omitempty"`
}

type Server struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Game           string          `json:"game"`
	OwnerID        int64           `json:"ownerId"`
	Runtime        string          `json:"runtime"`
	MemoryMB       int             `json:"memoryMb"`
	CPULimit       float64         `json:"cpuLimit"`
	DiskLimitMB    int             `json:"diskLimitMb"`
	Port           int             `json:"port"`
	ExtraPorts     []PortSpec      `json:"extraPorts"`
	Autostart      bool            `json:"autostart"`
	AutoRestart    bool            `json:"autoRestart"`
	DockerImage    string          `json:"dockerImage"`
	Settings       json.RawMessage `json:"settings"`
	InstallState   string          `json:"installState"` // "", installing, installed, failed
	InstallError   string          `json:"installError"`
	PublicEnabled  bool            `json:"publicEnabled"`
	PublicSlug     string          `json:"publicSlug"`
	PublicConfig   json.RawMessage `json:"publicConfig"`
	DisplayAddress string          `json:"displayAddress"`
	Color          string          `json:"color"`
	Suspended      bool            `json:"suspended"`
	WasRunning     bool            `json:"-"`
	CreatedAt      int64           `json:"createdAt"`
	UpdatedAt      int64           `json:"updatedAt"`
}

// Clone returns a deep copy.
func (s *Server) Clone() *Server {
	c := *s
	c.ExtraPorts = append([]PortSpec(nil), s.ExtraPorts...)
	c.Settings = append(json.RawMessage(nil), s.Settings...)
	c.PublicConfig = append(json.RawMessage(nil), s.PublicConfig...)
	return &c
}

const serverCols = `id, name, description, game, COALESCE(owner_id, 0), runtime, memory_mb, cpu_limit, disk_limit_mb,
	port, extra_ports, autostart, auto_restart, docker_image, settings, install_state, install_error, public_enabled,
	COALESCE(public_slug, ''), public_config, display_address, color, suspended, was_running, created_at, updated_at`

func scanServer(row interface{ Scan(...any) error }) (*Server, error) {
	var s Server
	var extra, settings, pub string
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Game, &s.OwnerID, &s.Runtime, &s.MemoryMB, &s.CPULimit,
		&s.DiskLimitMB, &s.Port, &extra, &s.Autostart, &s.AutoRestart, &s.DockerImage, &settings, &s.InstallState,
		&s.InstallError, &s.PublicEnabled, &s.PublicSlug, &pub, &s.DisplayAddress, &s.Color, &s.Suspended,
		&s.WasRunning, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	json.Unmarshal([]byte(extra), &s.ExtraPorts)
	if s.ExtraPorts == nil {
		s.ExtraPorts = []PortSpec{}
	}
	if settings == "" {
		settings = "{}"
	}
	if pub == "" {
		pub = "{}"
	}
	s.Settings = json.RawMessage(settings)
	s.PublicConfig = json.RawMessage(pub)
	return &s, nil
}

func (st *Store) ListServers(ctx context.Context) ([]*Server, error) {
	rows, err := st.DB.QueryContext(ctx, `SELECT `+serverCols+` FROM servers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Server
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (st *Store) GetServer(ctx context.Context, id string) (*Server, error) {
	return scanServer(st.DB.QueryRowContext(ctx, `SELECT `+serverCols+` FROM servers WHERE id = ?`, id))
}

func (st *Store) GetServerBySlug(ctx context.Context, slug string) (*Server, error) {
	return scanServer(st.DB.QueryRowContext(ctx, `SELECT `+serverCols+` FROM servers WHERE public_slug = ?`, slug))
}

func nullSlug(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (st *Store) CreateServer(ctx context.Context, s *Server) error {
	t := now()
	s.CreatedAt, s.UpdatedAt = t, t
	if s.Settings == nil {
		s.Settings = json.RawMessage("{}")
	}
	if s.PublicConfig == nil {
		s.PublicConfig = json.RawMessage("{}")
	}
	if s.ExtraPorts == nil {
		s.ExtraPorts = []PortSpec{}
	}
	var owner any
	if s.OwnerID != 0 {
		owner = s.OwnerID
	}
	_, err := st.DB.ExecContext(ctx, `INSERT INTO servers (id, name, description, game, owner_id, runtime, memory_mb,
		cpu_limit, disk_limit_mb, port, extra_ports, autostart, auto_restart, docker_image, settings, install_state,
		install_error, public_enabled, public_slug, public_config, display_address, color, suspended, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Description, s.Game, owner, s.Runtime, s.MemoryMB, s.CPULimit, s.DiskLimitMB, s.Port,
		mustJSON(s.ExtraPorts), b2i(s.Autostart), b2i(s.AutoRestart), s.DockerImage, string(s.Settings), s.InstallState,
		s.InstallError, b2i(s.PublicEnabled), nullSlug(s.PublicSlug), string(s.PublicConfig), s.DisplayAddress,
		s.Color, b2i(s.Suspended), t, t)
	return err
}

func (st *Store) UpdateServer(ctx context.Context, s *Server) error {
	s.UpdatedAt = now()
	var owner any
	if s.OwnerID != 0 {
		owner = s.OwnerID
	}
	_, err := st.DB.ExecContext(ctx, `UPDATE servers SET name = ?, description = ?, owner_id = ?, runtime = ?,
		memory_mb = ?, cpu_limit = ?, disk_limit_mb = ?, port = ?, extra_ports = ?, autostart = ?, auto_restart = ?,
		docker_image = ?, settings = ?, install_state = ?, install_error = ?, public_enabled = ?, public_slug = ?,
		public_config = ?, display_address = ?, color = ?, suspended = ?, updated_at = ? WHERE id = ?`,
		s.Name, s.Description, owner, s.Runtime, s.MemoryMB, s.CPULimit, s.DiskLimitMB, s.Port, mustJSON(s.ExtraPorts),
		b2i(s.Autostart), b2i(s.AutoRestart), s.DockerImage, string(s.Settings), s.InstallState, s.InstallError,
		b2i(s.PublicEnabled), nullSlug(s.PublicSlug), string(s.PublicConfig), s.DisplayAddress, s.Color,
		b2i(s.Suspended), s.UpdatedAt, s.ID)
	return err
}

func (st *Store) SetServerWasRunning(ctx context.Context, id string, running bool) {
	st.DB.ExecContext(ctx, `UPDATE servers SET was_running = ? WHERE id = ?`, b2i(running), id)
}

func (st *Store) DeleteServer(ctx context.Context, id string) error {
	return st.Tx(ctx, func(tx *sql.Tx) error {
		// metrics has no FK (WITHOUT ROWID table for speed)
		if _, err := tx.ExecContext(ctx, `DELETE FROM metrics WHERE server_id = ?`, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM servers WHERE id = ?`, id)
		return err
	})
}

func (st *Store) UsedPorts(ctx context.Context) (map[int]string, error) {
	rows, err := st.DB.QueryContext(ctx, `SELECT id, port, extra_ports, settings FROM servers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var id, extra, settings string
		var port int
		if err := rows.Scan(&id, &port, &extra, &settings); err != nil {
			return nil, err
		}
		if port > 0 {
			out[port] = id
		}
		var ex []PortSpec
		json.Unmarshal([]byte(extra), &ex)
		for _, p := range ex {
			out[p.Port] = id
		}
		// internal ports used by games (RCON, web server of a map plugin) live in the settings
		var internal struct {
			RCONPort int `json:"rconPort"`
			Map      struct {
				PluginPort int `json:"pluginPort"`
			} `json:"map"`
		}
		json.Unmarshal([]byte(settings), &internal)
		if internal.RCONPort > 0 {
			out[internal.RCONPort] = id
		}
		if internal.Map.PluginPort > 0 {
			out[internal.Map.PluginPort] = id
		}
	}
	return out, rows.Err()
}

// ---- members ----

type Member struct {
	ServerID    string   `json:"serverId"`
	UserID      int64    `json:"userId"`
	Username    string   `json:"username"`
	Permissions []string `json:"permissions"`
	AddedBy     int64    `json:"addedBy"`
	CreatedAt   int64    `json:"createdAt"`
}

func (st *Store) ListMembers(ctx context.Context, serverID string) ([]*Member, error) {
	rows, err := st.DB.QueryContext(ctx, `SELECT m.server_id, m.user_id, u.username, m.permissions, COALESCE(m.added_by, 0), m.created_at
		FROM server_members m JOIN users u ON u.id = m.user_id WHERE m.server_id = ? ORDER BY u.username COLLATE NOCASE`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Member{}
	for rows.Next() {
		var m Member
		var perms string
		if err := rows.Scan(&m.ServerID, &m.UserID, &m.Username, &perms, &m.AddedBy, &m.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(perms), &m.Permissions)
		if m.Permissions == nil {
			m.Permissions = []string{}
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (st *Store) GetMember(ctx context.Context, serverID string, userID int64) (*Member, error) {
	var m Member
	var perms string
	err := st.DB.QueryRowContext(ctx, `SELECT m.server_id, m.user_id, u.username, m.permissions, COALESCE(m.added_by, 0), m.created_at
		FROM server_members m JOIN users u ON u.id = m.user_id WHERE m.server_id = ? AND m.user_id = ?`, serverID, userID).
		Scan(&m.ServerID, &m.UserID, &m.Username, &perms, &m.AddedBy, &m.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	json.Unmarshal([]byte(perms), &m.Permissions)
	return &m, nil
}

// MemberServerPerms returns server id -> permissions for a user.
func (st *Store) MemberServerPerms(ctx context.Context, userID int64) (map[string][]string, error) {
	rows, err := st.DB.QueryContext(ctx, `SELECT server_id, permissions FROM server_members WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var id, perms string
		if err := rows.Scan(&id, &perms); err != nil {
			return nil, err
		}
		var p []string
		json.Unmarshal([]byte(perms), &p)
		out[id] = p
	}
	return out, rows.Err()
}

func (st *Store) UpsertMember(ctx context.Context, exec interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, serverID string, userID int64, perms []string, addedBy int64) error {
	if perms == nil {
		perms = []string{}
	}
	_, err := exec.ExecContext(ctx, `INSERT INTO server_members (server_id, user_id, permissions, added_by, created_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT (server_id, user_id) DO UPDATE SET permissions = excluded.permissions`,
		serverID, userID, mustJSON(perms), addedBy, now())
	return err
}

func (st *Store) DeleteMember(ctx context.Context, serverID string, userID int64) error {
	_, err := st.DB.ExecContext(ctx, `DELETE FROM server_members WHERE server_id = ? AND user_id = ?`, serverID, userID)
	return err
}
