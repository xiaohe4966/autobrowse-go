package migrations

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"sort"
	"strings"
)

// FS embeds the migrations directory.
//go:embed *.sql
var FS embed.FS

// AutoMigrate runs all pending SQL migrations in order.
func AutoMigrate(db *sql.DB) error {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := isApplied(db, name)
		if err != nil {
			return fmt.Errorf("check migration %q: %w", name, err)
		}
		if applied {
			log.Printf("[migrations] already applied: %s", name)
			continue
		}

		sqlBytes, err := FS.ReadFile(filepath.Join(".", name))
		if err != nil {
			return fmt.Errorf("read migration %q: %w", name, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for %q: %w", name, err)
		}

		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("execute migration %q: %w", name, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO migrations (name) VALUES (?)",
			name,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %q: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %q: %w", name, err)
		}

		log.Printf("[migrations] applied: %s", name)
	}

	return nil
}

func isApplied(db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM migrations WHERE name = ?",
		name,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
