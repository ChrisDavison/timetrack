package store

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndListProjects(t *testing.T) {
	s := testStore(t)

	p, err := s.CreateProject("EngD", "#4a90d9")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID == 0 || p.Name != "EngD" || p.Color != "#4a90d9" || p.Archived {
		t.Errorf("unexpected project: %+v", p)
	}

	if _, err := s.CreateProject("Personal", ""); err != nil {
		t.Fatalf("CreateProject second: %v", err)
	}

	ps, err := s.Projects(false)
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(ps) != 2 {
		t.Fatalf("want 2 projects, got %d", len(ps))
	}
}

func TestCreateProjectDuplicateName(t *testing.T) {
	s := testStore(t)
	if _, err := s.CreateProject("EngD", ""); err != nil {
		t.Fatal(err)
	}
	// Case-insensitive uniqueness.
	if _, err := s.CreateProject("engd", ""); err == nil {
		t.Error("want error creating duplicate project, got nil")
	}
}

func TestProjectByName(t *testing.T) {
	s := testStore(t)
	created, _ := s.CreateProject("EngD", "")

	got, err := s.ProjectByName("engd")
	if err != nil {
		t.Fatalf("ProjectByName: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("want ID %d, got %d", created.ID, got.ID)
	}

	if _, err := s.ProjectByName("nope"); err == nil {
		t.Error("want error for unknown project, got nil")
	} else if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should name the missing project, got: %v", err)
	}
}

func TestArchiveProjectHiddenFromDefaultList(t *testing.T) {
	s := testStore(t)
	p, _ := s.CreateProject("Old", "")
	p.Archived = true
	if err := s.UpdateProject(p); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}

	active, _ := s.Projects(false)
	if len(active) != 0 {
		t.Errorf("archived project should be hidden, got %d", len(active))
	}
	all, _ := s.Projects(true)
	if len(all) != 1 {
		t.Errorf("want 1 project including archived, got %d", len(all))
	}
}

func TestCreateSubProject(t *testing.T) {
	s := testStore(t)
	parent, _ := s.CreateProject("DDC", "#2a78d6")

	sub, err := s.CreateProject("DDC/Computer Vision", "")
	if err != nil {
		t.Fatalf("create sub-project: %v", err)
	}
	if sub.ParentID != parent.ID || sub.Name != "Computer Vision" {
		t.Errorf("sub = %+v", sub)
	}
	if sub.Color != "#2a78d6" {
		t.Errorf("sub should inherit parent color, got %q", sub.Color)
	}
	if sub.Path() != "DDC/Computer Vision" {
		t.Errorf("Path() = %q", sub.Path())
	}
	if parent.Path() != "DDC" {
		t.Errorf("parent Path() = %q", parent.Path())
	}
}

func TestCreateSubProjectRules(t *testing.T) {
	s := testStore(t)
	s.CreateProject("DDC", "")
	s.CreateProject("Other", "")
	s.CreateProject("DDC/CV", "")

	if _, err := s.CreateProject("Nope/CV", ""); err == nil {
		t.Error("unknown parent should be rejected")
	}
	if _, err := s.CreateProject("DDC/CV/Deep", ""); err == nil {
		t.Error("grandchild should be rejected")
	}
	if _, err := s.CreateProject("DDC/cv", ""); err == nil {
		t.Error("duplicate child under same parent should be rejected")
	}
	if _, err := s.CreateProject("Other/CV", ""); err != nil {
		t.Errorf("same child name under different parent should be allowed: %v", err)
	}
	if _, err := s.CreateProject("DDC/", ""); err == nil {
		t.Error("empty child segment should be rejected")
	}
}

func TestProjectByPath(t *testing.T) {
	s := testStore(t)
	s.CreateProject("DDC", "")
	sub, _ := s.CreateProject("DDC/CV", "")

	got, err := s.ProjectByName("ddc/cv")
	if err != nil || got.ID != sub.ID {
		t.Errorf("ProjectByName path = %+v, %v", got, err)
	}
	// Bare name resolves top-level only.
	if _, err := s.ProjectByName("CV"); err == nil {
		t.Error("bare sub-project name should not resolve")
	}
}

func TestProjectsTreeOrder(t *testing.T) {
	s := testStore(t)
	s.CreateProject("Personal", "")
	s.CreateProject("DDC", "")
	s.CreateProject("DDC/CV", "")
	s.CreateProject("DDC/Appleby", "")

	ps, err := s.Projects(false)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, p := range ps {
		paths = append(paths, p.Path())
	}
	want := []string{"DDC", "DDC/Appleby", "DDC/CV", "Personal"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("tree order = %v, want %v", paths, want)
	}
	if ps[1].ParentName != "DDC" {
		t.Errorf("ParentName not filled: %+v", ps[1])
	}
}

// TestMigrationFromV1 builds a database as it existed before sub-projects
// (migration 001 only, with data) and verifies opening it migrates cleanly.
func TestMigrationFromV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	initSQL, err := fs.ReadFile(migrationFS, "migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY)`,
		string(initSQL),
		`INSERT INTO schema_migrations (name) VALUES ('migrations/001_init.sql')`,
		`INSERT INTO projects (id, name, color) VALUES (1, 'EngD', '#4a90d9')`,
		`INSERT INTO entries (project_id, subject, date, start_time, duration_blocks, kind)
		 VALUES (1, 'old work', '2026-07-01', '09:00', 3, 'logged')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("building v1 db: %v", err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("migrating v1 db: %v", err)
	}
	defer s.Close()
	entries, err := s.Entries(Filter{Project: "EngD"})
	if err != nil || len(entries) != 1 || entries[0].Subject != "old work" {
		t.Errorf("v1 data lost after migration: %v, %v", entries, err)
	}
	if _, err := s.CreateProject("EngD/Subtopic", ""); err != nil {
		t.Errorf("sub-project creation on migrated db: %v", err)
	}
}

func TestArchiveParentCascades(t *testing.T) {
	s := testStore(t)
	p, _ := s.CreateProject("DDC", "")
	s.CreateProject("DDC/CV", "")

	p.Archived = true
	if err := s.UpdateProject(p); err != nil {
		t.Fatal(err)
	}
	if active, _ := s.Projects(false); len(active) != 0 {
		t.Errorf("children should be archived with parent, got %v", active)
	}
}
