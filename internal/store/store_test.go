package store

import (
	"path/filepath"
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
