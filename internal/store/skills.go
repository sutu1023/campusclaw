package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// Skill is one class-scoped, teacher-authored guidance file parsed into columns.
type Skill struct {
	ID          int64     `json:"id"`
	ClassID     int64     `json:"class_id"`
	Name        string    `json:"name"`
	Trigger     string    `json:"trigger"`
	Description string    `json:"description"`
	Tools       []string  `json:"tools"`
	Source      string    `json:"source"`
	Subject     string    `json:"subject"`
	Enabled     bool      `json:"enabled"`
	SeedKey     string    `json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewSkill is the input for CreateSkill / UpdateSkill.
type NewSkill struct {
	ClassID     int64
	Name        string
	Trigger     string
	Description string
	Tools       []string
	Source      string
	Subject     string
	Enabled     bool
}

// isDuplicateKey reports a MySQL unique-constraint violation.
func isDuplicateKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func encodeTools(tools []string) string {
	if len(tools) == 0 {
		return "[]"
	}
	buf, err := json.Marshal(tools)
	if err != nil {
		return "[]"
	}
	return string(buf)
}

func decodeTools(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var tools []string
	if err := json.Unmarshal([]byte(raw), &tools); err != nil {
		return []string{}
	}
	if tools == nil {
		return []string{}
	}
	return tools
}

// CreateSkill inserts a teacher-authored skill for a class.
func (s *Store) CreateSkill(ctx context.Context, ns NewSkill) (*Skill, error) {
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO skills (class_id, name, trigger_text, description, tools_json, source_text, subject_text, enabled)
		 VALUES (?,?,?,?,?,?,?,?)`,
		ns.ClassID, ns.Name, ns.Trigger, ns.Description, encodeTools(ns.Tools), ns.Source, ns.Subject, boolInt(ns.Enabled))
	if err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("insert skill: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetSkill(ctx, ns.ClassID, id)
}

// UpdateSkill replaces a skill's fields, scoped to the caller's class.
func (s *Store) UpdateSkill(ctx context.Context, classID, id int64, ns NewSkill) (*Skill, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE skills SET name=?, trigger_text=?, description=?, tools_json=?, source_text=?, subject_text=?, enabled=?
		 WHERE id=? AND class_id=?`,
		ns.Name, ns.Trigger, ns.Description, encodeTools(ns.Tools), ns.Source, ns.Subject, boolInt(ns.Enabled), id, classID)
	if err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("update skill: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Either the row is missing or nothing changed; confirm existence.
		if _, err := s.GetSkill(ctx, classID, id); err != nil {
			return nil, err
		}
	}
	return s.GetSkill(ctx, classID, id)
}

// ListSkills returns every skill of a class, newest first.
func (s *Store) ListSkills(ctx context.Context, classID int64) ([]Skill, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, class_id, name, trigger_text, description, tools_json, source_text, subject_text, enabled, seed_key, created_at, updated_at
		 FROM skills WHERE class_id=? ORDER BY id`, classID)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()
	return collectSkills(rows)
}

// GetSkill fetches a skill scoped to the caller's class; cross-class and missing
// rows both return ErrNotFound so existence is never leaked.
func (s *Store) GetSkill(ctx context.Context, classID, id int64) (*Skill, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, class_id, name, trigger_text, description, tools_json, source_text, subject_text, enabled, seed_key, created_at, updated_at
		 FROM skills WHERE id=? AND class_id=?`, id, classID)
	if err != nil {
		return nil, fmt.Errorf("get skill: %w", err)
	}
	defer rows.Close()
	list, err := collectSkills(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return &list[0], nil
}

// DeleteSkill removes a skill and its agent bindings in one transaction.
func (s *Store) DeleteSkill(ctx context.Context, classID, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete skill: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `DELETE FROM skills WHERE id=? AND class_id=?`, id, classID)
	if err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tutor_agent_skills WHERE skill_id=?`, id); err != nil {
		return fmt.Errorf("clear skill bindings: %w", err)
	}
	return tx.Commit()
}

func collectSkills(rows *sql.Rows) ([]Skill, error) {
	var out []Skill
	for rows.Next() {
		var sk Skill
		var tools string
		var seed sql.NullString
		var enabled int
		if err := rows.Scan(&sk.ID, &sk.ClassID, &sk.Name, &sk.Trigger, &sk.Description, &tools,
			&sk.Source, &sk.Subject, &enabled, &seed, &sk.CreatedAt, &sk.UpdatedAt); err != nil {
			return nil, err
		}
		sk.Tools = decodeTools(tools)
		sk.Enabled = enabled != 0
		sk.SeedKey = seed.String
		out = append(out, sk)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
