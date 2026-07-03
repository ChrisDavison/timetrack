CREATE TABLE projects (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    color      TEXT NOT NULL DEFAULT '',
    archived   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE entries (
    id              INTEGER PRIMARY KEY,
    project_id      INTEGER NOT NULL REFERENCES projects(id),
    subject         TEXT NOT NULL,
    notes           TEXT NOT NULL DEFAULT '',
    date            TEXT NOT NULL,
    start_time      TEXT NOT NULL,
    duration_blocks INTEGER,
    kind            TEXT NOT NULL DEFAULT 'logged'
                    CHECK (kind IN ('planned','logged','running')),
    started_at      TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_entries_date ON entries(date);
CREATE INDEX idx_entries_project ON entries(project_id);
CREATE UNIQUE INDEX idx_one_running ON entries(kind) WHERE kind = 'running';

CREATE TABLE tags (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE entry_tags (
    entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (entry_id, tag_id)
);
