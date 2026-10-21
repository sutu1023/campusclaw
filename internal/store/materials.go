package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Material is an uploaded teaching material.
type Material struct {
	ID         int64     `json:"id"`
	ClassID    int64     `json:"class_id"`
	Title      string    `json:"title"`
	FilePath   string    `json:"file_path,omitempty"`
	UploadedBy *int64    `json:"uploaded_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// KnowledgeEntry holds the original uploaded text for a material.
type KnowledgeEntry struct {
	ID         int64     `json:"id"`
	MaterialID int64     `json:"material_id"`
	ClassID    int64     `json:"class_id"`
	BodyText   string    `json:"body_text"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListMaterials returns one page of a class's materials plus the total count.
// The class filter is mandatory and always server-derived.
func (s *Store) ListMaterials(ctx context.Context, classID int64, page, pageSize int, title string) ([]Material, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	where := `class_id=?`
	args := []any{classID}
	if title != "" {
		where += ` AND title LIKE ?`
		args = append(args, "%"+title+"%")
	}

	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM materials WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count materials: %w", err)
	}

	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, class_id, title, file_path, uploaded_by, created_at
		 FROM materials WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list materials: %w", err)
	}
	defer rows.Close()

	var out []Material
	for rows.Next() {
		m, err := scanMaterial(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *m)
	}
	return out, total, rows.Err()
}

// GetMaterial fetches a material scoped to its class. Cross-class and missing
// rows both return ErrNotFound so existence is never leaked.
func (s *Store) GetMaterial(ctx context.Context, classID, materialID int64) (*Material, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id, class_id, title, file_path, uploaded_by, created_at
		 FROM materials WHERE id=? AND class_id=?`, materialID, classID)
	m, err := scanMaterialRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

// CreateMaterialWithEntry writes the material and its original text in one
// transaction. Either both rows commit or neither does.
func (s *Store) CreateMaterialWithEntry(ctx context.Context, classID int64, title, filePath string, uploadedBy int64, bodyText string) (int64, int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO materials (class_id, title, file_path, uploaded_by) VALUES (?,?,?,?)`,
		classID, title, filePath, uploadedBy)
	if err != nil {
		return 0, 0, fmt.Errorf("insert material: %w", err)
	}
	materialID, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}

	res, err = tx.ExecContext(ctx,
		`INSERT INTO knowledge_entries (material_id, class_id, body_text) VALUES (?,?,?)`,
		materialID, classID, bodyText)
	if err != nil {
		return 0, 0, fmt.Errorf("insert knowledge entry: %w", err)
	}
	entryID, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit upload: %w", err)
	}
	return materialID, entryID, nil
}

// MaterialRef identifies a material for the startup ensure-index pass.
type MaterialRef struct {
	ID      int64
	ClassID int64
}

// ListMaterialsToIndex returns materials that have no ready chunk yet. It is
// used by the startup/backfill paths to (re)build the demo index idempotently.
func (s *Store) ListMaterialsToIndex(ctx context.Context) ([]MaterialRef, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT m.id, m.class_id
		 FROM materials m
		 LEFT JOIN knowledge_entries e ON e.material_id = m.id
		 LEFT JOIN knowledge_chunks c ON c.knowledge_entry_id = e.id AND c.index_status = 'ready'
		 GROUP BY m.id, m.class_id
		 HAVING COUNT(c.id) = 0
		 ORDER BY m.id`)
	if err != nil {
		return nil, fmt.Errorf("list materials to index: %w", err)
	}
	defer rows.Close()
	var out []MaterialRef
	for rows.Next() {
		var ref MaterialRef
		if err := rows.Scan(&ref.ID, &ref.ClassID); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// GetEntryByMaterial returns the original text row for a class-scoped material.
func (s *Store) GetEntryByMaterial(ctx context.Context, classID, materialID int64) (*KnowledgeEntry, error) {
	var e KnowledgeEntry
	err := s.DB.QueryRowContext(ctx,
		`SELECT id, material_id, class_id, body_text, created_at
		 FROM knowledge_entries WHERE material_id=? AND class_id=? LIMIT 1`, materialID, classID).
		Scan(&e.ID, &e.MaterialID, &e.ClassID, &e.BodyText, &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get entry: %w", err)
	}
	return &e, nil
}

type materialScanner interface {
	Scan(dest ...any) error
}

func scanMaterialRow(row materialScanner) (*Material, error) {
	var m Material
	var filePath sql.NullString
	var uploadedBy sql.NullInt64
	if err := row.Scan(&m.ID, &m.ClassID, &m.Title, &filePath, &uploadedBy, &m.CreatedAt); err != nil {
		return nil, err
	}
	m.FilePath = filePath.String
	if uploadedBy.Valid {
		m.UploadedBy = &uploadedBy.Int64
	}
	return &m, nil
}

func scanMaterial(rows *sql.Rows) (*Material, error) { return scanMaterialRow(rows) }
