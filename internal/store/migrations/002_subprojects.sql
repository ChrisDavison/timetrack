-- Add one level of project nesting: projects may have a parent_id.
-- Uniqueness changes from global to per-parent, which requires a rebuild.
CREATE TABLE projects_new (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL COLLATE NOCASE,
    color      TEXT NOT NULL DEFAULT '',
    archived   INTEGER NOT NULL DEFAULT 0,
    parent_id  INTEGER REFERENCES projects_new(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO projects_new (id, name, color, archived, created_at)
SELECT id, name, color, archived, created_at FROM projects;

DROP TABLE projects;
ALTER TABLE projects_new RENAME TO projects;

CREATE UNIQUE INDEX ux_projects_parent_name
    ON projects(COALESCE(parent_id, 0), name COLLATE NOCASE);
