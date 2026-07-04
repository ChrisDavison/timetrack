-- Multi-day activities: entries created together for a date range share an
-- activity_id, so the group can be managed (edited/confirmed/deleted) as a
-- unit. No metadata lives on activities itself; display attributes are
-- derived from member entries to avoid drift.
CREATE TABLE activities (
    id         INTEGER PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

ALTER TABLE entries ADD COLUMN activity_id INTEGER REFERENCES activities(id) ON DELETE SET NULL;

CREATE INDEX idx_entries_activity ON entries(activity_id);
