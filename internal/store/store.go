// Package store provides sqlite-backed persistence for timetrack.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the database at path and applies
// any pending migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var applied int
		if err := s.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE name = ?`, name).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		sqlText, err := fs.ReadFile(migrationFS, name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlText)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

type Project struct {
	ID       int64
	Name     string
	Color    string
	Archived bool
}

func (s *Store) CreateProject(name, color string) (Project, error) {
	if name == "" {
		return Project{}, fmt.Errorf("project name is required")
	}
	res, err := s.db.Exec(`INSERT INTO projects (name, color) VALUES (?, ?)`, name, color)
	if err != nil {
		return Project{}, fmt.Errorf("create project %q: %w", name, err)
	}
	id, _ := res.LastInsertId()
	return Project{ID: id, Name: name, Color: color}, nil
}

func (s *Store) ProjectByName(name string) (Project, error) {
	var p Project
	err := s.db.QueryRow(`SELECT id, name, color, archived FROM projects WHERE name = ? COLLATE NOCASE`, name).
		Scan(&p.ID, &p.Name, &p.Color, &p.Archived)
	if err == sql.ErrNoRows {
		return Project{}, fmt.Errorf("unknown project %q", name)
	}
	return p, err
}

func (s *Store) Projects(includeArchived bool) ([]Project, error) {
	q := `SELECT id, name, color, archived FROM projects`
	if !includeArchived {
		q += ` WHERE archived = 0`
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ps []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Color, &p.Archived); err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	return ps, rows.Err()
}

func (s *Store) UpdateProject(p Project) error {
	_, err := s.db.Exec(`UPDATE projects SET name = ?, color = ?, archived = ? WHERE id = ?`,
		p.Name, p.Color, p.Archived, p.ID)
	return err
}
