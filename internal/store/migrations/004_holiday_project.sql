-- "Holiday" is a permanent system project: it always exists and can't be
-- archived or deleted (enforced in Go, not here). Its hours are excluded
-- from committed-work totals and instead subtracted from capacity.
ALTER TABLE projects ADD COLUMN system INTEGER NOT NULL DEFAULT 0;

INSERT INTO projects (name, color, system)
SELECT 'Holiday', '#e0a339', 1
WHERE NOT EXISTS (
    SELECT 1 FROM projects WHERE name = 'Holiday' COLLATE NOCASE AND parent_id IS NULL
);

UPDATE projects SET system = 1, archived = 0
WHERE name = 'Holiday' COLLATE NOCASE AND parent_id IS NULL;
