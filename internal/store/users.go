package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNotFound is returned when a class-scoped lookup finds no row.
var ErrNotFound = errors.New("not found")

// ErrDuplicate signals a class-scoped unique constraint (e.g. a duplicate
// agent or skill name). Handlers turn it into a 400.
var ErrDuplicate = errors.New("duplicate")

// User is a CampusClaw account. ClassID is the tenant boundary.
type User struct {
	ID           int64
	Username     string
	DisplayName  string
	PasswordHash string
	Role         string
	ClassID      int64
}

// GetUserByUsername looks up an account by its unique username.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, username, display_name, password_hash, role, class_id
		 FROM users WHERE username=?`, username)
	return scanUser(row)
}

// GetUserByID looks up an account by primary key.
func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, username, display_name, password_hash, role, class_id
		 FROM users WHERE id=?`, id)
	return scanUser(row)
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.Role, &u.ClassID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &u, nil
}
