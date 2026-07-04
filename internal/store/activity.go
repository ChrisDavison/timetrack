package store

import (
	"fmt"
	"time"
)

// maxActivityDays caps how many days a single activity can span, so a
// mistyped end date (or year) can't generate an unbounded number of entries.
const maxActivityDays = 366

// AddActivity creates one entry per day from n.Date through end (inclusive),
// optionally skipping weekends, sharing n's project/subject/notes/duration/
// kind/tags. Entries created this way are linked by an activity_id so they
// can later be managed as a group. A range that resolves to a single day
// (end == n.Date, or every other day falls on a skipped weekend) is created
// as a plain ungrouped entry via AddEntry.
func (s *Store) AddActivity(n NewEntry, end string, weekdaysOnly bool) ([]Entry, error) {
	if err := n.validate(); err != nil {
		return nil, err
	}
	startDate, err := time.Parse("2006-01-02", n.Date)
	if err != nil {
		return nil, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", n.Date)
	}
	endDate, err := time.Parse("2006-01-02", end)
	if err != nil {
		return nil, fmt.Errorf("invalid end date %q (want YYYY-MM-DD)", end)
	}
	if endDate.Before(startDate) {
		return nil, fmt.Errorf("end date %q is before start date %q", end, n.Date)
	}
	totalDays := int(endDate.Sub(startDate).Hours()/24) + 1
	if totalDays > maxActivityDays {
		return nil, fmt.Errorf("activity spans %d days, more than the %d-day limit; narrow the range", totalDays, maxActivityDays)
	}

	var dates []string
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		if weekdaysOnly && (d.Weekday() == time.Saturday || d.Weekday() == time.Sunday) {
			continue
		}
		dates = append(dates, d.Format("2006-01-02"))
	}
	if len(dates) == 0 {
		return nil, fmt.Errorf("no days in range %s to %s (weekdays-only excludes the whole range)", n.Date, end)
	}
	if len(dates) == 1 {
		e, err := s.AddEntry(n)
		if err != nil {
			return nil, err
		}
		return []Entry{e}, nil
	}

	p, err := s.ProjectByName(n.Project)
	if err != nil {
		return nil, err
	}
	start, err := SnapStart(n.Start)
	if err != nil {
		return nil, err
	}
	blocks := blocksFromMinutes(n.Minutes)
	tags := ParseTags(n.Tags)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO activities DEFAULT VALUES`)
	if err != nil {
		return nil, err
	}
	activityID, _ := res.LastInsertId()

	var ids []int64
	for _, date := range dates {
		res, err := tx.Exec(
			`INSERT INTO entries (project_id, subject, notes, date, start_time, duration_blocks, kind, activity_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, n.Subject, n.Notes, date, start, blocks, n.Kind, activityID)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		if err := s.setTags(tx, id, tags); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(ids))
	for _, id := range ids {
		e, err := s.Entry(id)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// ActivityEntries returns an activity's member entries, ordered by date.
func (s *Store) ActivityEntries(id int64) ([]Entry, error) {
	rows, err := s.db.Query(entrySelect+` WHERE e.activity_id = ? ORDER BY e.date, e.start_time, e.id`, id)
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
	if len(entries) == 0 {
		return nil, fmt.Errorf("no activity with id %d", id)
	}
	for i, e := range entries {
		if entries[i], err = s.loadTags(e); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// UpdateActivity applies shared attributes (project, subject, notes,
// duration, kind, tags) to every member of an activity. Each member keeps
// its own date and start time.
func (s *Store) UpdateActivity(id int64, n NewEntry) error {
	if err := n.validateShared(); err != nil {
		return err
	}
	members, err := s.ActivityEntries(id)
	if err != nil {
		return err
	}
	p, err := s.ProjectByName(n.Project)
	if err != nil {
		return err
	}
	blocks := blocksFromMinutes(n.Minutes)
	tags := ParseTags(n.Tags)

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, m := range members {
		if _, err := tx.Exec(
			`UPDATE entries SET project_id = ?, subject = ?, notes = ?, duration_blocks = ?, kind = ? WHERE id = ?`,
			p.ID, n.Subject, n.Notes, blocks, n.Kind, m.ID); err != nil {
			return err
		}
		if err := s.setTags(tx, m.ID, tags); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ConfirmActivity flips every planned member of an activity to logged.
func (s *Store) ConfirmActivity(id int64) error {
	if _, err := s.ActivityEntries(id); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE entries SET kind = ? WHERE activity_id = ? AND kind = ?`, KindLogged, id, KindPlanned)
	return err
}

// DeleteActivity removes every member entry and the activity itself.
func (s *Store) DeleteActivity(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM entries WHERE activity_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM activities WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
