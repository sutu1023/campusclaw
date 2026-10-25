package store

import (
	"context"
	"errors"
	"fmt"
)

// migrateMCPColumns upgrades pre-MCP databases without resetting rows/volumes.
// CREATE TABLE IF NOT EXISTS alone cannot widen or add existing columns.
func (s *Store) migrateMCPColumns(ctx context.Context) error {
	var dataType string
	if err := s.DB.QueryRowContext(ctx, `SELECT DATA_TYPE FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='skills' AND column_name='tools_json'`).Scan(&dataType); err != nil {
		return fmt.Errorf("inspect skills.tools_json migration: %w", err)
	}
	switch dataType {
	case "varchar", "char":
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE skills MODIFY COLUMN tools_json TEXT NOT NULL`); err != nil {
			return fmt.Errorf("expand skills.tools_json: %w", err)
		}
	case "text", "mediumtext", "longtext":
		// Never shrink an already-upgraded or more generous text column.
	default:
		return errors.New("unsupported existing skills.tools_json column type")
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='messages' AND column_name='tool_calls_json'`).Scan(&count); err != nil {
		return fmt.Errorf("inspect messages.tool_calls_json migration: %w", err)
	}
	if count == 0 {
		if _, err := s.DB.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN tool_calls_json MEDIUMTEXT NULL`); err != nil {
			return fmt.Errorf("add messages.tool_calls_json: %w", err)
		}
	}
	return nil
}
