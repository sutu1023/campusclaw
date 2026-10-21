// Command migrate copies the legacy SQLite database into MySQL and reindexes
// every migrated material.
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"campusclaw/internal/chunk"
	"campusclaw/internal/config"
	"campusclaw/internal/embed"
	"campusclaw/internal/indexer"
	"campusclaw/internal/store"
	"campusclaw/internal/vector"

	_ "modernc.org/sqlite"
)

func main() {
	sqlitePath := flag.String("sqlite", "./data/app.db", "path to the legacy SQLite database")
	flag.Parse()

	cfg, err := gatewayConfig()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}
	ctx := context.Background()

	src, err := sql.Open("sqlite", *sqlitePath)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer src.Close()

	st, err := store.Open(ctx, cfg.DBDSN, 60*time.Second)
	if err != nil {
		log.Fatalf("database error: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("migrate schema: %v", err)
	}

	if err := copyClasses(ctx, src, st); err != nil {
		log.Fatalf("copy classes: %v", err)
	}
	if err := copyUsers(ctx, src, st); err != nil {
		log.Fatalf("copy users: %v", err)
	}
	if err := copyMaterials(ctx, src, st, cfg.UploadDir); err != nil {
		log.Fatalf("copy materials: %v", err)
	}
	if err := copyEntries(ctx, src, st); err != nil {
		log.Fatalf("copy entries: %v", err)
	}

	// Rebuild chunks and vectors for every migrated material.
	emb := embed.New(cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey, cfg.EmbeddingModel, cfg.EmbeddingDim)
	vec := vector.New(cfg.QdrantURL, cfg.QdrantCollection, cfg.EmbeddingDim)
	if err := vec.EnsureCollection(ctx); err != nil {
		log.Printf("warning: ensure qdrant collection: %v", err)
	}
	idx := &indexer.Indexer{Store: st, Embed: emb, Vector: vec, Model: cfg.EmbeddingModel}

	rows, err := st.DB.QueryContext(ctx, `SELECT id, class_id FROM materials ORDER BY id`)
	if err != nil {
		log.Fatalf("list materials: %v", err)
	}
	type ref struct{ id, classID int64 }
	var refs []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.id, &r.classID); err != nil {
			rows.Close()
			log.Fatalf("scan material: %v", err)
		}
		refs = append(refs, r)
	}
	rows.Close()

	for _, r := range refs {
		if _, err := st.DeleteChunksByMaterial(ctx, r.id); err != nil {
			log.Printf("warning: clear chunks for material %d: %v", r.id, err)
		}
		n, err := idx.IndexMaterial(ctx, r.classID, r.id, chunk.Options{})
		if err != nil {
			log.Printf("warning: index material %d: %v", r.id, err)
			continue
		}
		log.Printf("material %d: indexed %d chunks", r.id, n)
	}
	log.Printf("migration complete")
}

// gatewayConfig loads only the fields the migration needs, without requiring
// the HTTP-only TOKEN_SECRET.
func gatewayConfig() (*config.Config, error) {
	if os.Getenv("TOKEN_SECRET") == "" {
		_ = os.Setenv("TOKEN_SECRET", "migrate-command")
	}
	return config.Load()
}

func copyClasses(ctx context.Context, src *sql.DB, dst *store.Store) error {
	rows, err := src.QueryContext(ctx, `SELECT id, name FROM classes`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		if _, err := dst.DB.ExecContext(ctx,
			`INSERT INTO classes (id, name) VALUES (?,?) ON DUPLICATE KEY UPDATE name=VALUES(name)`,
			id, name); err != nil {
			return err
		}
	}
	return rows.Err()
}

func copyUsers(ctx context.Context, src *sql.DB, dst *store.Store) error {
	rows, err := src.QueryContext(ctx,
		`SELECT id, username, display_name, password_hash, role, class_id FROM users`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, classID int64
		var username, display, hash, role string
		if err := rows.Scan(&id, &username, &display, &hash, &role, &classID); err != nil {
			return err
		}
		if _, err := dst.DB.ExecContext(ctx,
			`INSERT INTO users (id, username, display_name, password_hash, role, class_id)
			 VALUES (?,?,?,?,?,?)
			 ON DUPLICATE KEY UPDATE display_name=VALUES(display_name),
			   password_hash=VALUES(password_hash), role=VALUES(role), class_id=VALUES(class_id)`,
			id, username, display, hash, role, classID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func copyMaterials(ctx context.Context, src *sql.DB, dst *store.Store, uploadDir string) error {
	rows, err := src.QueryContext(ctx,
		`SELECT id, class_id, title, file_path, uploaded_by, created_at FROM materials`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, classID int64
		var title string
		var filePath sql.NullString
		var uploadedBy sql.NullInt64
		var createdAt string
		if err := rows.Scan(&id, &classID, &title, &filePath, &uploadedBy, &createdAt); err != nil {
			return err
		}
		mapped := mapUploadPath(filePath.String, uploadDir)
		if _, err := dst.DB.ExecContext(ctx,
			`INSERT INTO materials (id, class_id, title, file_path, uploaded_by)
			 VALUES (?,?,?,?,?)
			 ON DUPLICATE KEY UPDATE title=VALUES(title), file_path=VALUES(file_path)`,
			id, classID, title, mapped, uploadedBy); err != nil {
			return err
		}
	}
	return rows.Err()
}

func copyEntries(ctx context.Context, src *sql.DB, dst *store.Store) error {
	rows, err := src.QueryContext(ctx,
		`SELECT id, material_id, class_id, body_text FROM knowledge_entries`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, materialID, classID int64
		var body string
		if err := rows.Scan(&id, &materialID, &classID, &body); err != nil {
			return err
		}
		if _, err := dst.DB.ExecContext(ctx,
			`INSERT INTO knowledge_entries (id, material_id, class_id, body_text)
			 VALUES (?,?,?,?)
			 ON DUPLICATE KEY UPDATE body_text=VALUES(body_text)`,
			id, materialID, classID, body); err != nil {
			return err
		}
	}
	return rows.Err()
}

// mapUploadPath rewrites an old /uploads/... path onto the new upload root,
// falling back to the original when the mapped file does not exist.
func mapUploadPath(old, newRoot string) string {
	if old == "" {
		return ""
	}
	const marker = "/uploads/"
	if i := strings.Index(old, marker); i >= 0 {
		rel := old[i+len(marker):]
		mapped := filepath.Join(newRoot, rel)
		if _, err := os.Stat(mapped); err == nil {
			return mapped
		}
	}
	if _, err := os.Stat(old); err == nil {
		return old
	}
	return old
}
