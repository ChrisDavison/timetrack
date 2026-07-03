package store

import (
	"database/sql"
	"fmt"
)

// SnapshotVersion is the current export format version.
const SnapshotVersion = 1

// Snapshot is the machine-portable form of a database: what tt export
// writes, tt import reads, and tt merge exchanges between sqlite files.
type Snapshot struct {
	Version    int           `json:"version"`
	ExportedAt string        `json:"exported_at"`
	Projects   []SyncProject `json:"projects"`
	Entries    []SyncEntry   `json:"entries"`
}

type SyncProject struct {
	Path     string `json:"path"` // "Parent/Child" or bare top-level name
	Color    string `json:"color,omitempty"`
	Archived bool   `json:"archived,omitempty"`
}

type SyncEntry struct {
	UUID      string   `json:"uuid"`
	Project   string   `json:"project"` // full path
	Subject   string   `json:"subject"`
	Notes     string   `json:"notes,omitempty"`
	Date      string   `json:"date"`
	Start     string   `json:"start"`
	Blocks    int      `json:"blocks"`
	Kind      Kind     `json:"kind"`
	Tags      []string `json:"tags,omitempty"`
	UpdatedAt string   `json:"updated_at"`
	Deleted   bool     `json:"deleted,omitempty"` // tombstone
}

type MergeStats struct {
	Added     int // entries this store had never seen
	Updated   int // local entries replaced by a newer remote version
	Deleted   int // local entries tombstoned by a newer remote deletion
	Unchanged int // already up to date (or local version is newer)
}

func (m MergeStats) String() string {
	return fmt.Sprintf("%d added, %d updated, %d deleted, %d unchanged",
		m.Added, m.Updated, m.Deleted, m.Unchanged)
}

// ExportSnapshot captures every project and entry, including deletion
// tombstones (so merges propagate deletes) but excluding a running timer.
func (s *Store) ExportSnapshot() (Snapshot, error) {
	snap := Snapshot{Version: SnapshotVersion, ExportedAt: nowStamp()}

	projects, err := s.Projects(true)
	if err != nil {
		return Snapshot{}, err
	}
	for _, p := range projects {
		snap.Projects = append(snap.Projects, SyncProject{Path: p.Path(), Color: p.Color, Archived: p.Archived})
	}

	rows, err := s.db.Query(`
		SELECT e.id, e.uuid, p.name, COALESCE(pp.name, ''), e.subject, e.notes,
		       e.date, e.start_time, COALESCE(e.duration_blocks, 0), e.kind, e.updated_at, e.deleted
		FROM entries e
		JOIN projects p ON p.id = e.project_id
		LEFT JOIN projects pp ON pp.id = p.parent_id
		WHERE e.kind != 'running'
		ORDER BY e.date, e.start_time, e.id`)
	if err != nil {
		return Snapshot{}, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var se SyncEntry
		var id int64
		var name, parent string
		if err := rows.Scan(&id, &se.UUID, &name, &parent, &se.Subject, &se.Notes,
			&se.Date, &se.Start, &se.Blocks, &se.Kind, &se.UpdatedAt, &se.Deleted); err != nil {
			return Snapshot{}, err
		}
		se.Project = name
		if parent != "" {
			se.Project = parent + "/" + name
		}
		snap.Entries = append(snap.Entries, se)
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, err
	}
	for i, id := range ids {
		e, err := s.loadTags(Entry{ID: id})
		if err != nil {
			return Snapshot{}, err
		}
		snap.Entries[i].Tags = e.Tags
	}
	return snap, nil
}

// ensureProjectPath returns the local project for a path, creating it (and
// a missing parent) if needed. Color/archived apply on creation only; local
// project metadata is never overwritten by a merge.
func (s *Store) ensureProjectPath(path, color string, archived bool) (Project, error) {
	if p, err := s.ProjectByName(path); err == nil {
		return p, nil
	}
	if parent, _, err := splitPath(path); err != nil {
		return Project{}, err
	} else if parent != "" {
		if _, err := s.ProjectByName(parent); err != nil {
			if _, err := s.CreateProject(parent, ""); err != nil {
				return Project{}, err
			}
		}
	}
	p, err := s.CreateProject(path, color)
	if err != nil {
		return Project{}, err
	}
	if archived {
		p.Archived = true
		if err := s.UpdateProject(p); err != nil {
			return Project{}, err
		}
	}
	return p, nil
}

// MergeSnapshot merges a snapshot from another machine: entries are matched
// by UUID, newer updated_at wins, and tombstones delete. Merging the same
// snapshot twice is a no-op.
func (s *Store) MergeSnapshot(snap Snapshot) (MergeStats, error) {
	var st MergeStats
	if snap.Version > SnapshotVersion {
		return st, fmt.Errorf("snapshot version %d is newer than this binary supports (%d)", snap.Version, SnapshotVersion)
	}
	for _, sp := range snap.Projects {
		if _, err := s.ensureProjectPath(sp.Path, sp.Color, sp.Archived); err != nil {
			return st, err
		}
	}
	for _, se := range snap.Entries {
		if se.Kind == KindRunning {
			continue
		}
		var localID int64
		var localUpdated string
		var localDeleted bool
		err := s.db.QueryRow(`SELECT id, updated_at, deleted FROM entries WHERE uuid = ?`, se.UUID).
			Scan(&localID, &localUpdated, &localDeleted)
		switch {
		case err == sql.ErrNoRows:
			if se.Deleted {
				continue // never seen here; nothing to delete
			}
			p, err := s.ensureProjectPath(se.Project, "", false)
			if err != nil {
				return st, err
			}
			res, err := s.db.Exec(
				`INSERT INTO entries (project_id, subject, notes, date, start_time, duration_blocks, kind, uuid, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				p.ID, se.Subject, se.Notes, se.Date, se.Start, max(se.Blocks, 1), se.Kind, se.UUID, se.UpdatedAt)
			if err != nil {
				return st, err
			}
			id, _ := res.LastInsertId()
			if err := s.setTags(s.db, id, se.Tags); err != nil {
				return st, err
			}
			st.Added++
		case err != nil:
			return st, err
		default:
			if se.UpdatedAt <= localUpdated {
				st.Unchanged++
				continue
			}
			p, err := s.ensureProjectPath(se.Project, "", false)
			if err != nil {
				return st, err
			}
			_, err = s.db.Exec(
				`UPDATE entries SET project_id = ?, subject = ?, notes = ?, date = ?, start_time = ?,
				        duration_blocks = ?, kind = ?, updated_at = ?, deleted = ? WHERE id = ?`,
				p.ID, se.Subject, se.Notes, se.Date, se.Start, max(se.Blocks, 1), se.Kind,
				se.UpdatedAt, se.Deleted, localID)
			if err != nil {
				return st, err
			}
			if err := s.setTags(s.db, localID, se.Tags); err != nil {
				return st, err
			}
			if se.Deleted && !localDeleted {
				st.Deleted++
			} else {
				st.Updated++
			}
		}
	}
	return st, nil
}
