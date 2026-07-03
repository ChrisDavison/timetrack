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
	"strings"

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
	// Single connection: PRAGMAs are per-connection, and a lone user never
	// needs concurrent connections.
	db.SetMaxOpenConns(1)
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
	// Migrations may rebuild tables that other tables reference (the
	// standard SQLite recipe), so foreign keys are off while they run and
	// integrity is checked afterwards.
	if _, err := s.db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer s.db.Exec(`PRAGMA foreign_keys=ON`)
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
	rows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("foreign key violations found after migration")
	}
	return rows.Err()
}

type Project struct {
	ID         int64
	Name       string // own segment name, without the parent prefix
	Color      string
	Archived   bool
	System     bool   // permanent project (e.g. "Holiday"); cannot be archived or deleted
	ParentID   int64  // 0 for top-level projects
	ParentName string // "" for top-level projects
}

// Path is the project's full reference name: "Parent/Child" or a bare
// top-level name. This form is accepted everywhere a project is named.
func (p Project) Path() string {
	if p.ParentName != "" {
		return p.ParentName + "/" + p.Name
	}
	return p.Name
}

// splitPath breaks "Parent/Child" into its segments; a bare name returns
// parent "".
func splitPath(name string) (parent, child string, err error) {
	parts := strings.Split(name, "/")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
		if parts[i] == "" {
			return "", "", fmt.Errorf("invalid project name %q", name)
		}
	}
	switch len(parts) {
	case 1:
		return "", parts[0], nil
	case 2:
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("project %q: only one level of nesting is supported", name)
	}
}

// CreateProject creates a project. A "Parent/Child" name creates a
// sub-project of an existing top-level project; a child with no color
// inherits its parent's.
func (s *Store) CreateProject(name, color string) (Project, error) {
	parentName, own, err := splitPath(strings.TrimSpace(name))
	if err != nil {
		return Project{}, err
	}
	var parentID any // nil for top-level
	parentDisplay := ""
	if parentName != "" {
		parent, err := s.ProjectByName(parentName)
		if err != nil {
			return Project{}, err
		}
		if parent.ParentID != 0 {
			return Project{}, fmt.Errorf("%q is a sub-project and cannot have children", parentName)
		}
		parentID = parent.ID
		parentDisplay = parent.Name
		if color == "" {
			color = parent.Color
		}
	}
	res, err := s.db.Exec(`INSERT INTO projects (name, color, parent_id) VALUES (?, ?, ?)`, own, color, parentID)
	if err != nil {
		return Project{}, fmt.Errorf("create project %q: %w", name, err)
	}
	id, _ := res.LastInsertId()
	p := Project{ID: id, Name: own, Color: color, ParentName: parentDisplay}
	if parentID != nil {
		p.ParentID = parentID.(int64)
	}
	return p, nil
}

const projectSelect = `
	SELECT p.id, p.name, p.color, p.archived, p.system, COALESCE(p.parent_id, 0), COALESCE(pp.name, '')
	FROM projects p LEFT JOIN projects pp ON pp.id = p.parent_id`

func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.Color, &p.Archived, &p.System, &p.ParentID, &p.ParentName)
	return p, err
}

// ProjectByName resolves a bare top-level name or a "Parent/Child" path.
func (s *Store) ProjectByName(name string) (Project, error) {
	parentName, own, err := splitPath(strings.TrimSpace(name))
	if err != nil {
		return Project{}, err
	}
	var row *sql.Row
	if parentName == "" {
		row = s.db.QueryRow(projectSelect+` WHERE p.name = ? COLLATE NOCASE AND p.parent_id IS NULL`, own)
	} else {
		row = s.db.QueryRow(projectSelect+` WHERE p.name = ? COLLATE NOCASE AND pp.name = ? COLLATE NOCASE`, own, parentName)
	}
	p, err := scanProject(row)
	if err == sql.ErrNoRows {
		return Project{}, fmt.Errorf("unknown project %q", name)
	}
	return p, err
}

// ProjectByID resolves a project by its numeric ID.
func (s *Store) ProjectByID(id int64) (Project, error) {
	p, err := scanProject(s.db.QueryRow(projectSelect+` WHERE p.id = ?`, id))
	if err == sql.ErrNoRows {
		return Project{}, fmt.Errorf("no project with id %d", id)
	}
	return p, err
}

// Projects lists projects in tree order: each top-level project followed by
// its sub-projects.
func (s *Store) Projects(includeArchived bool) ([]Project, error) {
	q := projectSelect
	if !includeArchived {
		q += ` WHERE p.archived = 0`
	}
	q += ` ORDER BY COALESCE(pp.name, p.name) COLLATE NOCASE, p.parent_id IS NOT NULL, p.name COLLATE NOCASE`
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ps []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		ps = append(ps, p)
	}
	return ps, rows.Err()
}

// UpdateProject saves name, color, and archived state. Archiving a parent
// archives its sub-projects too.
func (s *Store) UpdateProject(p Project) error {
	if strings.Contains(p.Name, "/") {
		return fmt.Errorf("project name may not contain '/'")
	}
	if p.System && p.Archived {
		return fmt.Errorf("%q is a system project and cannot be archived", p.Name)
	}
	_, err := s.db.Exec(`UPDATE projects SET name = ?, color = ?, archived = ? WHERE id = ?`,
		p.Name, p.Color, p.Archived, p.ID)
	if err != nil {
		return err
	}
	if p.Archived {
		_, err = s.db.Exec(`UPDATE projects SET archived = 1 WHERE parent_id = ?`, p.ID)
	}
	return err
}

// DeleteProject removes a project. It refuses when the project still has
// time entries or sub-projects; archive such projects instead. System
// projects (e.g. "Holiday") can never be deleted.
func (s *Store) DeleteProject(id int64) error {
	p, err := s.ProjectByID(id)
	if err != nil {
		return err
	}
	if p.System {
		return fmt.Errorf("%q is a system project and cannot be deleted", p.Name)
	}
	var entryCount int
	if err := s.db.QueryRow(`SELECT count(*) FROM entries WHERE project_id = ?`, id).Scan(&entryCount); err != nil {
		return err
	}
	if entryCount > 0 {
		return fmt.Errorf("project has %d entries; reassign or delete them first", entryCount)
	}
	var childCount int
	if err := s.db.QueryRow(`SELECT count(*) FROM projects WHERE parent_id = ?`, id).Scan(&childCount); err != nil {
		return err
	}
	if childCount > 0 {
		return fmt.Errorf("project has sub-projects; delete or move them first")
	}
	_, err = s.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

// SetParent re-parents a project. An empty parentPath makes it top-level; a
// non-empty name makes it a sub-project of that (top-level) project.
func (s *Store) SetParent(id int64, parentPath string) error {
	p, err := s.ProjectByID(id)
	if err != nil {
		return err
	}
	var childCount int
	if err := s.db.QueryRow(`SELECT count(*) FROM projects WHERE parent_id = ?`, id).Scan(&childCount); err != nil {
		return err
	}
	if childCount > 0 {
		return fmt.Errorf("%q has sub-projects and cannot itself become a sub-project", p.Path())
	}

	parentPath = strings.TrimSpace(parentPath)
	if parentPath == "" {
		if p.ParentID == 0 {
			return nil // already top-level
		}
		_, err := s.db.Exec(`UPDATE projects SET parent_id = NULL WHERE id = ?`, id)
		if err != nil && strings.Contains(err.Error(), "ux_projects_parent_name") {
			return fmt.Errorf("a top-level project named %q already exists", p.Name)
		}
		return err
	}

	parent, err := s.ProjectByName(parentPath)
	if err != nil {
		return err
	}
	if parent.ID == id {
		return fmt.Errorf("a project cannot be its own parent")
	}
	if parent.ParentID != 0 {
		return fmt.Errorf("%q is a sub-project and cannot have children", parentPath)
	}
	_, err = s.db.Exec(`UPDATE projects SET parent_id = ? WHERE id = ?`, parent.ID, id)
	if err != nil && strings.Contains(err.Error(), "ux_projects_parent_name") {
		return fmt.Errorf("a project named %q already exists under %q", p.Name, parent.Name)
	}
	return err
}
