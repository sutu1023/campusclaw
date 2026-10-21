package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Agent is a class-scoped tutor agent: a role prompt plus bindings to the
// skills it may route to and the materials it may retrieve from.
type Agent struct {
	ID          int64     `json:"id"`
	ClassID     int64     `json:"class_id"`
	Name        string    `json:"name"`
	Prompt      string    `json:"prompt"`
	Enabled     bool      `json:"enabled"`
	SkillIDs    []int64   `json:"skill_ids"`
	MaterialIDs []int64   `json:"material_ids"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewAgent is the input for CreateAgent / UpdateAgent.
type NewAgent struct {
	ClassID     int64
	Name        string
	Prompt      string
	Enabled     bool
	SkillIDs    []int64
	MaterialIDs []int64
}

// CreateAgent writes the agent and both binding sets in one transaction.
func (s *Store) CreateAgent(ctx context.Context, na NewAgent) (*Agent, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create agent: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.validateBindings(ctx, tx, na.ClassID, na.SkillIDs, na.MaterialIDs); err != nil {
		return nil, err
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO tutor_agents (class_id, name, prompt, enabled) VALUES (?,?,?,?)`,
		na.ClassID, na.Name, na.Prompt, boolInt(na.Enabled))
	if err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("insert agent: %w", err)
	}
	agentID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := replaceAgentBindings(ctx, tx, agentID, na.SkillIDs, na.MaterialIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create agent: %w", err)
	}
	return s.GetAgent(ctx, na.ClassID, agentID, false)
}

// UpdateAgent replaces the agent fields and both binding sets in one transaction.
// The agent row must belong to the caller's class.
func (s *Store) UpdateAgent(ctx context.Context, classID, id int64, na NewAgent) (*Agent, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update agent: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM tutor_agents WHERE id=? AND class_id=? FOR UPDATE`, id, classID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load agent: %w", err)
	}
	if err := s.validateBindings(ctx, tx, classID, na.SkillIDs, na.MaterialIDs); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE tutor_agents SET name=?, prompt=?, enabled=? WHERE id=? AND class_id=?`,
		na.Name, na.Prompt, boolInt(na.Enabled), id, classID); err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("update agent: %w", err)
	}
	if err := replaceAgentBindings(ctx, tx, id, na.SkillIDs, na.MaterialIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update agent: %w", err)
	}
	return s.GetAgent(ctx, classID, id, false)
}

// ListAgents returns a class's agents. Students pass enabledOnly=true so they
// only ever see enabled agents; teachers see everything.
func (s *Store) ListAgents(ctx context.Context, classID int64, enabledOnly bool) ([]Agent, error) {
	query := `SELECT id, class_id, name, prompt, enabled, created_at, updated_at FROM tutor_agents WHERE class_id=?`
	if enabledOnly {
		query += ` AND enabled=1`
	}
	query += ` ORDER BY id`
	rows, err := s.DB.QueryContext(ctx, query, classID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var out []Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.loadBindings(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// GetAgent fetches an agent scoped to the caller's class. With enabledOnly set,
// a disabled agent is reported as ErrNotFound (the student-facing view).
func (s *Store) GetAgent(ctx context.Context, classID, id int64, enabledOnly bool) (*Agent, error) {
	query := `SELECT id, class_id, name, prompt, enabled, created_at, updated_at FROM tutor_agents WHERE id=? AND class_id=?`
	if enabledOnly {
		query += ` AND enabled=1`
	}
	row := s.DB.QueryRowContext(ctx, query, id, classID)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadBindings(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// ListAgentCandidateSkills returns the skills an agent may route to: bound and
// enabled, scoped to the class.
func (s *Store) ListAgentCandidateSkills(ctx context.Context, classID, agentID int64) ([]Skill, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT sk.id, sk.class_id, sk.name, sk.trigger_text, sk.description, sk.tools_json, sk.source_text,
		        sk.subject_text, sk.enabled, sk.seed_key, sk.created_at, sk.updated_at
		 FROM skills sk
		 JOIN tutor_agent_skills tas ON tas.skill_id = sk.id
		 JOIN tutor_agents a ON a.id = tas.agent_id
		 WHERE tas.agent_id=? AND a.class_id=? AND sk.class_id=? AND sk.enabled=1
		 ORDER BY sk.id`, agentID, classID, classID)
	if err != nil {
		return nil, fmt.Errorf("list candidate skills: %w", err)
	}
	defer rows.Close()
	return collectSkills(rows)
}

// AgentMaterialIDs returns the material ids bound to an agent, scoped to class.
func (s *Store) AgentMaterialIDs(ctx context.Context, classID, agentID int64) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT tam.material_id
		 FROM tutor_agent_materials tam
		 JOIN tutor_agents a ON a.id = tam.agent_id
		 JOIN materials m ON m.id = tam.material_id
		 WHERE tam.agent_id=? AND a.class_id=? AND m.class_id=?
		 ORDER BY tam.material_id`, agentID, classID, classID)
	if err != nil {
		return nil, fmt.Errorf("list agent materials: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) loadBindings(ctx context.Context, a *Agent) error {
	skillIDs, materialIDs, err := s.agentBindings(ctx, a.ID)
	if err != nil {
		return err
	}
	a.SkillIDs = skillIDs
	a.MaterialIDs = materialIDs
	return nil
}

func (s *Store) agentBindings(ctx context.Context, agentID int64) ([]int64, []int64, error) {
	skillIDs, err := s.bindingIDs(ctx, `SELECT skill_id FROM tutor_agent_skills WHERE agent_id=? ORDER BY skill_id`, agentID)
	if err != nil {
		return nil, nil, err
	}
	materialIDs, err := s.bindingIDs(ctx, `SELECT material_id FROM tutor_agent_materials WHERE agent_id=? ORDER BY material_id`, agentID)
	if err != nil {
		return nil, nil, err
	}
	return skillIDs, materialIDs, nil
}

func (s *Store) bindingIDs(ctx context.Context, query string, agentID int64) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, query, agentID)
	if err != nil {
		return nil, fmt.Errorf("load binding: %w", err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// validateBindings rejects any skill or material that is not owned by the class.
// Cross-class and missing ids both surface as ErrNotFound.
func (s *Store) validateBindings(ctx context.Context, tx *sql.Tx, classID int64, skillIDs, materialIDs []int64) error {
	if err := validateOwned(ctx, tx, `SELECT COUNT(*) FROM skills WHERE class_id=? AND id IN (%s)`, classID, skillIDs); err != nil {
		return err
	}
	return validateOwned(ctx, tx, `SELECT COUNT(*) FROM materials WHERE class_id=? AND id IN (%s)`, classID, materialIDs)
}

func validateOwned(ctx context.Context, tx *sql.Tx, format string, classID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := ""
	args := []any{classID}
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	var count int
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(format, placeholders), args...).Scan(&count); err != nil {
		return fmt.Errorf("validate bindings: %w", err)
	}
	if count != len(ids) {
		return ErrNotFound
	}
	return nil
}

func replaceAgentBindings(ctx context.Context, tx *sql.Tx, agentID int64, skillIDs, materialIDs []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM tutor_agent_skills WHERE agent_id=?`, agentID); err != nil {
		return fmt.Errorf("clear skill bindings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tutor_agent_materials WHERE agent_id=?`, agentID); err != nil {
		return fmt.Errorf("clear material bindings: %w", err)
	}
	for _, id := range skillIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tutor_agent_skills (agent_id, skill_id) VALUES (?,?)`, agentID, id); err != nil {
			return fmt.Errorf("bind skill: %w", err)
		}
	}
	for _, id := range materialIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tutor_agent_materials (agent_id, material_id) VALUES (?,?)`, agentID, id); err != nil {
			return fmt.Errorf("bind material: %w", err)
		}
	}
	return nil
}

type agentScanner interface {
	Scan(dest ...any) error
}

func scanAgent(row agentScanner) (*Agent, error) {
	var a Agent
	var enabled int
	if err := row.Scan(&a.ID, &a.ClassID, &a.Name, &a.Prompt, &enabled, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	a.Enabled = enabled != 0
	return &a, nil
}
