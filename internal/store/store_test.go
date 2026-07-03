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

func TestDeleteProject(t *testing.T) {
	s := testStore(t)
	empty, _ := s.CreateProject("Empty", "")
	if err := s.DeleteProject(empty.ID); err != nil {
		t.Fatalf("DeleteProject empty: %v", err)
	}
	if _, err := s.ProjectByName("Empty"); err == nil {
		t.Error("deleted project should no longer resolve")
	}

	withEntry, _ := s.CreateProject("HasEntry", "")
	if _, err := s.AddEntry(NewEntry{Project: "HasEntry", Subject: "w", Date: "2026-07-01", Start: "09:00", Minutes: 30, Kind: KindLogged}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(withEntry.ID); err == nil {
		t.Error("want error deleting project with entries, got nil")
	}

	parent, _ := s.CreateProject("Parent", "")
	s.CreateProject("Parent/Child", "")
	if err := s.DeleteProject(parent.ID); err == nil {
		t.Error("want error deleting project with sub-projects, got nil")
	}
}

func TestSetParent(t *testing.T) {
	s := testStore(t)
	ddc, _ := s.CreateProject("DDC", "")
	solo, _ := s.CreateProject("Solo", "")
	s.CreateProject("DDC/CV", "")

	// Assign a childless top-level project under a parent.
	if err := s.SetParent(solo.ID, "DDC"); err != nil {
		t.Fatalf("SetParent assign: %v", err)
	}
	got, err := s.ProjectByName("DDC/Solo")
	if err != nil {
		t.Fatalf("Solo should resolve under DDC: %v", err)
	}
	if got.ParentID != ddc.ID {
		t.Errorf("Solo.ParentID = %d, want %d", got.ParentID, ddc.ID)
	}

	// Unassign back to top level.
	if err := s.SetParent(got.ID, ""); err != nil {
		t.Fatalf("SetParent unassign: %v", err)
	}
	if _, err := s.ProjectByName("Solo"); err != nil {
		t.Errorf("Solo should resolve top-level again: %v", err)
	}

	// Reject assigning under a sub-project (would create 3 tiers).
	solo2, _ := s.ProjectByName("Solo")
	if err := s.SetParent(solo2.ID, "DDC/CV"); err == nil {
		t.Error("want error assigning under a sub-project, got nil")
	}

	// Reject moving a project that has its own children.
	if err := s.SetParent(ddc.ID, ""); err == nil {
		t.Error("want error re-parenting a project with sub-projects, got nil")
	}

	// Reject a name collision under the target parent.
	s.CreateProject("Other", "")
	s.CreateProject("Other/CV", "")
	otherCV, _ := s.ProjectByName("Other/CV")
	if err := s.SetParent(otherCV.ID, "DDC"); err == nil {
		t.Error("want error on name collision under target parent, got nil")
	}
}

func TestSetParentRollsUpEntries(t *testing.T) {
	s := testStore(t)
	s.CreateProject("DDC", "")
	appleby, _ := s.CreateProject("Appleby", "")
	if _, err := s.AddEntry(NewEntry{Project: "Appleby", Subject: "w", Date: "2026-07-01", Start: "09:00", Minutes: 60, Kind: KindLogged}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetParent(appleby.ID, "DDC"); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Entries(Filter{Project: "DDC"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ProjectPath() != "DDC/Appleby" {
		t.Errorf("Appleby's entries should roll up under DDC after re-parenting: %+v", entries)
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
