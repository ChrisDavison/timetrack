-- Sync support: stable entry identity, edit timestamps, soft-delete
-- tombstones so merges between machines can propagate deletions.
ALTER TABLE entries ADD COLUMN uuid TEXT;
ALTER TABLE entries ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
ALTER TABLE entries ADD COLUMN deleted INTEGER NOT NULL DEFAULT 0;

UPDATE entries SET uuid = lower(hex(randomblob(16))) WHERE uuid IS NULL;
UPDATE entries SET updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', created_at) WHERE updated_at = '';

CREATE UNIQUE INDEX ux_entries_uuid ON entries(uuid);
