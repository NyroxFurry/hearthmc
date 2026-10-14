package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
)

// MCPlayer is a player known to a Minecraft server.
type MCPlayer struct {
	ServerID       string          `json:"-"`
	UUID           string          `json:"uuid"`
	Name           string          `json:"name"`
	FirstSeen      int64           `json:"firstSeen"`
	LastSeen       int64           `json:"lastSeen"`
	TrackedSeconds int64           `json:"trackedSeconds"`
	Sessions       int64           `json:"sessions"`
	PlayTicks      int64           `json:"playTicks"`
	Deaths         int64           `json:"deaths"`
	MobKills       int64           `json:"mobKills"`
	PlayerKills    int64           `json:"playerKills"`
	BlocksMined    int64           `json:"blocksMined"`
	DistanceCM     int64           `json:"distanceCm"`
	Jumps          int64           `json:"jumps"`
	DamageDealt    int64           `json:"damageDealt"`
	DamageTaken    int64           `json:"damageTaken"`
	ItemsCrafted   int64           `json:"itemsCrafted"`
	FishCaught     int64           `json:"fishCaught"`
	Advancements   int64           `json:"advancements"`
	Stats          json.RawMessage `json:"stats,omitempty"`
	StatsMTime     int64           `json:"-"`
	AdvMTime       int64           `json:"-"`
}

// PlaySeconds returns the best known playtime (stats file if available, else tracked sessions).
func (p *MCPlayer) PlaySeconds() int64 {
	if p.PlayTicks > 0 {
		return p.PlayTicks / 20
	}
	return p.TrackedSeconds
}

const mcPlayerCols = `uuid, name, first_seen, last_seen, tracked_seconds, sessions, play_ticks, deaths, mob_kills,
	player_kills, blocks_mined, distance_cm, jumps, damage_dealt, damage_taken, items_crafted, fish_caught, advancements,
	stats_mtime, adv_mtime`

func scanMCPlayer(row interface{ Scan(...any) error }, withStats bool) (*MCPlayer, error) {
	var p MCPlayer
	dest := []any{&p.UUID, &p.Name, &p.FirstSeen, &p.LastSeen, &p.TrackedSeconds, &p.Sessions, &p.PlayTicks, &p.Deaths,
		&p.MobKills, &p.PlayerKills, &p.BlocksMined, &p.DistanceCM, &p.Jumps, &p.DamageDealt, &p.DamageTaken,
		&p.ItemsCrafted, &p.FishCaught, &p.Advancements, &p.StatsMTime, &p.AdvMTime}
	var stats string
	if withStats {
		dest = append(dest, &stats)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, notFound(err)
	}
	if withStats && stats != "" {
		p.Stats = json.RawMessage(stats)
	}
	return &p, nil
}

// PlayerSeen records that a player is (or was) online; creates the row if needed.
func (s *Store) PlayerSeen(ctx context.Context, serverID, uuid, name string, at int64, newSession bool) error {
	inc := 0
	if newSession {
		inc = 1
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO mc_players (server_id, uuid, name, first_seen, last_seen, sessions)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (server_id, uuid) DO UPDATE SET name = excluded.name, last_seen = excluded.last_seen,
			sessions = sessions + ?, first_seen = CASE WHEN first_seen = 0 THEN excluded.first_seen ELSE first_seen END`,
		serverID, uuid, name, at, at, inc, inc)
	return err
}

func (s *Store) StartMCSession(ctx context.Context, serverID, uuid string, at int64) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO mc_sessions (server_id, uuid, joined_at) VALUES (?, ?, ?)`, serverID, uuid, at)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// EndMCSession closes a session and adds its duration to the player's tracked time.
func (s *Store) EndMCSession(ctx context.Context, serverID string, sessionID int64, at int64) error {
	return s.Tx(ctx, func(tx *sql.Tx) error {
		var uuid string
		var joined, left int64
		if err := tx.QueryRowContext(ctx, `SELECT uuid, joined_at, left_at FROM mc_sessions WHERE id = ? AND server_id = ?`,
			sessionID, serverID).Scan(&uuid, &joined, &left); err != nil {
			return notFound(err)
		}
		if left != 0 {
			return nil
		}
		if at < joined {
			at = joined
		}
		if _, err := tx.ExecContext(ctx, `UPDATE mc_sessions SET left_at = ? WHERE id = ?`, at, sessionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE mc_players SET tracked_seconds = tracked_seconds + ?, last_seen = ?
			WHERE server_id = ? AND uuid = ?`, at-joined, at, serverID, uuid)
		return err
	})
}

// CloseOpenMCSessions ends all open sessions of a server (server stopped/crashed or panel restarted).
func (s *Store) CloseOpenMCSessions(ctx context.Context, serverID string, at int64) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM mc_sessions WHERE server_id = ? AND left_at = 0`, serverID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.EndMCSession(ctx, serverID, id, at); err != nil {
			return err
		}
	}
	return nil
}

// CloseStaleMCSessions closes sessions left open by an unclean shutdown without
// adding playtime (the real end time is unknown).
func (s *Store) CloseStaleMCSessions(ctx context.Context, serverID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE mc_sessions SET left_at = joined_at WHERE server_id = ? AND left_at = 0`, serverID)
	return err
}

// OpenMCSessions returns uuid -> session id of sessions that are still open.
func (s *Store) OpenMCSessions(ctx context.Context, serverID string) (map[string]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT uuid, id FROM mc_sessions WHERE server_id = ? AND left_at = 0`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var u string
		var id int64
		if err := rows.Scan(&u, &id); err != nil {
			return nil, err
		}
		out[u] = id
	}
	return out, rows.Err()
}

type MCStatsUpdate struct {
	UUID         string
	Name         string // optional, from usercache
	PlayTicks    int64
	Deaths       int64
	MobKills     int64
	PlayerKills  int64
	BlocksMined  int64
	DistanceCM   int64
	Jumps        int64
	DamageDealt  int64
	DamageTaken  int64
	ItemsCrafted int64
	FishCaught   int64
	Stats        string
	MTime        int64
}

func (s *Store) UpdateMCStats(ctx context.Context, serverID string, u *MCStatsUpdate) error {
	name := u.Name
	if name == "" {
		name = u.UUID[:8]
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO mc_players (server_id, uuid, name, play_ticks, deaths, mob_kills,
		player_kills, blocks_mined, distance_cm, jumps, damage_dealt, damage_taken, items_crafted, fish_caught, stats, stats_mtime)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (server_id, uuid) DO UPDATE SET
			name = CASE WHEN ? != '' THEN ? ELSE name END,
			play_ticks = excluded.play_ticks, deaths = excluded.deaths, mob_kills = excluded.mob_kills,
			player_kills = excluded.player_kills, blocks_mined = excluded.blocks_mined, distance_cm = excluded.distance_cm,
			jumps = excluded.jumps, damage_dealt = excluded.damage_dealt, damage_taken = excluded.damage_taken,
			items_crafted = excluded.items_crafted, fish_caught = excluded.fish_caught, stats = excluded.stats,
			stats_mtime = excluded.stats_mtime`,
		serverID, u.UUID, name, u.PlayTicks, u.Deaths, u.MobKills, u.PlayerKills, u.BlocksMined, u.DistanceCM, u.Jumps,
		u.DamageDealt, u.DamageTaken, u.ItemsCrafted, u.FishCaught, u.Stats, u.MTime, u.Name, u.Name)
	return err
}

func (s *Store) UpdateMCAdvancements(ctx context.Context, serverID, uuid string, count int64, mtime int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE mc_players SET advancements = ?, adv_mtime = ? WHERE server_id = ? AND uuid = ?`,
		count, mtime, serverID, uuid)
	return err
}

func (s *Store) RenameMCPlayer(ctx context.Context, serverID, uuid, name string) {
	s.DB.ExecContext(ctx, `UPDATE mc_players SET name = ? WHERE server_id = ? AND uuid = ? AND name != ?`, name, serverID, uuid, name)
}

// StatsMTimes returns uuid -> (stats mtime, advancements mtime) so unchanged files can be skipped.
func (s *Store) MCStatsMTimes(ctx context.Context, serverID string) (map[string][2]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT uuid, stats_mtime, adv_mtime FROM mc_players WHERE server_id = ?`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][2]int64{}
	for rows.Next() {
		var u string
		var a, b int64
		if err := rows.Scan(&u, &a, &b); err != nil {
			return nil, err
		}
		out[u] = [2]int64{a, b}
	}
	return out, rows.Err()
}

var mcSortColumns = map[string]string{
	"playtime":     "CASE WHEN play_ticks > 0 THEN play_ticks / 20 ELSE tracked_seconds END",
	"deaths":       "deaths",
	"mobKills":     "mob_kills",
	"playerKills":  "player_kills",
	"blocksMined":  "blocks_mined",
	"distance":     "distance_cm",
	"jumps":        "jumps",
	"damageDealt":  "damage_dealt",
	"damageTaken":  "damage_taken",
	"itemsCrafted": "items_crafted",
	"fishCaught":   "fish_caught",
	"advancements": "advancements",
	"lastSeen":     "last_seen",
	"firstSeen":    "first_seen",
	"name":         "name COLLATE NOCASE",
}

// ValidMCSort reports whether key is a sortable leaderboard column.
func ValidMCSort(key string) bool { _, ok := mcSortColumns[key]; return ok }

type MCPlayerQuery struct {
	Sort   string
	Asc    bool
	Search string
	Limit  int
	Offset int
}

func (s *Store) ListMCPlayers(ctx context.Context, serverID string, q MCPlayerQuery) ([]*MCPlayer, int, error) {
	col, ok := mcSortColumns[q.Sort]
	if !ok {
		col = mcSortColumns["playtime"]
	}
	dir := "DESC"
	if q.Asc {
		dir = "ASC"
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 50
	}
	where := "server_id = ?"
	args := []any{serverID}
	if q.Search != "" {
		where += " AND (name LIKE ? OR uuid LIKE ?)"
		pat := "%" + strings.ReplaceAll(q.Search, "%", "") + "%"
		args = append(args, pat, pat)
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM mc_players WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+mcPlayerCols+` FROM mc_players WHERE `+where+
		` ORDER BY `+col+` `+dir+`, name COLLATE NOCASE LIMIT `+strconv.Itoa(q.Limit)+` OFFSET `+strconv.Itoa(q.Offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*MCPlayer{}
	for rows.Next() {
		p, err := scanMCPlayer(rows, false)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

func (s *Store) GetMCPlayer(ctx context.Context, serverID, uuid string) (*MCPlayer, error) {
	return scanMCPlayer(s.DB.QueryRowContext(ctx, `SELECT `+mcPlayerCols+`, stats FROM mc_players WHERE server_id = ? AND uuid = ?`,
		serverID, uuid), true)
}

func (s *Store) GetMCPlayerByName(ctx context.Context, serverID, name string) (*MCPlayer, error) {
	return scanMCPlayer(s.DB.QueryRowContext(ctx, `SELECT `+mcPlayerCols+`, stats FROM mc_players WHERE server_id = ? AND name = ? COLLATE NOCASE
		ORDER BY last_seen DESC LIMIT 1`, serverID, name), true)
}

type MCTotals struct {
	Players      int64 `json:"players"`
	PlaySeconds  int64 `json:"playSeconds"`
	Deaths       int64 `json:"deaths"`
	MobKills     int64 `json:"mobKills"`
	PlayerKills  int64 `json:"playerKills"`
	BlocksMined  int64 `json:"blocksMined"`
	DistanceCM   int64 `json:"distanceCm"`
	Sessions     int64 `json:"sessions"`
	Advancements int64 `json:"advancements"`
}

func (s *Store) MCTotals(ctx context.Context, serverID string) (*MCTotals, error) {
	var t MCTotals
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN play_ticks > 0 THEN play_ticks / 20 ELSE tracked_seconds END), 0),
		COALESCE(SUM(deaths), 0), COALESCE(SUM(mob_kills), 0), COALESCE(SUM(player_kills), 0),
		COALESCE(SUM(blocks_mined), 0), COALESCE(SUM(distance_cm), 0), COALESCE(SUM(sessions), 0),
		COALESCE(SUM(advancements), 0)
		FROM mc_players WHERE server_id = ?`, serverID).Scan(&t.Players, &t.PlaySeconds, &t.Deaths, &t.MobKills,
		&t.PlayerKills, &t.BlocksMined, &t.DistanceCM, &t.Sessions, &t.Advancements)
	return &t, err
}

// ---- events ----

type MCEvent struct {
	ID      int64           `json:"id"`
	At      int64           `json:"at"`
	Type    string          `json:"type"` // death, advancement, chat, join, leave
	UUID    string          `json:"uuid"`
	Name    string          `json:"name"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (s *Store) AddMCEvent(ctx context.Context, serverID string, e *MCEvent) error {
	if e.Data == nil {
		e.Data = json.RawMessage("{}")
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO mc_events (server_id, at, type, uuid, name, message, data) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		serverID, e.At, e.Type, e.UUID, e.Name, e.Message, string(e.Data))
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

type MCEventQuery struct {
	Types  []string
	UUID   string
	Before int64 // id cursor
	Since  int64 // unix time
	Limit  int
}

func (s *Store) ListMCEvents(ctx context.Context, serverID string, q MCEventQuery) ([]*MCEvent, error) {
	where := []string{"server_id = ?"}
	args := []any{serverID}
	if len(q.Types) > 0 {
		ph := make([]string, len(q.Types))
		for i, t := range q.Types {
			ph[i] = "?"
			args = append(args, t)
		}
		where = append(where, "type IN ("+strings.Join(ph, ",")+")")
	}
	if q.UUID != "" {
		where = append(where, "uuid = ?")
		args = append(args, q.UUID)
	}
	if q.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, q.Before)
	}
	if q.Since > 0 {
		where = append(where, "at >= ?")
		args = append(args, q.Since)
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, at, type, uuid, name, message, data FROM mc_events WHERE `+
		strings.Join(where, " AND ")+` ORDER BY id DESC LIMIT `+strconv.Itoa(q.Limit), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*MCEvent{}
	for rows.Next() {
		var e MCEvent
		var d string
		if err := rows.Scan(&e.ID, &e.At, &e.Type, &e.UUID, &e.Name, &e.Message, &d); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(d)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// DeathCauses aggregates recorded deaths by cause for a server (optionally one player).
func (s *Store) MCDeathCauses(ctx context.Context, serverID, uuid string, since int64) (map[string]int64, error) {
	q := `SELECT COALESCE(json_extract(data, '$.cause'), 'unknown'), COUNT(*) FROM mc_events WHERE server_id = ? AND type = 'death' AND at >= ?`
	args := []any{serverID, since}
	if uuid != "" {
		q += ` AND uuid = ?`
		args = append(args, uuid)
	}
	q += ` GROUP BY 1`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var c string
		var n int64
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		out[c] = n
	}
	return out, rows.Err()
}

type MCSession struct {
	JoinedAt int64 `json:"joinedAt"`
	LeftAt   int64 `json:"leftAt"`
}

func (s *Store) MCSessionsSince(ctx context.Context, serverID, uuid string, since int64) ([]MCSession, error) {
	q := `SELECT joined_at, left_at FROM mc_sessions WHERE server_id = ? AND (left_at = 0 OR left_at >= ?)`
	args := []any{serverID, since}
	if uuid != "" {
		q += ` AND uuid = ?`
		args = append(args, uuid)
	}
	q += ` ORDER BY joined_at`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MCSession{}
	for rows.Next() {
		var m MCSession
		if err := rows.Scan(&m.JoinedAt, &m.LeftAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MCRecentSessions returns the most recent sessions of a player.
func (s *Store) MCRecentSessions(ctx context.Context, serverID, uuid string, limit int) ([]MCSession, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT joined_at, left_at FROM mc_sessions WHERE server_id = ? AND uuid = ?
		ORDER BY joined_at DESC LIMIT ?`, serverID, uuid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MCSession{}
	for rows.Next() {
		var m MCSession
		if err := rows.Scan(&m.JoinedAt, &m.LeftAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
