package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// newUUID returns 32 hex characters of randomness: the stable identity an
// entry keeps across machines when databases are merged.
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return hex.EncodeToString(b)
}

// nowStamp is the updated_at format; UTC and sub-second so last-write-wins
// merge comparisons are fine-grained.
func nowStamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

type Kind string

const (
	KindPlanned Kind = "planned"
	KindLogged  Kind = "logged"
	KindRunning Kind = "running"
)

// BlockMinutes is the tracking granularity.
const BlockMinutes = 30

type Entry struct {
	ID            int64
	ProjectID     int64
	ProjectName   string
	ProjectParent string // "" when the project is top-level
	ProjectColor  string
	Subject       string
	Notes         string
	Date          string // YYYY-MM-DD
	Start         string // HH:MM, snapped to :00/:30
	Blocks        int    // 30-min blocks; 0 while running
	Kind          Kind
	StartedAt     time.Time // set while running
	Tags          []string
	ActivityID    int64  // 0 when not part of a multi-day activity group
	UUID          string // stable identity across machines
	UpdatedAt     string // RFC3339Nano UTC, for last-write-wins merging
}

// Hours returns the entry duration in hours.
func (e Entry) Hours() float64 { return float64(e.Blocks) * BlockMinutes / 60 }

// ProjectPath is the entry's full project reference: "Parent/Child" or a
// bare top-level name.
func (e Entry) ProjectPath() string {
	if e.ProjectParent != "" {
		return e.ProjectParent + "/" + e.ProjectName
	}
	return e.ProjectName
}

// NewEntry holds user input for creating or updating an entry.
type NewEntry struct {
	Project string // project name, must exist
	Subject string
	Notes   string
	Date    string // YYYY-MM-DD
	Start   string // HH:MM, snapped on save
	Minutes int    // duration, rounded up to whole blocks
	Kind    Kind   // planned or logged
	Tags    string // free text, e.g. "#research #writing" or "research, writing"
}

// ParseTags extracts normalized tag names from free text: '#' stripped,
// lowercased, deduplicated, sorted. Returns nil when no tags present.
func ParseTags(s string) []string {
	seen := map[string]bool{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' }) {
		tag := strings.ToLower(strings.TrimPrefix(f, "#"))
		if tag != "" {
			seen[tag] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	tags := make([]string, 0, len(seen))
	for t := range seen {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// SnapStart validates an HH:MM time and snaps it to the nearest half hour,
// clamped so the result never rolls past the end of the day.
func SnapStart(s string) (string, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return "", fmt.Errorf("invalid start time %q (want HH:MM)", s)
	}
	h, m := t.Hour(), t.Minute()
	switch {
	case m < 15:
		m = 0
	case m < 45:
		m = 30
	default:
		h, m = h+1, 0
	}
	if h > 23 {
		h, m = 23, 30
	}
	return fmt.Sprintf("%02d:%02d", h, m), nil
}

func blocksFromMinutes(minutes int) int {
	return (minutes + BlockMinutes - 1) / BlockMinutes
}

func (n NewEntry) validate() error {
	if err := n.validateShared(); err != nil {
		return err
	}
	if _, err := time.Parse("2006-01-02", n.Date); err != nil {
		return fmt.Errorf("invalid date %q (want YYYY-MM-DD)", n.Date)
	}
	return nil
}

// validateShared checks the fields an activity group shares across all its
// member entries (everything except date/start, which stay per-day).
func (n NewEntry) validateShared() error {
	if strings.TrimSpace(n.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if n.Minutes <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if n.Kind != KindPlanned && n.Kind != KindLogged {
		return fmt.Errorf("kind must be %q or %q", KindPlanned, KindLogged)
	}
	return nil
}

func (s *Store) AddEntry(n NewEntry) (Entry, error) {
	if err := n.validate(); err != nil {
		return Entry{}, err
	}
	p, err := s.ProjectByName(n.Project)
	if err != nil {
		return Entry{}, err
	}
	start, err := SnapStart(n.Start)
	if err != nil {
		return Entry{}, err
	}
	res, err := s.db.Exec(
		`INSERT INTO entries (project_id, subject, notes, date, start_time, duration_blocks, kind, uuid, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, n.Subject, n.Notes, n.Date, start, blocksFromMinutes(n.Minutes), n.Kind, newUUID(), nowStamp())
	if err != nil {
		return Entry{}, err
	}
	id, _ := res.LastInsertId()
	if err := s.setTags(s.db, id, ParseTags(n.Tags)); err != nil {
		return Entry{}, err
	}
	return s.Entry(id)
}

func (s *Store) UpdateEntry(id int64, n NewEntry) (Entry, error) {
	if err := n.validate(); err != nil {
		return Entry{}, err
	}
	p, err := s.ProjectByName(n.Project)
	if err != nil {
		return Entry{}, err
	}
	start, err := SnapStart(n.Start)
	if err != nil {
		return Entry{}, err
	}
	_, err = s.db.Exec(
		`UPDATE entries SET project_id = ?, subject = ?, notes = ?, date = ?, start_time = ?, duration_blocks = ?, kind = ?, updated_at = ?
		 WHERE id = ?`,
		p.ID, n.Subject, n.Notes, n.Date, start, blocksFromMinutes(n.Minutes), n.Kind, nowStamp(), id)
	if err != nil {
		return Entry{}, err
	}
	if err := s.setTags(s.db, id, ParseTags(n.Tags)); err != nil {
		return Entry{}, err
	}
	return s.Entry(id)
}

// DeleteEntry soft-deletes: the row stays as a tombstone so merges to other
// machines propagate the deletion instead of resurrecting the entry.
func (s *Store) DeleteEntry(id int64) error {
	_, err := s.db.Exec(`UPDATE entries SET deleted = 1, updated_at = ? WHERE id = ?`, nowStamp(), id)
	return err
}

// execer is satisfied by both *sql.DB and *sql.Tx, so tag writes can join
// whichever transaction (if any) the caller is already using. Mixing the two
// on the single-connection pool would otherwise deadlock: a Tx holds the
// pool's only connection, so a plain s.db.Exec issued before it commits
// blocks forever waiting for a connection that will never free up.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (s *Store) setTags(db execer, entryID int64, tags []string) error {
	if _, err := db.Exec(`DELETE FROM entry_tags WHERE entry_id = ?`, entryID); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := db.Exec(`INSERT OR IGNORE INTO tags (name) VALUES (?)`, tag); err != nil {
			return err
		}
		if _, err := db.Exec(
			`INSERT INTO entry_tags (entry_id, tag_id) SELECT ?, id FROM tags WHERE name = ?`,
			entryID, tag); err != nil {
			return err
		}
	}
	return nil
}

const entrySelect = `
	SELECT e.id, e.project_id, p.name, COALESCE(pp.name, ''), p.color, e.subject, e.notes,
	       e.date, e.start_time, COALESCE(e.duration_blocks, 0), e.kind, COALESCE(e.started_at, ''),
	       COALESCE(e.activity_id, 0), e.uuid, e.updated_at
	FROM entries e
	JOIN projects p ON p.id = e.project_id
	LEFT JOIN projects pp ON pp.id = p.parent_id`

func scanEntry(row interface{ Scan(...any) error }) (Entry, error) {
	var e Entry
	var startedAt string
	err := row.Scan(&e.ID, &e.ProjectID, &e.ProjectName, &e.ProjectParent, &e.ProjectColor, &e.Subject, &e.Notes,
		&e.Date, &e.Start, &e.Blocks, &e.Kind, &startedAt, &e.ActivityID, &e.UUID, &e.UpdatedAt)
	if err != nil {
		return Entry{}, err
	}
	if startedAt != "" {
		e.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	}
	return e, nil
}

func (s *Store) Entry(id int64) (Entry, error) {
	e, err := scanEntry(s.db.QueryRow(entrySelect+` WHERE e.id = ? AND e.deleted = 0`, id))
	if err == sql.ErrNoRows {
		return Entry{}, fmt.Errorf("no entry with id %d", id)
	}
	if err != nil {
		return Entry{}, err
	}
	return s.loadTags(e)
}

func (s *Store) loadTags(e Entry) (Entry, error) {
	rows, err := s.db.Query(
		`SELECT t.name FROM tags t JOIN entry_tags et ON et.tag_id = t.id WHERE et.entry_id = ? ORDER BY t.name`,
		e.ID)
	if err != nil {
		return e, err
	}
	defer rows.Close()
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return e, err
		}
		e.Tags = append(e.Tags, tag)
	}
	return e, rows.Err()
}

// Filter selects entries; zero-value fields are ignored and set fields
// combine with AND.
type Filter struct {
	Project string // project name
	Tag     string
	From    string // inclusive YYYY-MM-DD
	To      string // inclusive YYYY-MM-DD
	Kind    Kind
	Search  string // substring of subject or notes
}

func (s *Store) Entries(f Filter) ([]Entry, error) {
	q := entrySelect
	conds := []string{`e.deleted = 0`}
	var args []any
	if f.Project != "" {
		pr, err := s.ProjectByName(f.Project)
		if err != nil {
			return nil, err
		}
		// A top-level project includes its sub-projects' entries.
		conds = append(conds, `(e.project_id = ? OR p.parent_id = ?)`)
		args = append(args, pr.ID, pr.ID)
	}
	if f.Tag != "" {
		conds = append(conds, `e.id IN (SELECT et.entry_id FROM entry_tags et JOIN tags t ON t.id = et.tag_id WHERE t.name = ?)`)
		args = append(args, strings.ToLower(strings.TrimPrefix(f.Tag, "#")))
	}
	if f.From != "" {
		conds = append(conds, `e.date >= ?`)
		args = append(args, f.From)
	}
	if f.To != "" {
		conds = append(conds, `e.date <= ?`)
		args = append(args, f.To)
	}
	if f.Kind != "" {
		conds = append(conds, `e.kind = ?`)
		args = append(args, f.Kind)
	}
	if f.Search != "" {
		conds = append(conds, `(e.subject LIKE ? OR e.notes LIKE ?)`)
		like := "%" + f.Search + "%"
		args = append(args, like, like)
	}
	q += ` WHERE ` + strings.Join(conds, ` AND `)
	q += ` ORDER BY e.date, e.start_time, e.id`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, e := range entries {
		if entries[i], err = s.loadTags(e); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// StartTimer begins a running entry at now. Only one timer may run at a time.
func (s *Store) StartTimer(project, subject, notes, tags string, now time.Time) (Entry, error) {
	if strings.TrimSpace(subject) == "" {
		return Entry{}, fmt.Errorf("subject is required")
	}
	p, err := s.ProjectByName(project)
	if err != nil {
		return Entry{}, err
	}
	start, err := SnapStart(now.Format("15:04"))
	if err != nil {
		return Entry{}, err
	}
	res, err := s.db.Exec(
		`INSERT INTO entries (project_id, subject, notes, date, start_time, duration_blocks, kind, started_at, uuid, updated_at)
		 VALUES (?, ?, ?, ?, ?, NULL, ?, ?, ?, ?)`,
		p.ID, subject, notes, now.Format("2006-01-02"), start, KindRunning, now.Format(time.RFC3339), newUUID(), nowStamp())
	if err != nil {
		if strings.Contains(err.Error(), "idx_one_running") {
			return Entry{}, fmt.Errorf("a timer is already running; stop it first")
		}
		return Entry{}, err
	}
	id, _ := res.LastInsertId()
	if err := s.setTags(s.db, id, ParseTags(tags)); err != nil {
		return Entry{}, err
	}
	return s.Entry(id)
}

// StopTimer finishes the running entry, rounding the elapsed time up to
// whole blocks (minimum one).
func (s *Store) StopTimer(now time.Time) (Entry, error) {
	running, err := s.RunningEntry()
	if err != nil {
		return Entry{}, err
	}
	if running == nil {
		return Entry{}, fmt.Errorf("no timer is running")
	}
	elapsed := int(now.Sub(running.StartedAt).Minutes())
	blocks := max(blocksFromMinutes(elapsed), 1)
	_, err = s.db.Exec(
		`UPDATE entries SET duration_blocks = ?, kind = ?, started_at = NULL, updated_at = ? WHERE id = ?`,
		blocks, KindLogged, nowStamp(), running.ID)
	if err != nil {
		return Entry{}, err
	}
	return s.Entry(running.ID)
}

// RunningEntry returns the running entry, or nil when no timer is active.
func (s *Store) RunningEntry() (*Entry, error) {
	e, err := scanEntry(s.db.QueryRow(entrySelect + ` WHERE e.kind = 'running' AND e.deleted = 0`))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e, err = s.loadTags(e)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ConfirmPlanned flips a planned entry to logged.
func (s *Store) ConfirmPlanned(id int64) (Entry, error) {
	e, err := s.Entry(id)
	if err != nil {
		return Entry{}, err
	}
	if e.Kind != KindPlanned {
		return Entry{}, fmt.Errorf("entry %d is %s, not planned", id, e.Kind)
	}
	if _, err := s.db.Exec(`UPDATE entries SET kind = ?, updated_at = ? WHERE id = ?`, KindLogged, nowStamp(), id); err != nil {
		return Entry{}, err
	}
	return s.Entry(id)
}
