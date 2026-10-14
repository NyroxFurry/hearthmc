// Package store contains all database access for the panel.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	DB *sql.DB
}

func New(db *sql.DB) *Store { return &Store{DB: db} }

func now() int64 { return time.Now().Unix() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// RandomID returns a random lowercase alphanumeric id of length n.
func RandomID(n int) string {
	const alphabet = "abcdefghijkmnopqrstuvwxyz23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// IsUniqueViolation reports whether err is a UNIQUE constraint failure.
func IsUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Tx runs fn inside a transaction.
func (s *Store) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Cleanup removes old data (expired sessions, old metrics, old audit entries).
func (s *Store) Cleanup(ctx context.Context, metricsDays, auditDays, chatDays int) {
	t := now()
	s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, t)
	if metricsDays > 0 {
		s.DB.ExecContext(ctx, `DELETE FROM metrics WHERE at < ?`, t-int64(metricsDays)*86400)
	}
	if auditDays > 0 {
		s.DB.ExecContext(ctx, `DELETE FROM audit_log WHERE at < ?`, t-int64(auditDays)*86400)
	}
	if chatDays > 0 {
		s.DB.ExecContext(ctx, `DELETE FROM mc_events WHERE type = 'chat' AND at < ?`, t-int64(chatDays)*86400)
	}
	s.DB.ExecContext(ctx, `DELETE FROM invites WHERE used_at = 0 AND expires_at < ?`, t-7*86400)
}
