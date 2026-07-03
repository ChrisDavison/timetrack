package store

import (
	"reflect"
	"testing"
	"time"
)

func TestParseTags(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"#research", []string{"research"}},
		{"#Research #IMPLEMENTATION", []string{"implementation", "research"}},
		{"research, implementation", []string{"implementation", "research"}},
		{"#a #a a", []string{"a"}},
		{"  #meetings  ", []string{"meetings"}},
	}
	for _, c := range cases {
		got := ParseTags(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseTags(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSnapStart(t *testing.T) {
	cases := []struct{ in, want string }{
		{"09:00", "09:00"},
		{"09:14", "09:00"},
		{"09:15", "09:30"},
		{"09:44", "09:30"},
		{"09:45", "10:00"},
		{"23:50", "23:30"}, // never snap past end of day
	}
	for _, c := range cases {
		got, err := SnapStart(c.in)
		if err != nil {
			t.Errorf("SnapStart(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("SnapStart(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "9am", "25:00", "09:60"} {
		if _, err := SnapStart(bad); err == nil {
			t.Errorf("SnapStart(%q): want error, got nil", bad)
		}
	}
}

func mustProject(t *testing.T, s *Store, name string) Project {
	t.Helper()
	p, err := s.CreateProject(name, "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAddEntry(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")

	e, err := s.AddEntry(NewEntry{
		Project: "engd",
		Subject: "thesis chapter 3",
		Notes:   "figures",
		Date:    "2026-07-03",
		Start:   "09:20",
		Minutes: 80,
		Kind:    KindLogged,
		Tags:    "#research #writing",
	})
	if err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	if e.Start != "09:30" {
		t.Errorf("start should snap to 09:30, got %s", e.Start)
	}
	if e.Blocks != 3 { // 80 min rounds up to 3 blocks
		t.Errorf("want 3 blocks, got %d", e.Blocks)
	}
	if e.ProjectName != "EngD" {
		t.Errorf("want project EngD, got %s", e.ProjectName)
	}
	if !reflect.DeepEqual(e.Tags, []string{"research", "writing"}) {
		t.Errorf("tags = %v", e.Tags)
	}
}

func TestAddEntryValidation(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")
	valid := NewEntry{Project: "EngD", Subject: "x", Date: "2026-07-03", Start: "09:00", Minutes: 30, Kind: KindLogged}

	cases := map[string]func(NewEntry) NewEntry{
		"unknown project": func(e NewEntry) NewEntry { e.Project = "nope"; return e },
		"empty subject":   func(e NewEntry) NewEntry { e.Subject = ""; return e },
		"bad date":        func(e NewEntry) NewEntry { e.Date = "03/07/2026"; return e },
		"zero duration":   func(e NewEntry) NewEntry { e.Minutes = 0; return e },
		"bad kind":        func(e NewEntry) NewEntry { e.Kind = "running"; return e },
	}
	for name, mutate := range cases {
		if _, err := s.AddEntry(mutate(valid)); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
	if _, err := s.AddEntry(valid); err != nil {
		t.Errorf("valid entry rejected: %v", err)
	}
}

func TestUpdateAndDeleteEntry(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")
	mustProject(t, s, "Personal")

	e, _ := s.AddEntry(NewEntry{Project: "EngD", Subject: "x", Date: "2026-07-03", Start: "09:00", Minutes: 30, Kind: KindLogged, Tags: "#a"})

	got, err := s.UpdateEntry(e.ID, NewEntry{Project: "Personal", Subject: "y", Date: "2026-07-04", Start: "10:00", Minutes: 60, Kind: KindPlanned, Tags: "#b"})
	if err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	if got.ProjectName != "Personal" || got.Subject != "y" || got.Blocks != 2 || got.Kind != KindPlanned {
		t.Errorf("update not applied: %+v", got)
	}
	if !reflect.DeepEqual(got.Tags, []string{"b"}) {
		t.Errorf("tags = %v", got.Tags)
	}

	if err := s.DeleteEntry(e.ID); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if _, err := s.Entry(e.ID); err == nil {
		t.Error("want error fetching deleted entry")
	}
}

func TestEntriesFilters(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")
	mustProject(t, s, "Personal")

	add := func(project, subject, date, tags string, kind Kind) {
		t.Helper()
		_, err := s.AddEntry(NewEntry{Project: project, Subject: subject, Date: date, Start: "09:00", Minutes: 30, Kind: kind, Tags: tags})
		if err != nil {
			t.Fatal(err)
		}
	}
	add("EngD", "thesis writing", "2026-07-01", "#research", KindLogged)
	add("EngD", "supervisor meeting", "2026-07-02", "#meetings", KindLogged)
	add("Personal", "tax return", "2026-07-02", "", KindLogged)
	add("Personal", "holiday planning", "2026-07-10", "#research", KindPlanned)

	cases := []struct {
		name string
		f    Filter
		want int
	}{
		{"no filter", Filter{}, 4},
		{"by project", Filter{Project: "EngD"}, 2},
		{"by tag", Filter{Tag: "research"}, 2},
		{"by date range", Filter{From: "2026-07-02", To: "2026-07-02"}, 2},
		{"by kind", Filter{Kind: KindPlanned}, 1},
		{"by search", Filter{Search: "thesis"}, 1},
		{"combined project+tag", Filter{Project: "Personal", Tag: "research"}, 1},
		{"combined range+project", Filter{From: "2026-07-01", To: "2026-07-05", Project: "Personal"}, 1},
	}
	for _, c := range cases {
		got, err := s.Entries(c.f)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(got) != c.want {
			t.Errorf("%s: want %d entries, got %d", c.name, c.want, len(got))
		}
	}
}

func TestTimerStartStop(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")

	begin := time.Date(2026, 7, 3, 9, 10, 0, 0, time.Local)
	e, err := s.StartTimer("EngD", "deep work", "", "#focus", begin)
	if err != nil {
		t.Fatalf("StartTimer: %v", err)
	}
	if e.Kind != KindRunning {
		t.Errorf("want running, got %s", e.Kind)
	}

	if _, err := s.StartTimer("EngD", "second", "", "", begin); err == nil {
		t.Error("second concurrent timer should be rejected")
	}

	running, err := s.RunningEntry()
	if err != nil || running == nil || running.ID != e.ID {
		t.Fatalf("RunningEntry = %v, %v", running, err)
	}

	// 50 minutes elapsed -> rounds up to 2 blocks; start snaps 09:10 -> 09:00.
	stopped, err := s.StopTimer(begin.Add(50 * time.Minute))
	if err != nil {
		t.Fatalf("StopTimer: %v", err)
	}
	if stopped.Kind != KindLogged || stopped.Blocks != 2 || stopped.Start != "09:00" || stopped.Date != "2026-07-03" {
		t.Errorf("stopped entry wrong: %+v", stopped)
	}

	if r, _ := s.RunningEntry(); r != nil {
		t.Error("timer should be cleared after stop")
	}
	if _, err := s.StopTimer(begin); err == nil {
		t.Error("stop with no running timer should error")
	}
}

func TestConfirmPlanned(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")
	e, _ := s.AddEntry(NewEntry{Project: "EngD", Subject: "x", Date: "2026-07-10", Start: "09:00", Minutes: 60, Kind: KindPlanned})

	got, err := s.ConfirmPlanned(e.ID)
	if err != nil {
		t.Fatalf("ConfirmPlanned: %v", err)
	}
	if got.Kind != KindLogged {
		t.Errorf("want logged, got %s", got.Kind)
	}

	if _, err := s.ConfirmPlanned(got.ID); err == nil {
		t.Error("confirming a non-planned entry should error")
	}
}

func TestEntryProjectPath(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "DDC")
	if _, err := s.CreateProject("DDC/CV", ""); err != nil {
		t.Fatal(err)
	}
	e, err := s.AddEntry(NewEntry{Project: "DDC/CV", Subject: "annotate", Date: "2026-07-03", Start: "09:00", Minutes: 30, Kind: KindLogged})
	if err != nil {
		t.Fatalf("AddEntry on sub-project: %v", err)
	}
	if e.ProjectPath() != "DDC/CV" {
		t.Errorf("ProjectPath() = %q", e.ProjectPath())
	}
}

func TestFilterParentIncludesChildren(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "DDC")
	mustProject(t, s, "Personal")
	for _, name := range []string{"DDC/CV", "DDC/Appleby"} {
		if _, err := s.CreateProject(name, ""); err != nil {
			t.Fatal(err)
		}
	}
	add := func(project, subject string) {
		t.Helper()
		if _, err := s.AddEntry(NewEntry{Project: project, Subject: subject, Date: "2026-07-03", Start: "09:00", Minutes: 30, Kind: KindLogged}); err != nil {
			t.Fatal(err)
		}
	}
	add("DDC", "admin")
	add("DDC/CV", "annotate")
	add("DDC/Appleby", "site visit")
	add("Personal", "tax")

	got, err := s.Entries(Filter{Project: "DDC"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("parent filter should include children: got %d entries", len(got))
	}

	got, err = s.Entries(Filter{Project: "DDC/CV"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Subject != "annotate" {
		t.Errorf("sub-project filter should be exact: %+v", got)
	}
}

func TestEntryUUIDAssigned(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")
	e1, _ := s.AddEntry(NewEntry{Project: "EngD", Subject: "a", Date: "2026-07-03", Start: "09:00", Minutes: 30, Kind: KindLogged})
	e2, _ := s.AddEntry(NewEntry{Project: "EngD", Subject: "b", Date: "2026-07-03", Start: "10:00", Minutes: 30, Kind: KindLogged})
	if len(e1.UUID) != 32 || len(e2.UUID) != 32 {
		t.Errorf("uuids should be 32 hex chars: %q %q", e1.UUID, e2.UUID)
	}
	if e1.UUID == e2.UUID {
		t.Error("uuids must be unique")
	}
	timer, err := s.StartTimer("EngD", "live", "", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(timer.UUID) != 32 {
		t.Errorf("timer entry needs a uuid too, got %q", timer.UUID)
	}
}

func TestUpdatedAtBumps(t *testing.T) {
	s := testStore(t)
	mustProject(t, s, "EngD")
	e, _ := s.AddEntry(NewEntry{Project: "EngD", Subject: "a", Date: "2026-07-03", Start: "09:00", Minutes: 30, Kind: KindLogged})
	if e.UpdatedAt == "" {
		t.Fatal("updated_at should be set on create")
	}
	time.Sleep(2 * time.Millisecond)
	got, err := s.UpdateEntry(e.ID, NewEntry{Project: "EngD", Subject: "b", Date: "2026-07-03", Start: "09:00", Minutes: 30, Kind: KindLogged})
	if err != nil {
		t.Fatal(err)
	}
	if !(got.UpdatedAt > e.UpdatedAt) {
		t.Errorf("updated_at should increase on update: %q -> %q", e.UpdatedAt, got.UpdatedAt)
	}
}
