package report

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/davison/timetrack/internal/store"
)

// seeded returns a store containing:
//
//	EngD     2026-07-01  2.0h logged  #research
//	EngD     2026-07-02  1.0h logged  #meetings
//	Personal 2026-07-02  0.5h logged  (untagged)
//	Personal 2026-07-03  1.5h planned #research
func seeded(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, name := range []string{"EngD", "Personal"} {
		if _, err := s.CreateProject(name, ""); err != nil {
			t.Fatal(err)
		}
	}
	add := func(project, date, tags string, minutes int, kind store.Kind) {
		t.Helper()
		_, err := s.AddEntry(store.NewEntry{
			Project: project, Subject: "work", Date: date, Start: "09:00",
			Minutes: minutes, Kind: kind, Tags: tags,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	add("EngD", "2026-07-01", "#research", 120, store.KindLogged)
	add("EngD", "2026-07-02", "#meetings", 60, store.KindLogged)
	add("Personal", "2026-07-02", "", 30, store.KindLogged)
	add("Personal", "2026-07-03", "#research", 90, store.KindPlanned)
	return s
}

func lineFor(lines []Line, key string) (Line, bool) {
	for _, l := range lines {
		if l.Key == key {
			return l, true
		}
	}
	return Line{}, false
}

func TestByProject(t *testing.T) {
	s := seeded(t)
	r, err := Build(s, store.Filter{From: "2026-07-01", To: "2026-07-07"}, ByProject)
	if err != nil {
		t.Fatal(err)
	}
	engd, ok := lineFor(r.Lines, "EngD")
	if !ok || engd.LoggedHours != 3.0 || engd.PlannedHours != 0 {
		t.Errorf("EngD line = %+v, ok=%v", engd, ok)
	}
	personal, ok := lineFor(r.Lines, "Personal")
	if !ok || personal.LoggedHours != 0.5 || personal.PlannedHours != 1.5 {
		t.Errorf("Personal line = %+v, ok=%v", personal, ok)
	}
	if r.TotalLogged != 3.5 || r.TotalPlanned != 1.5 {
		t.Errorf("totals logged=%v planned=%v", r.TotalLogged, r.TotalPlanned)
	}
}

func TestByTagCountsEntryPerTagAndUntagged(t *testing.T) {
	s := seeded(t)
	r, err := Build(s, store.Filter{}, ByTag)
	if err != nil {
		t.Fatal(err)
	}
	research, _ := lineFor(r.Lines, "research")
	if research.LoggedHours != 2.0 || research.PlannedHours != 1.5 {
		t.Errorf("research line = %+v", research)
	}
	untagged, ok := lineFor(r.Lines, "(untagged)")
	if !ok || untagged.LoggedHours != 0.5 {
		t.Errorf("untagged line = %+v, ok=%v", untagged, ok)
	}
}

func TestByDay(t *testing.T) {
	s := seeded(t)
	r, err := Build(s, store.Filter{}, ByDay)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Lines) != 3 {
		t.Fatalf("want 3 day lines, got %d: %+v", len(r.Lines), r.Lines)
	}
	// Days come back in date order.
	if r.Lines[0].Key != "2026-07-01" || r.Lines[2].Key != "2026-07-03" {
		t.Errorf("day order wrong: %+v", r.Lines)
	}
	d2, _ := lineFor(r.Lines, "2026-07-02")
	if d2.LoggedHours != 1.5 {
		t.Errorf("2026-07-02 logged = %v", d2.LoggedHours)
	}
}

func TestFilterRespected(t *testing.T) {
	s := seeded(t)
	r, err := Build(s, store.Filter{Project: "EngD"}, ByDay)
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalLogged != 3.0 || r.TotalPlanned != 0 {
		t.Errorf("filtered totals = %+v", r)
	}
}

func TestCSV(t *testing.T) {
	s := seeded(t)
	r, _ := Build(s, store.Filter{}, ByProject)
	csv := r.CSV()
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if lines[0] != "key,logged_hours,planned_hours" {
		t.Errorf("csv header = %q", lines[0])
	}
	if len(lines) != 3 {
		t.Fatalf("want header+2 rows, got %d: %q", len(lines), csv)
	}
	if !strings.Contains(csv, "EngD,3.0,0.0") {
		t.Errorf("csv missing EngD row: %q", csv)
	}
}

// hierarchySeeded: DDC has 1h direct logged, DDC/CV 2h logged,
// DDC/Appleby 1.5h planned; Personal 0.5h logged.
func hierarchySeeded(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, name := range []string{"DDC", "Personal", "DDC/CV", "DDC/Appleby"} {
		if _, err := s.CreateProject(name, ""); err != nil {
			t.Fatal(err)
		}
	}
	add := func(project string, minutes int, kind store.Kind) {
		t.Helper()
		if _, err := s.AddEntry(store.NewEntry{Project: project, Subject: "w", Date: "2026-07-03", Start: "09:00", Minutes: minutes, Kind: kind}); err != nil {
			t.Fatal(err)
		}
	}
	add("DDC", 60, store.KindLogged)
	add("DDC/CV", 120, store.KindLogged)
	add("DDC/Appleby", 90, store.KindPlanned)
	add("Personal", 30, store.KindLogged)
	return s
}

func TestByProjectRollup(t *testing.T) {
	s := hierarchySeeded(t)
	r, err := Build(s, store.Filter{}, ByProject)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		key string
		sub bool
		log float64
		pln float64
	}
	var got []row
	for _, l := range r.Lines {
		got = append(got, row{l.Key, l.Sub, l.LoggedHours, l.PlannedHours})
	}
	want := []row{
		{"DDC", false, 3.0, 1.5},
		{"(direct)", true, 1.0, 0},
		{"Appleby", true, 0, 1.5},
		{"CV", true, 2.0, 0},
		{"Personal", false, 0.5, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %+v\nwant    %+v", got, want)
	}
}

func TestParentWithoutChildrenHasNoSubLines(t *testing.T) {
	s := hierarchySeeded(t)
	r, err := Build(s, store.Filter{Project: "Personal"}, ByProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Lines) != 1 || r.Lines[0].Sub {
		t.Errorf("lines = %+v", r.Lines)
	}
}

func TestCSVIsFlatFullPaths(t *testing.T) {
	s := hierarchySeeded(t)
	r, _ := Build(s, store.Filter{}, ByProject)
	csv := r.CSV()
	for _, want := range []string{"DDC,1.0,0.0", "DDC/CV,2.0,0.0", "DDC/Appleby,0.0,1.5", "Personal,0.5,0.0"} {
		if !strings.Contains(csv, want+"\n") {
			t.Errorf("csv missing %q:\n%s", want, csv)
		}
	}
	if strings.Contains(csv, "DDC,3.0") {
		t.Errorf("csv must not contain rollup rows:\n%s", csv)
	}
	if strings.Contains(csv, "(direct)") {
		t.Errorf("csv must not contain display-only keys:\n%s", csv)
	}
}
