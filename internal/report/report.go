// Package report aggregates time entries for summaries and export.
package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/davison/timetrack/internal/store"
)

type GroupBy string

const (
	ByProject GroupBy = "project"
	ByTag     GroupBy = "tag"
	ByDay     GroupBy = "day"
)

// Untagged is the tag-report bucket for entries with no tags.
const Untagged = "(untagged)"

// Direct labels a parent project's own time when it also has sub-project lines.
const Direct = "(direct)"

type Line struct {
	Key          string // display label; sub-project lines show the bare child name
	Path         string // full project path for CSV; "" on rollup display lines
	Sub          bool   // indented breakdown line under the preceding rollup line
	Group        string // top-level project name; set on a rollup header and its sub lines
	Rollup       bool   // this is a parent line with sub lines (the foldable header)
	LoggedHours  float64
	PlannedHours float64
}

type Report struct {
	By           GroupBy
	Lines        []Line
	TotalLogged  float64
	TotalPlanned float64
}

func addHours(l *Line, e store.Entry) {
	if e.Kind == store.KindPlanned {
		l.PlannedHours += e.Hours()
	} else {
		l.LoggedHours += e.Hours()
	}
}

// Build aggregates the entries matched by f, grouped by the given dimension.
// Running entries are excluded. A multi-tagged entry counts once per tag in
// tag reports, so tag lines can sum to more than the total.
//
// Project reports roll up: each top-level project line includes its
// sub-projects' hours, followed by indented Sub lines per sub-project (and a
// "(direct)" line for the parent's own time when both exist).
func Build(s *store.Store, f store.Filter, by GroupBy) (Report, error) {
	if by != ByProject && by != ByTag && by != ByDay {
		return Report{}, fmt.Errorf("unknown grouping %q", by)
	}
	entries, err := s.Entries(f)
	if err != nil {
		return Report{}, err
	}
	r := Report{By: by}
	live := entries[:0:0]
	for _, e := range entries {
		if e.Kind == store.KindRunning {
			continue
		}
		if e.Kind == store.KindPlanned {
			r.TotalPlanned += e.Hours()
		} else {
			r.TotalLogged += e.Hours()
		}
		live = append(live, e)
	}
	if by == ByProject {
		r.Lines = projectLines(live)
	} else {
		r.Lines = flatLines(live, by)
	}
	return r, nil
}

func flatLines(entries []store.Entry, by GroupBy) []Line {
	buckets := map[string]*Line{}
	bucket := func(key string) *Line {
		if buckets[key] == nil {
			buckets[key] = &Line{Key: key, Path: key}
		}
		return buckets[key]
	}
	for _, e := range entries {
		switch by {
		case ByDay:
			addHours(bucket(e.Date), e)
		case ByTag:
			if len(e.Tags) == 0 {
				addHours(bucket(Untagged), e)
			}
			for _, tag := range e.Tags {
				addHours(bucket(tag), e)
			}
		}
	}
	lines := make([]Line, 0, len(buckets))
	for _, l := range buckets {
		lines = append(lines, *l)
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i].Key < lines[j].Key })
	return lines
}

func projectLines(entries []store.Entry) []Line {
	tops := map[string]*Line{}    // rollup totals per top-level project
	directs := map[string]*Line{} // parents' own time
	subs := map[string]map[string]*Line{}
	for _, e := range entries {
		top := e.ProjectParent
		if top == "" {
			top = e.ProjectName
		}
		if tops[top] == nil {
			tops[top] = &Line{Key: top}
		}
		addHours(tops[top], e)
		if e.ProjectParent == "" {
			if directs[top] == nil {
				directs[top] = &Line{Key: Direct, Path: top, Sub: true}
			}
			addHours(directs[top], e)
		} else {
			if subs[top] == nil {
				subs[top] = map[string]*Line{}
			}
			if subs[top][e.ProjectName] == nil {
				subs[top][e.ProjectName] = &Line{Key: e.ProjectName, Path: e.ProjectPath(), Sub: true}
			}
			addHours(subs[top][e.ProjectName], e)
		}
	}

	topKeys := make([]string, 0, len(tops))
	for k := range tops {
		topKeys = append(topKeys, k)
	}
	sort.Strings(topKeys)

	var lines []Line
	for _, top := range topKeys {
		topLine := *tops[top]
		children := subs[top]
		if len(children) == 0 {
			// No breakdown: the top line is the flat line.
			topLine.Path = top
			lines = append(lines, topLine)
			continue
		}
		topLine.Rollup = true
		topLine.Group = top
		lines = append(lines, topLine) // rollup display line, Path ""
		if d := directs[top]; d != nil {
			d.Group = top
			lines = append(lines, *d)
		}
		childKeys := make([]string, 0, len(children))
		for k := range children {
			childKeys = append(childKeys, k)
		}
		sort.Strings(childKeys)
		for _, k := range childKeys {
			child := *children[k]
			child.Group = top
			lines = append(lines, child)
		}
	}
	return lines
}

// CSV renders the report with a header row and one row per line. Project
// CSVs are flat — full-path keys, no rollup rows — so summing a column
// never double-counts.
func (r Report) CSV() string {
	var b strings.Builder
	b.WriteString("key,logged_hours,planned_hours\n")
	for _, l := range r.Lines {
		if l.Path == "" {
			continue
		}
		fmt.Fprintf(&b, "%s,%.1f,%.1f\n", l.Path, l.LoggedHours, l.PlannedHours)
	}
	return b.String()
}
