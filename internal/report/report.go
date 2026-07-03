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

type Line struct {
	Key          string
	LoggedHours  float64
	PlannedHours float64
}

type Report struct {
	By           GroupBy
	Lines        []Line
	TotalLogged  float64
	TotalPlanned float64
}

// Build aggregates the entries matched by f, grouped by the given dimension.
// Running entries are excluded. A multi-tagged entry counts once per tag in
// tag reports, so tag lines can sum to more than the total.
func Build(s *store.Store, f store.Filter, by GroupBy) (Report, error) {
	entries, err := s.Entries(f)
	if err != nil {
		return Report{}, err
	}
	r := Report{By: by}
	buckets := map[string]*Line{}
	bucket := func(key string) *Line {
		if buckets[key] == nil {
			buckets[key] = &Line{Key: key}
		}
		return buckets[key]
	}
	addHours := func(l *Line, e store.Entry) {
		if e.Kind == store.KindPlanned {
			l.PlannedHours += e.Hours()
		} else {
			l.LoggedHours += e.Hours()
		}
	}
	for _, e := range entries {
		if e.Kind == store.KindRunning {
			continue
		}
		if e.Kind == store.KindPlanned {
			r.TotalPlanned += e.Hours()
		} else {
			r.TotalLogged += e.Hours()
		}
		switch by {
		case ByProject:
			addHours(bucket(e.ProjectName), e)
		case ByDay:
			addHours(bucket(e.Date), e)
		case ByTag:
			if len(e.Tags) == 0 {
				addHours(bucket(Untagged), e)
			}
			for _, tag := range e.Tags {
				addHours(bucket(tag), e)
			}
		default:
			return Report{}, fmt.Errorf("unknown grouping %q", by)
		}
	}
	for _, l := range buckets {
		r.Lines = append(r.Lines, *l)
	}
	sort.Slice(r.Lines, func(i, j int) bool { return r.Lines[i].Key < r.Lines[j].Key })
	return r, nil
}

// CSV renders the report with a header row and one row per line.
func (r Report) CSV() string {
	var b strings.Builder
	b.WriteString("key,logged_hours,planned_hours\n")
	for _, l := range r.Lines {
		fmt.Fprintf(&b, "%s,%.1f,%.1f\n", l.Key, l.LoggedHours, l.PlannedHours)
	}
	return b.String()
}
