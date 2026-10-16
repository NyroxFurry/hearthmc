package store

import "context"

// SSHKey is a public key a user can log in with over SFTP.
type SSHKey struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"userId"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"` // SHA256:… like ssh-keygen -l
	PublicKey   string `json:"publicKey"`   // authorized_keys format
	CreatedAt   int64  `json:"createdAt"`
	LastUsedAt  int64  `json:"lastUsedAt"`
}

const sshKeyCols = `id, user_id, name, fingerprint, public_key, created_at, last_used_at`

func scanSSHKey(row interface{ Scan(...any) error }) (*SSHKey, error) {
	var k SSHKey
	if err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.Fingerprint, &k.PublicKey, &k.CreatedAt, &k.LastUsedAt); err != nil {
		return nil, notFound(err)
	}
	return &k, nil
}

func (s *Store) CreateSSHKey(ctx context.Context, k *SSHKey) error {
	k.CreatedAt = now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO ssh_keys (user_id, name, fingerprint, public_key, created_at) VALUES (?, ?, ?, ?, ?)`,
		k.UserID, k.Name, k.Fingerprint, k.PublicKey, k.CreatedAt)
	if err != nil {
		return err
	}
	k.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) ListSSHKeys(ctx context.Context, userID int64) ([]*SSHKey, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sshKeyCols+` FROM ssh_keys WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*SSHKey{}
	for rows.Next() {
		k, err := scanSSHKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// FindSSHKey returns the key of a user with the given fingerprint.
func (s *Store) FindSSHKey(ctx context.Context, userID int64, fingerprint string) (*SSHKey, error) {
	return scanSSHKey(s.DB.QueryRowContext(ctx, `SELECT `+sshKeyCols+` FROM ssh_keys WHERE user_id = ? AND fingerprint = ?`, userID, fingerprint))
}

func (s *Store) TouchSSHKey(ctx context.Context, id int64) {
	s.DB.ExecContext(ctx, `UPDATE ssh_keys SET last_used_at = ? WHERE id = ?`, now(), id)
}

func (s *Store) DeleteSSHKey(ctx context.Context, userID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM ssh_keys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
