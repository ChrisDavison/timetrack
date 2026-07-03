# timetrack

Personal time tracking: a single binary (`tt`) that serves a web UI and doubles
as a CLI. Data lives in one sqlite file. Single user, no auth.

Time is tracked in 30-minute blocks against projects, with a subject, optional
notes, and `#tags`. Entries are either **logged** (work done) or **planned**
(future allocation), so the dashboard can warn when a day or week is
over-committed.

Projects can have one level of sub-projects, referenced everywhere as
`Parent/Child` (e.g. `DDC/Computer Vision`). Time can be logged against
either level. Filtering by a parent includes its sub-projects; the web
project report shows each parent's rolled-up percentage of total logged
hours, with its sub-project breakdown (including a `(direct)` line for the
parent's own time) folded away behind a disclosure toggle. **CSV exports
stay flat** — one full-path row per (sub-)project, no rollup rows — so
summing a column never double-counts. Sub-projects inherit their parent's
color unless given their own, and archiving a parent archives its children.
A project can be deleted once it has no entries or sub-projects, and can be
re-parented (assigned to, or unassigned from, a parent) at any time.

## Build

```sh
go build -o tt .
```

## Web UI

```sh
tt serve                 # http://localhost:8090
tt serve -addr :9000 -capacity 7.5
```

- **Dashboard** — today/week hours vs capacity, over-commitment warning,
  live timer, per-project bars for the week.
- **Entries** — filterable list (project, tag, date range, kind, text search;
  filters combine). Inline edit/delete, confirm planned entries as done.
- **Calendar** — week grid (click an empty slot to add an entry there) and a
  month overview shaded by hours per day. Planned entries render hatched.
- **Report** — hours by project, tag, or day for any range; CSV download.
- **Projects** — add, recolor, archive, delete, and assign/unassign a parent.

## CLI

```sh
tt projects add EngD '#2a78d6'
tt projects add 'EngD/Thesis'       # sub-project
tt projects reparent Thesis EngD    # move a top-level project under a parent
tt projects reparent Thesis         # ...or back to top level
tt projects delete Thesis           # only if it has no entries or sub-projects
tt add -p EngD -s "thesis chapter 3" -d 1.5h -at 09:30 -t '#research #writing'
tt add -p 'EngD/Thesis' -s "figures" -d 1h
tt add -p EngD -s "supervisor meeting" -d 1h -date tomorrow -plan
tt start -p EngD -s "deep work"    # live timer
tt status
tt stop                            # rounds up to the next 30-min block
tt list -project EngD -from 2026-07-01 -to 2026-07-07
tt report -week                    # or -month, -from/-to; -by project|tag|day
tt report -by tag -csv > tags.csv
```

Dates accept `YYYY-MM-DD`, `today`, `yesterday`, `tomorrow`. Durations accept
`30m`, `1.5h`, `2h30m` and are rounded up to whole blocks. Start times snap to
the nearest half hour.

## Data

The database defaults to `~/.local/share/timetrack/timetrack.db`; override with
`TIMETRACK_DB` or `-db`. Back up by copying that one file.

## Development

```sh
go test ./...
```

Schema and design notes: `docs/superpowers/specs/2026-07-03-timetrack-design.md`.
