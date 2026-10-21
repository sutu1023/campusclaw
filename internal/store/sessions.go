package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// DeleteSessionsForUser revokes every session of a user. Called on login so a
// new token always replaces any previous one (session fixation defense).
func (s *Store) DeleteSessionsForUser(ctx context.Context, userID int64) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

// CreateSession stores a hashed token for a user. Only the hash is persisted;
// the raw token never touches the database.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?,?,?)`,
		tokenHash, userID, expiresAt); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// SessionUser resolves a non-expired token hash to its user.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (*User, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.display_name, u.password_hash, u.role, u.class_id
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash=? AND s.expires_at > NOW()`, tokenHash)
	return scanUser(row)
}

// DeleteSession revokes a single token (logout).
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes stale rows.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= NOW()`)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CountSessions reports how many rows match a user, used by tests.
func (s *Store) CountSessions(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id=?`, userID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}
