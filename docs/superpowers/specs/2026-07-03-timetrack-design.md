# timetrack — personal time tracking (web + CLI)

## Context

Single-user personal time tracker to record how time is spent across work/personal projects, plan future allocations, and report for budgeting. No auth. Data in sqlite. Lives in the already-initialized empty repo `/home/davison/code/timetrack`.

Decisions agreed with user:
- **Language: Go** — one static binary `tt` serving both the web UI and CLI subcommands.
- **Frontend: server-rendered HTML + htmx** — no node/build step; templates and static assets embedded via `go:embed`.
- **Entry modes: manual entry + live timer** (`tt start` / `tt stop`, plus web timer widget).
- **Planned vs logged entries** — future allocations are first-class entries shown distinctly; dashboard compares commitment vs capacity.

## Database schema

Driver: `modernc.org/sqlite` (pure Go, no cgo). WAL mode, foreign keys on. Migrations as embedded numbered SQL files applied at startup.

```sql
CREATE TABLE projects (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    color      TEXT,                          -- hex, for calendar display
    archived   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE entries (
    id              INTEGER PRIMARY KEY,
    project_id      INTEGER NOT NULL REFERENCES projects(id),
    subject         TEXT NOT NULL,
    notes           TEXT,
    date            TEXT NOT NULL,            -- 'YYYY-MM-DD'
    start_time      TEXT NOT NULL,            -- 'HH:MM', snapped to :00 / :30
    duration_blocks INTEGER,                  -- count of 30-min blocks; NULL only while running
    kind            TEXT NOT NULL DEFAULT 'logged'
                    CHECK (kind IN ('planned','logged','running')),
    started_at      TEXT,                     -- RFC3339; set while kind='running'
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_entries_date ON entries(date);
CREATE INDEX idx_entries_project ON entries(project_id);
-- at most one running timer:
CREATE UNIQUE INDEX idx_one_running ON entries(kind) WHERE kind = 'running';

CREATE TABLE tags (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE                 -- lowercase, stored without '#'
);

CREATE TABLE entry_tags (
    entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    tag_id   INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (entry_id, tag_id)
);
```

Notes:
- 30-min granularity is enforced structurally: durations are stored as block counts, start times snapped to :00/:30 at the store layer.
- Timer flow: `start` inserts kind='running' with `started_at` (rejected if one exists); `stop` computes elapsed, rounds **up** to nearest block (min 1), fills `duration_blocks`, snaps `start_time`, flips kind to 'logged'.
- Planned → logged: a "confirm" action flips kind (optionally adjusting duration).
- Tags typed as `#research #implementation` in one field; parsed, normalized to lowercase, upserted into `tags`.

## Architecture

```
timetrack/
├── main.go                  # CLI dispatch (stdlib flag; subcommand per file)
├── internal/store/          # sqlite open/migrate, all queries, snapping/rounding logic
│   └── migrations/*.sql     # embedded
├── internal/report/         # aggregation: sums by project/tag/day over a range
├── internal/web/            # http handlers, routes (stdlib net/http 1.22 mux)
│   ├── templates/*.html     # embedded html/template
│   └── static/              # simple.css-style stylesheet, htmx.min.js (vendored)
```

Single binary, subcommands:
- `tt serve [--addr :8090]` — web UI
- `tt add -p PROJECT -s "subject" -d 1.5h [--date 2026-07-03|today|yesterday] [--at 09:30] [-n notes] [-t research,impl] [--plan]`
- `tt start -p PROJECT -s "subject"` / `tt stop` / `tt status`
- `tt list [--project P] [--tag T] [--from D] [--to D] [--kind planned|logged]` — filters combine (AND)
- `tt report [--week|--month|--from/--to] [--by project|tag|day] [--csv]`
- `tt projects [add|archive|list]`

DB path: `~/.local/share/timetrack/timetrack.db` (XDG), overridable via `--db` / `TIMETRACK_DB`.

## Web UI (pages)

- **Dashboard `/`** — today and this-week hours (logged and planned separately), remaining planned commitments, over-commitment warning when planned+logged for a day exceeds a configurable capacity (default 8h/day), running-timer widget with stop button, per-project week breakdown bars.
- **Entries `/entries`** — table with combinable filters (project, tag, date range, kind, text search in subject/notes) as query params; htmx swaps the table on filter change; inline edit/delete.
- **Calendar `/calendar?week=2026-06-29`** — week grid, 30-min rows, entries as colored blocks (project color; planned entries hatched/outlined), click empty slot to pre-fill the new-entry form. Month view: per-day total hours heat-style summary.
- **Entry form** — new/edit, used from calendar and entries pages.
- **Projects `/projects`** — add, rename, set color, archive.

## Features beyond the original spec (answering "what have I not considered")

Included: live timer, planned-vs-actual, CSV export (budgeting), project archiving, project colors, daily capacity setting, single-file DB = trivial backup.
Deliberately deferred (YAGNI): ICS export, copy-last-week, recurring entries, idle detection, multi-device sync.

## Implementation order

Each step test-first (`go test ./...`); store and report logic are the test-heavy parts.

1. Scaffold: `go mod init`, `.gitignore`, commit design doc to `docs/superpowers/specs/2026-07-03-timetrack-design.md`.
2. `internal/store`: open/migrate, project CRUD, entry CRUD with snapping/rounding + tag parsing, timer start/stop; table-driven tests against temp DBs.
3. `internal/report`: range aggregation by project/tag/day; tests.
4. CLI subcommands (thin wrappers over store/report).
5. `internal/web`: serve, dashboard, entries list + filters.
6. Calendar week/month views.
7. Entry form + htmx inline interactions, timer widget.
8. CSV export, polish, README.

## Verification

- `go test ./...` green at every step.
- CLI smoke: `tt projects add`, `tt add`, `tt start/stop`, `tt list`, `tt report --csv` against a scratch DB.
- End-to-end: `tt serve`, drive dashboard/entries/calendar in the browser (claude-in-chrome) — create, filter, edit an entry; confirm calendar block placement and dashboard totals.
