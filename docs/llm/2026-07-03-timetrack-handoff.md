# Handoff: timetrack — personal time tracker (Go web + CLI)

**Date**: 2026-07-03
**Reason**: End of build session; capturing workflow notes and operational knowledge
**Status**: Ready to Resume — all planned features shipped, tests green, work committed

## Goal
Personal time tracking across work/personal projects in 30-minute blocks, for
seeing time allocation and budgeting. One binary (`tt`) serves the web UI and
acts as a CLI. Single user, no auth, sqlite storage. Now also syncs between
two machines.

## Current state
**Working / confirmed** (all committed on master, `go test ./...` = 51 tests green):
- Full app: dashboard (capacity vs logged+planned, over-commitment warning),
  entries with combinable filters (htmx), week/month calendar, reports, CSV export.
- Live timer (`tt start/stop/status` + web widget); planned-vs-logged entries.
- Sub-projects, one level, `Parent/Child` path syntax everywhere; parent filters
  include children; reports roll up with indented breakdown; CSV stays flat.
- Two-machine sync: `tt merge other.db`, `tt export` / `tt import -` (JSON).
  UUID identity + last-write-wins + deletion tombstones; merges are idempotent.
- Design spec: `docs/superpowers/specs/2026-07-03-timetrack-design.md`. Usage: `README.md`.

**Broken / incomplete**:
- UI never visually inspected in a real browser (Chrome extension wasn't
  connected); HTML verified via curl only. Aesthetics unreviewed.
- No `tt delete <id>` CLI command — deletion is web-only (syncs fine regardless).

## Decisions
- Go + server-rendered htmx, single binary, `modernc.org/sqlite` (no cgo) — High.
- Duration stored as count of 30-min blocks; start times snapped to :00/:30 — High.
- Entry `kind`: planned / logged / running (partial unique index = one timer) — High.
- Sub-projects via `parent_id` self-reference; per-parent name uniqueness
  (required a table rebuild in migration 002) — High.
- Sync: soft deletes everywhere; reads filter `deleted = 0`; LWW on
  `updated_at` (RFC3339Nano UTC) — Medium (LWW trusts wall clocks; fine for one person).
- CSV exports never contain rollup rows (summing a column must not double-count) — High.

## Things to know (operational)
- DB: `~/.local/share/timetrack/timetrack.db`; override `TIMETRACK_DB` / `-db`.
- **Migrate before first copy to machine 2**: run the current binary once so
  entries get UUIDs; independently-migrated shared history duplicates on merge.
- Migration runner runs with `PRAGMA foreign_keys=OFF` + `foreign_key_check`
  after (needed for table rebuilds); store uses a single connection (`SetMaxOpenConns(1)`).
- `tt merge` opens (and therefore migrates) the other db file — merge from a copy.
- Project colors come from the dataviz reference palette; UI colors/labels obey
  its rules (planned = hatched, labels always text, status colors for over-commitment).
- Dev loop: `go test ./...`; smoke: `TIMETRACK_DB=/tmp/x.db ./tt ...`; web:
  `./tt serve` then curl/browser on :8090.

## Dead ends — don't retry these
None.

## Don't repeat
- User works TDD-first via superpowers skills; plan mode + AskUserQuestion
  before features. Keep granularity/YAGNI bias (rejected: arbitrary-depth
  nesting, dual-axis dashboards, web UI for sync).

## Open questions
- [ ] Is the visual design acceptable? (Needs a human look at `tt serve`.)
- [ ] Want `tt delete <id>` on the CLI?

## Next steps
1. Run `./tt serve` in `~/code/timetrack` and review the UI in a browser; note tweaks.
2. If starting on machine 2: build `tt`, run it once on machine 1 first, then
   copy the db and follow README "Two machines".
3. Optionally add `tt delete <id>` (soft delete via `store.DeleteEntry`, TDD).

## Paste this to resume
> "I'm resuming work from a previous session. Read
> `docs/llm/2026-07-03-timetrack-handoff.md` and then: [e.g. review UI feedback
> below / add tt delete]. Ask me if anything is unclear before proceeding."
