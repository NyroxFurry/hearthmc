package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
)

// ---- settings ----

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool) {
	var v string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return "", false
	}
	return v, true
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key)
	return err
}

func (s *Store) AllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ---- backups ----

type Backup struct {
	ID         string `json:"id"`
	ServerID   string `json:"serverId"`
	Name       string `json:"name"`
	File       string `json:"-"`
	Size       int64  `json:"size"`
	Files      int64  `json:"files"`
	Status     string `json:"status"` // running, ok, failed
	Error      string `json:"error"`
	Locked     bool   `json:"locked"`
	CreatedBy  int64  `json:"createdBy"`
	CreatedAt  int64  `json:"createdAt"`
	FinishedAt int64  `json:"finishedAt"`
}

const backupCols = `id, server_id, name, file, size, files, status, error, locked, COALESCE(created_by, 0), created_at, finished_at`

func scanBackup(row interface{ Scan(...any) error }) (*Backup, error) {
	var b Backup
	if err := row.Scan(&b.ID, &b.ServerID, &b.Name, &b.File, &b.Size, &b.Files, &b.Status, &b.Error, &b.Locked,
		&b.CreatedBy, &b.CreatedAt, &b.FinishedAt); err != nil {
		return nil, notFound(err)
	}
	return &b, nil
}

func (s *Store) CreateBackup(ctx context.Context, b *Backup) error {
	b.CreatedAt = now()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO backups (id, server_id, name, file, size, files, status, error, locked, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, b.ID, b.ServerID, b.Name, b.File, b.Size, b.Files, b.Status, b.Error,
		b2i(b.Locked), b.CreatedBy, b.CreatedAt)
	return err
}

func (s *Store) UpdateBackup(ctx context.Context, b *Backup) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE backups SET name = ?, size = ?, files = ?, status = ?, error = ?, locked = ?, finished_at = ? WHERE id = ?`,
		b.Name, b.Size, b.Files, b.Status, b.Error, b2i(b.Locked), b.FinishedAt, b.ID)
	return err
}

func (s *Store) GetBackup(ctx context.Context, id string) (*Backup, error) {
	return scanBackup(s.DB.QueryRowContext(ctx, `SELECT `+backupCols+` FROM backups WHERE id = ?`, id))
}

func (s *Store) ListBackups(ctx context.Context, serverID string) ([]*Backup, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+backupCols+` FROM backups WHERE server_id = ? ORDER BY created_at DESC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Backup{}
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteBackup(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM backups WHERE id = ?`, id)
	return err
}

// MarkRunningBackupsFailed is called at startup: backups that were running when
// the panel stopped can never finish.
func (s *Store) MarkRunningBackupsFailed(ctx context.Context) {
	s.DB.ExecContext(ctx, `UPDATE backups SET status = 'failed', error = 'Panel wurde während des Backups beendet', finished_at = ? WHERE status = 'running'`, now())
}

// ---- schedules ----

type ScheduleStep struct {
	Type    string `json:"type"`              // command, power, backup, wait, say
	Value   string `json:"value"`             // command text / power action / backup name / message
	Seconds int    `json:"seconds,omitempty"` // for wait
}

type Schedule struct {
	ID             int64          `json:"id"`
	ServerID       string         `json:"serverId"`
	Name           string         `json:"name"`
	Cron           string         `json:"cron"`
	Enabled        bool           `json:"enabled"`
	OnlyWhenOnline bool           `json:"onlyWhenOnline"`
	Steps          []ScheduleStep `json:"steps"`
	LastRunAt      int64          `json:"lastRunAt"`
	LastStatus     string         `json:"lastStatus"`
	LastError      string         `json:"lastError"`
	CreatedAt      int64          `json:"createdAt"`
}

const scheduleCols = `id, server_id, name, cron, enabled, only_when_online, steps, last_run_at, last_status, last_error, created_at`

func scanSchedule(row interface{ Scan(...any) error }) (*Schedule, error) {
	var sc Schedule
	var steps string
	if err := row.Scan(&sc.ID, &sc.ServerID, &sc.Name, &sc.Cron, &sc.Enabled, &sc.OnlyWhenOnline, &steps,
		&sc.LastRunAt, &sc.LastStatus, &sc.LastError, &sc.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	json.Unmarshal([]byte(steps), &sc.Steps)
	if sc.Steps == nil {
		sc.Steps = []ScheduleStep{}
	}
	return &sc, nil
}

func (s *Store) ListSchedules(ctx context.Context, serverID string) ([]*Schedule, error) {
	q := `SELECT ` + scheduleCols + ` FROM schedules`
	args := []any{}
	if serverID != "" {
		q += ` WHERE server_id = ?`
		args = append(args, serverID)
	}
	q += ` ORDER BY id`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Schedule{}
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Store) GetSchedule(ctx context.Context, id int64) (*Schedule, error) {
	return scanSchedule(s.DB.QueryRowContext(ctx, `SELECT `+scheduleCols+` FROM schedules WHERE id = ?`, id))
}

func (s *Store) CreateSchedule(ctx context.Context, sc *Schedule) error {
	sc.CreatedAt = now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO schedules (server_id, name, cron, enabled, only_when_online, steps, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, sc.ServerID, sc.Name, sc.Cron, b2i(sc.Enabled), b2i(sc.OnlyWhenOnline),
		mustJSON(sc.Steps), sc.CreatedAt)
	if err != nil {
		return err
	}
	sc.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateSchedule(ctx context.Context, sc *Schedule) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE schedules SET name = ?, cron = ?, enabled = ?, only_when_online = ?, steps = ? WHERE id = ?`,
		sc.Name, sc.Cron, b2i(sc.Enabled), b2i(sc.OnlyWhenOnline), mustJSON(sc.Steps), sc.ID)
	return err
}

func (s *Store) SetScheduleResult(ctx context.Context, id int64, at int64, status, errMsg string) {
	s.DB.ExecContext(ctx, `UPDATE schedules SET last_run_at = ?, last_status = ?, last_error = ? WHERE id = ?`, at, status, errMsg, id)
}

func (s *Store) DeleteSchedule(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	return err
}

// ---- audit log ----

type AuditEntry struct {
	ID       int64           `json:"id"`
	At       int64           `json:"at"`
	UserID   int64           `json:"userId"`
	Username string          `json:"username"`
	ServerID string          `json:"serverId"`
	Action   string          `json:"action"`
	Details  json.RawMessage `json:"details"`
	IP       string          `json:"ip"`
}

func (s *Store) AddAudit(ctx context.Context, e *AuditEntry) {
	if e.At == 0 {
		e.At = now()
	}
	if e.Details == nil {
		e.Details = json.RawMessage("{}")
	}
	var uid, sid any
	if e.UserID != 0 {
		uid = e.UserID
	}
	if e.ServerID != "" {
		sid = e.ServerID
	}
	s.DB.ExecContext(ctx, `INSERT INTO audit_log (at, user_id, username, server_id, action, details, ip) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.At, uid, e.Username, sid, e.Action, string(e.Details), e.IP)
}

type AuditFilter struct {
	ServerID string
	UserID   int64
	Action   string // prefix match
	Before   int64  // id cursor
	Limit    int
}

func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]*AuditEntry, error) {
	var where []string
	var args []any
	if f.ServerID != "" {
		where = append(where, "server_id = ?")
		args = append(args, f.ServerID)
	}
	if f.UserID != 0 {
		where = append(where, "user_id = ?")
		args = append(args, f.UserID)
	}
	if f.Action != "" {
		where = append(where, "action LIKE ?")
		args = append(args, strings.ReplaceAll(f.Action, "%", "")+"%")
	}
	if f.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, f.Before)
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := `SELECT id, at, COALESCE(user_id, 0), username, COALESCE(server_id, ''), action, details, ip FROM audit_log`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY id DESC LIMIT " + strconv.Itoa(f.Limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var d string
		if err := rows.Scan(&e.ID, &e.At, &e.UserID, &e.Username, &e.ServerID, &e.Action, &d, &e.IP); err != nil {
			return nil, err
		}
		e.Details = json.RawMessage(d)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// ---- webhooks ----

type Webhook struct {
	ID         int64    `json:"id"`
	ServerID   string   `json:"serverId"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"` // discord, generic
	URL        string   `json:"url"`
	Events     []string `json:"events"`
	Enabled    bool     `json:"enabled"`
	LastStatus string   `json:"lastStatus"`
	CreatedAt  int64    `json:"createdAt"`
}

const webhookCols = `id, COALESCE(server_id, ''), name, kind, url, events, enabled, last_status, created_at`

func scanWebhook(row interface{ Scan(...any) error }) (*Webhook, error) {
	var w Webhook
	var ev string
	if err := row.Scan(&w.ID, &w.ServerID, &w.Name, &w.Kind, &w.URL, &ev, &w.Enabled, &w.LastStatus, &w.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	json.Unmarshal([]byte(ev), &w.Events)
	if w.Events == nil {
		w.Events = []string{}
	}
	return &w, nil
}

// ListWebhooks returns hooks for a server; serverID "" returns the global hooks;
// serverID "*" returns all hooks.
func (s *Store) ListWebhooks(ctx context.Context, serverID string) ([]*Webhook, error) {
	var rows *sql.Rows
	var err error
	switch serverID {
	case "*":
		rows, err = s.DB.QueryContext(ctx, `SELECT `+webhookCols+` FROM webhooks ORDER BY id`)
	case "":
		rows, err = s.DB.QueryContext(ctx, `SELECT `+webhookCols+` FROM webhooks WHERE server_id IS NULL ORDER BY id`)
	default:
		rows, err = s.DB.QueryContext(ctx, `SELECT `+webhookCols+` FROM webhooks WHERE server_id = ? ORDER BY id`, serverID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Webhook{}
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) GetWebhook(ctx context.Context, id int64) (*Webhook, error) {
	return scanWebhook(s.DB.QueryRowContext(ctx, `SELECT `+webhookCols+` FROM webhooks WHERE id = ?`, id))
}

func (s *Store) SaveWebhook(ctx context.Context, w *Webhook) error {
	if w.Events == nil {
		w.Events = []string{}
	}
	var sid any
	if w.ServerID != "" {
		sid = w.ServerID
	}
	if w.ID == 0 {
		w.CreatedAt = now()
		res, err := s.DB.ExecContext(ctx, `INSERT INTO webhooks (server_id, name, kind, url, events, enabled, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, sid, w.Name, w.Kind, w.URL, mustJSON(w.Events), b2i(w.Enabled), w.CreatedAt)
		if err != nil {
			return err
		}
		w.ID, _ = res.LastInsertId()
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE webhooks SET name = ?, kind = ?, url = ?, events = ?, enabled = ? WHERE id = ?`,
		w.Name, w.Kind, w.URL, mustJSON(w.Events), b2i(w.Enabled), w.ID)
	return err
}

func (s *Store) SetWebhookStatus(ctx context.Context, id int64, status string) {
	s.DB.ExecContext(ctx, `UPDATE webhooks SET last_status = ? WHERE id = ?`, status, id)
}

func (s *Store) DeleteWebhook(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
	return err
}

// ---- metrics ----

type MetricPoint struct {
	At      int64   `json:"t"`
	CPU     float64 `json:"cpu"`
	Mem     int64   `json:"mem"`
	Heap    int64   `json:"heap"` // Java heap in use, 0 = unknown
	Players int     `json:"players"`
}

func (s *Store) AddMetric(ctx context.Context, serverID string, p MetricPoint) {
	s.DB.ExecContext(ctx, `INSERT OR REPLACE INTO metrics (server_id, at, cpu, mem, heap, players) VALUES (?, ?, ?, ?, ?, ?)`,
		serverID, p.At, p.CPU, p.Mem, p.Heap, p.Players)
}

// Metrics returns points in [from, now], downsampled into buckets of bucketSec seconds.
func (s *Store) Metrics(ctx context.Context, serverID string, from int64, bucketSec int64) ([]MetricPoint, error) {
	if bucketSec < 1 {
		bucketSec = 1
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT (at / ?) * ? AS b, AVG(cpu), AVG(mem), AVG(heap), MAX(players)
		FROM metrics WHERE server_id = ? AND at >= ? GROUP BY b ORDER BY b`, bucketSec, bucketSec, serverID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetricPoint{}
	for rows.Next() {
		var p MetricPoint
		var mem, heap float64
		if err := rows.Scan(&p.At, &p.CPU, &mem, &heap, &p.Players); err != nil {
			return nil, err
		}
		p.Mem, p.Heap = int64(mem), int64(heap)
		out = append(out, p)
	}
	return out, rows.Err()
}
